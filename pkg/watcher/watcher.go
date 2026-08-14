package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hononeko/qbit-gluetun-sync/pkg/logger"
)

// WatchFile continuously watches the directory containing filePath for CREATE, WRITE, or RENAME events.
// It gracefully handles missing parent directories by retrying until the directory is created.
func WatchFile(ctx context.Context, filePath string, callback func(port int)) error {
	cleanPath := filepath.Clean(filePath)
	dir := filepath.Dir(cleanPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	go runWatchLoop(ctx, watcher, dir, cleanPath, callback)
	return nil
}

// runWatchLoop handles dynamic directory attachment, event processing, and reattachment on directory recreation.
func runWatchLoop(ctx context.Context, watcher *fsnotify.Watcher, dir, targetFile string, callback func(port int)) {
	defer func() { _ = watcher.Close() }()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	attached := false

	for {
		if !attached {
			if _, err := os.Stat(dir); err == nil {
				if err := watcher.Add(dir); err == nil {
					logger.Info("Successfully attached watcher to directory", "dir", dir)
					attached = true
					// Check if target file is already present
					CheckFileNow(targetFile, callback)
				} else {
					logger.Debug("Retrying watcher attachment to directory", "dir", dir, "err", err)
				}
			}
		}

		select {
		case <-ctx.Done():
			logger.Debug("Stopping file watcher loop", "file", targetFile)
			return

		case <-ticker.C:
			// Periodic retry if not yet attached or if directory was recreated
			if !attached {
				continue
			}
			// Verify watched directory still exists
			if _, err := os.Stat(dir); err != nil {
				logger.Warn("Watched directory disappeared, re-entering attachment loop", "dir", dir)
				_ = watcher.Remove(dir)
				attached = false
			}

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// If watched directory was removed or renamed
			if filepath.Clean(event.Name) == dir && (event.Op&fsnotify.Remove != 0 || event.Op&fsnotify.Rename != 0) {
				logger.Warn("Watched directory was removed or renamed", "dir", dir, "op", event.Op.String())
				_ = watcher.Remove(dir)
				attached = false
				continue
			}

			// Match target file on Create, Write, or Rename
			if filepath.Clean(event.Name) == targetFile {
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					logger.Debug("Detected file event", "op", event.Op.String(), "file", event.Name)
					handleFileChange(targetFile, callback)
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Error("Watcher received error", "err", err)
		}
	}
}

// CheckFileNow manually checks the file once, with retries for non-atomic writes.
func CheckFileNow(filePath string, callback func(port int)) {
	cleanPath := filepath.Clean(filePath)
	if _, err := os.Stat(cleanPath); err == nil {
		logger.Debug("Checking port file", "file", cleanPath)
		handleFileChange(cleanPath, callback)
	}
}

// ReadPort reads and parses a port from the given file with retry for partial writes.
func ReadPort(filePath string) (int, error) {
	cleanPath := filepath.Clean(filePath)

	var lastErr error
	maxAttempts := 3
	for i := 0; i < maxAttempts; i++ {
		//nolint:gosec // filePath is controlled by config/env
		content, err := os.ReadFile(cleanPath)
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}

		portStr := strings.TrimSpace(string(content))
		if portStr == "" {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return 0, fmt.Errorf("invalid port syntax in %s: %w", cleanPath, err)
		}

		if port <= 0 || port > 65535 {
			return 0, fmt.Errorf("port %d in %s is out of range (1-65535)", port, cleanPath)
		}

		return port, nil
	}

	if lastErr != nil {
		return 0, fmt.Errorf("failed to read port file %s: %w", cleanPath, lastErr)
	}
	return 0, fmt.Errorf("port file %s was empty after %d read attempts", cleanPath, maxAttempts)
}

// handleFileChange reads the file and executes the callback if valid.
func handleFileChange(filePath string, callback func(port int)) {
	port, err := ReadPort(filePath)
	if err != nil {
		logger.Warn("Failed to parse port from file", "file", filePath, "err", err)
		return
	}

	callback(port)
}
