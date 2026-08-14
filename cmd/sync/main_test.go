package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	mux := setupMux(nil, nil)

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

func TestReadyz(t *testing.T) {
	state := &SyncState{}
	mux := setupMux(state, nil)

	// 1. Initial state (not ready)
	req, _ := http.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 on uninitialized state, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "waiting for initial port synchronization") {
		t.Errorf("expected reason in body, got: %s", rr.Body.String())
	}

	// 2. Success recorded -> Ready
	state.recordSuccess(12345)
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Errorf("expected status 200 after success, got %d", rr2.Code)
	}
	if rr2.Body.String() != "READY" {
		t.Errorf("expected body READY, got %s", rr2.Body.String())
	}

	// 3. Failure recorded -> Degraded / Not Ready
	state.recordFailure(errors.New("connection timeout"))
	rr3 := httptest.NewRecorder()
	mux.ServeHTTP(rr3, req)
	if rr3.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 after failure, got %d", rr3.Code)
	}
	if !strings.Contains(rr3.Body.String(), "qbittorrent unreachable") {
		t.Errorf("expected failure error in body, got: %s", rr3.Body.String())
	}
}

func TestStatusEndpoint(t *testing.T) {
	state := &SyncState{
		reconciliationInterval: 10 * time.Minute,
	}
	state.recordSuccess(34567)

	mux := setupMux(state, nil)
	req, _ := http.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &data); err != nil {
		t.Fatalf("failed to decode JSON status: %v", err)
	}

	if data["status"] != "synced" {
		t.Errorf("expected status synced, got %v", data["status"])
	}
	if port, ok := data["current_port"].(float64); !ok || int(port) != 34567 {
		t.Errorf("expected current_port 34567, got %v", data["current_port"])
	}
	if reachable, ok := data["qbittorrent_reachable"].(bool); !ok || !reachable {
		t.Errorf("expected qbittorrent_reachable true, got %v", data["qbittorrent_reachable"])
	}
	if _, ok := data["last_success_time"].(string); !ok {
		t.Errorf("expected last_success_time in status")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	state := &SyncState{}
	state.recordSuccess(45678)
	state.recordFailure(errors.New("temporary error"))

	mux := setupMux(state, nil)
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "qbit_gluetun_sync_current_port 45678") {
		t.Errorf("metrics missing current_port: %s", body)
	}
	if !strings.Contains(body, `qbit_gluetun_sync_operations_total{status="success"} 1`) {
		t.Errorf("metrics missing success count: %s", body)
	}
	if !strings.Contains(body, `qbit_gluetun_sync_operations_total{status="failure"} 1`) {
		t.Errorf("metrics missing failure count: %s", body)
	}
	if !strings.Contains(body, "qbit_gluetun_sync_qbittorrent_reachable 0") {
		t.Errorf("metrics missing qbittorrent_reachable 0: %s", body)
	}
	if !strings.Contains(body, "qbit_gluetun_sync_last_success_timestamp_seconds") {
		t.Errorf("metrics missing last_success_timestamp_seconds: %s", body)
	}
}

func TestManualSyncEndpoint(t *testing.T) {
	var triggered int32
	triggerSync := func() {
		atomic.AddInt32(&triggered, 1)
	}

	state := &SyncState{}
	state.recordSuccess(55555)

	mux := setupMux(state, triggerSync)

	req, _ := http.NewRequest(http.MethodPost, "/sync", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from POST /sync, got %d", rr.Code)
	}

	if val := atomic.LoadInt32(&triggered); val != 1 {
		t.Errorf("expected triggerSync to be called once, got %d", val)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode json response: %v", err)
	}
	if res["status"] != "sync_triggered" {
		t.Errorf("expected status sync_triggered, got %v", res["status"])
	}
	if port, ok := res["current_port"].(float64); !ok || int(port) != 55555 {
		t.Errorf("expected current_port 55555, got %v", res["current_port"])
	}
}

func TestHealthCheckModeAndCLI(t *testing.T) {
	if !isHealthCheckMode([]string{"app", "-healthcheck"}) {
		t.Errorf("expected true for -healthcheck")
	}
	if !isHealthCheckMode([]string{"app", "--healthcheck"}) {
		t.Errorf("expected true for --healthcheck")
	}
	if isHealthCheckMode([]string{"app", "normal_arg"}) {
		t.Errorf("expected false for normal_arg")
	}

	// Test CLI against test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}

	exitCode := runCLIHealthCheck("127.0.0.1", port)
	if exitCode != 0 {
		t.Errorf("expected exit code 0 for healthy server, got %d", exitCode)
	}

	// Test bad port
	exitCodeBad := runCLIHealthCheck("127.0.0.1", "1")
	if exitCodeBad != 1 {
		t.Errorf("expected exit code 1 for unreachable server, got %d", exitCodeBad)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	triggerCh := make(chan struct{}, 5)
	syncFunc := func() {
		triggerCh <- struct{}{}
	}

	go runReconciliationLoop(ctx, syncFunc, 10*time.Millisecond)

	// Wait for at least 2 triggers deterministically
	for i := 0; i < 2; i++ {
		select {
		case <-triggerCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for reconciliation trigger %d", i+1)
		}
	}
}
