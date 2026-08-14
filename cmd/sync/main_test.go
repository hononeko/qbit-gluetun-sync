package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestGetSecret(t *testing.T) {
	tempDir := t.TempDir()
	secretFilePath := filepath.Join(tempDir, "pass_secret.txt")

	if err := os.WriteFile(secretFilePath, []byte("super_secret_from_file\n"), 0600); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	_ = os.Setenv("TEST_PASS", "plain_password")
	_ = os.Setenv("TEST_PASS_FILE", secretFilePath)
	defer func() {
		_ = os.Unsetenv("TEST_PASS")
		_ = os.Unsetenv("TEST_PASS_FILE")
	}()

	// File secret must take precedence over env var
	val := getSecret("TEST_PASS", "TEST_PASS_FILE", "fallback")
	if val != "super_secret_from_file" {
		t.Errorf("Expected super_secret_from_file from file, got %s", val)
	}

	// When file doesn't exist, should fall back to env var
	_ = os.Setenv("TEST_PASS_FILE", filepath.Join(tempDir, "non_existent.txt"))
	valFallback := getSecret("TEST_PASS", "TEST_PASS_FILE", "fallback")
	if valFallback != "plain_password" {
		t.Errorf("Expected plain_password from env fallback, got %s", valFallback)
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

	triggerCh := make(chan struct{}, 5)
	syncFunc := func(port int) {
		if port == 44444 {
			triggerCh <- struct{}{}
		}
	}

	go runReconciliationLoop(ctx, portFile, syncFunc, 10*time.Millisecond)

	// Wait for at least 2 triggers deterministically
	for i := 0; i < 2; i++ {
		select {
		case <-triggerCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for reconciliation trigger %d", i+1)
		}
	}
}
