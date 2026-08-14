package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitForPort(ch <-chan int, expected int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}

		select {
		case p := <-ch:
			if p == expected {
				return true
			}
		case <-time.After(remaining):
			return false
		}
	}
}

func TestWatchFile_BasicAndReactivity(t *testing.T) {
	tempDir := t.TempDir()
	portFile := filepath.Join(tempDir, "forwarded_port")

	// Create initial file
	err := os.WriteFile(portFile, []byte("11111\n"), 0600)
	if err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	portCh := make(chan int, 10)
	callback := func(port int) {
		portCh <- port
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initial check
	CheckFileNow(portFile, callback)
	if !waitForPort(portCh, 11111, 1*time.Second) {
		t.Fatalf("expected port 11111 on startup check")
	}

	// Start watcher
	err = WatchFile(ctx, portFile, callback)
	if err != nil {
		t.Fatalf("WatchFile returned error: %v", err)
	}

	// Give watcher loop a moment to attach
	time.Sleep(100 * time.Millisecond)

	// Simulate port update
	err = os.WriteFile(portFile, []byte("22222\n"), 0600)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if !waitForPort(portCh, 22222, 2*time.Second) {
		t.Fatalf("expected port 22222 after file update")
	}
}

func TestWatchFile_DelayedDirectoryCreation(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "delayed_gluetun")
	portFile := filepath.Join(subDir, "forwarded_port")

	portCh := make(chan int, 10)
	callback := func(port int) {
		portCh <- port
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watcher BEFORE directory exists
	err := WatchFile(ctx, portFile, callback)
	if err != nil {
		t.Fatalf("WatchFile returned error: %v", err)
	}

	// Wait 100ms, then create directory and file
	time.Sleep(100 * time.Millisecond)
	if err := os.MkdirAll(subDir, 0750); err != nil {
		t.Fatalf("failed to create delayed directory: %v", err)
	}
	if err := os.WriteFile(portFile, []byte("33333\n"), 0600); err != nil {
		t.Fatalf("failed to write port file: %v", err)
	}

	// Should attach on next ticker tick and read 33333
	if !waitForPort(portCh, 33333, 3*time.Second) {
		t.Fatalf("expected watcher to discover newly created directory and read port 33333")
	}
}

func TestReadPort_Validation(t *testing.T) {
	tempDir := t.TempDir()
	validFile := filepath.Join(tempDir, "valid")
	invalidFile := filepath.Join(tempDir, "invalid")
	outOfRangeFile := filepath.Join(tempDir, "out_of_range")

	_ = os.WriteFile(validFile, []byte("54321\n"), 0600)
	_ = os.WriteFile(invalidFile, []byte("not_a_number\n"), 0600)
	_ = os.WriteFile(outOfRangeFile, []byte("99999\n"), 0600)

	port, err := ReadPort(validFile)
	if err != nil || port != 54321 {
		t.Fatalf("expected port 54321, got %d, err %v", port, err)
	}

	_, err = ReadPort(invalidFile)
	if err == nil {
		t.Fatalf("expected error for invalid port string, got nil")
	}

	_, err = ReadPort(outOfRangeFile)
	if err == nil {
		t.Fatalf("expected error for out of range port, got nil")
	}
}
