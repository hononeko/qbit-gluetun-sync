package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestHelperProcess is a requirement of the CLI test pattern.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)
}

func TestHealthCheck(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	mux := setupMux()

	mux.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	if rr.Body.String() != "OK" {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), "OK")
	}

	// Test non-GET request
	reqPost, err := http.NewRequest(http.MethodPost, "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	rrPost := httptest.NewRecorder()
	mux.ServeHTTP(rrPost, reqPost)

	if status := rrPost.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code for POST: got %v want %v", status, http.StatusMethodNotAllowed)
	}
}

func TestGetEnv(t *testing.T) {
	_ = os.Setenv("TEST_ENV_VAR", "set_value")
	defer func() { _ = os.Unsetenv("TEST_ENV_VAR") }()

	val := getEnv("TEST_ENV_VAR", "default")
	if val != "set_value" {
		t.Errorf("Expected set_value, got %s", val)
	}

	val2 := getEnv("MISSING_VAR", "default")
	if val2 != "default" {
		t.Errorf("Expected default, got %s", val2)
	}
}

func TestReconciliationLoop(t *testing.T) {
	tempDir := t.TempDir()
	portFile := filepath.Join(tempDir, "forwarded_port")

	if err := os.WriteFile(portFile, []byte("44444\n"), 0600); err != nil {
		t.Fatalf("failed to write test port file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var triggerCount int32
	syncFunc := func(port int) {
		if port == 44444 {
			atomic.AddInt32(&triggerCount, 1)
		}
	}

	go runReconciliationLoop(ctx, portFile, syncFunc, 50*time.Millisecond)

	// Wait for ticker triggers
	time.Sleep(160 * time.Millisecond)
	cancel()

	if count := atomic.LoadInt32(&triggerCount); count < 2 {
		t.Errorf("expected reconciliation loop to trigger at least 2 times, got %d", count)
	}
}
