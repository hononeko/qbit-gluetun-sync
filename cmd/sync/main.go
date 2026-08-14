package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hononeko/qbit-gluetun-sync/pkg/logger"
	"github.com/hononeko/qbit-gluetun-sync/pkg/qbit"
	"github.com/hononeko/qbit-gluetun-sync/pkg/watcher"
)

// SyncState tracks real-time sync metrics and upstream health.
type SyncState struct {
	mu                     sync.RWMutex
	currentPort            int
	lastSuccessTime        time.Time
	lastAttemptTime        time.Time
	lastSyncErr            string
	syncSuccessCount       int64
	syncFailureCount       int64
	qbittorrentReachable   bool
	initialSyncDone        bool
	reconciliationInterval time.Duration
}

func (s *SyncState) recordSuccess(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.currentPort = port
	s.lastSuccessTime = now
	s.lastAttemptTime = now
	s.lastSyncErr = ""
	s.syncSuccessCount++
	s.qbittorrentReachable = true
	s.initialSyncDone = true
}

func (s *SyncState) recordFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAttemptTime = time.Now().UTC()
	if err != nil {
		s.lastSyncErr = err.Error()
	}
	s.syncFailureCount++
	s.qbittorrentReachable = false
}

func (s *SyncState) isReady() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.initialSyncDone {
		return false, "waiting for initial port synchronization"
	}
	if !s.qbittorrentReachable {
		return false, fmt.Sprintf("qbittorrent unreachable (last error: %s)", s.lastSyncErr)
	}
	return true, ""
}

func (s *SyncState) getStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := "synced"
	if !s.initialSyncDone {
		status = "waiting_for_initial_sync"
	} else if !s.qbittorrentReachable {
		status = "degraded"
	}

	res := map[string]interface{}{
		"status":                  status,
		"current_port":            s.currentPort,
		"last_sync_error":         s.lastSyncErr,
		"sync_success_count":      s.syncSuccessCount,
		"sync_failure_count":      s.syncFailureCount,
		"qbittorrent_reachable":   s.qbittorrentReachable,
		"initial_sync_done":       s.initialSyncDone,
		"reconciliation_interval": s.reconciliationInterval.String(),
	}
	if !s.lastSuccessTime.IsZero() {
		res["last_success_time"] = s.lastSuccessTime.Format(time.RFC3339)
	}
	if !s.lastAttemptTime.IsZero() {
		res["last_attempt_time"] = s.lastAttemptTime.Format(time.RFC3339)
	}
	return res
}

func (s *SyncState) getMetrics() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lastSuccessTimestamp int64
	if !s.lastSuccessTime.IsZero() {
		lastSuccessTimestamp = s.lastSuccessTime.Unix()
	}

	reachableVal := 0
	if s.qbittorrentReachable {
		reachableVal = 1
	}

	var sb strings.Builder
	sb.WriteString("# HELP qbit_gluetun_sync_current_port Currently configured listening port in qBitTorrent\n")
	sb.WriteString("# TYPE qbit_gluetun_sync_current_port gauge\n")
	fmt.Fprintf(&sb, "qbit_gluetun_sync_current_port %d\n\n", s.currentPort)

	sb.WriteString("# HELP qbit_gluetun_sync_operations_total Total number of port sync attempts\n")
	sb.WriteString("# TYPE qbit_gluetun_sync_operations_total counter\n")
	fmt.Fprintf(&sb, "qbit_gluetun_sync_operations_total{status=\"success\"} %d\n", s.syncSuccessCount)
	fmt.Fprintf(&sb, "qbit_gluetun_sync_operations_total{status=\"failure\"} %d\n\n", s.syncFailureCount)

	sb.WriteString("# HELP qbit_gluetun_sync_last_success_timestamp_seconds Unix timestamp of last successful sync\n")
	sb.WriteString("# TYPE qbit_gluetun_sync_last_success_timestamp_seconds gauge\n")
	fmt.Fprintf(&sb, "qbit_gluetun_sync_last_success_timestamp_seconds %d\n\n", lastSuccessTimestamp)

	sb.WriteString("# HELP qbit_gluetun_sync_qbittorrent_reachable Whether qBitTorrent is currently reachable (1 for reachable, 0 for unreachable)\n")
	sb.WriteString("# TYPE qbit_gluetun_sync_qbittorrent_reachable gauge\n")
	fmt.Fprintf(&sb, "qbit_gluetun_sync_qbittorrent_reachable %d\n", reachableVal)

	return sb.String()
}

func main() {
	// Parse CLI healthcheck flag before service startup
	listenAddr := getEnv("LISTEN_ADDR", "")
	listenPort := getEnv("LISTEN_PORT", "9090")

	if isHealthCheckMode(os.Args) {
		code := runCLIHealthCheck(listenAddr, listenPort)
		os.Exit(code)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logLevel := getEnv("LOG_LEVEL", "info")
	logFormat := getEnv("LOG_FORMAT", "text")
	logger.InitWithFormat(logLevel, logFormat)

	// Parse environment variables and secret files
	qbitAddr := getEnv("QBIT_ADDR", "http://localhost:8080")
	qbitUser := getSecret("QBIT_USER", "QBIT_USER_FILE", "")
	qbitPass := getSecret("QBIT_PASS", "QBIT_PASS_FILE", "")
	qbitAPIKey := getSecret("QBIT_API_KEY", "QBIT_API_KEY_FILE", "")
	qbitAPIKeyHeader := getEnv("QBIT_API_KEY_HEADER", "X-Api-Key")
	portFile := getEnv("PORT_FILE", "/tmp/gluetun/forwarded_port")
	syncIntervalStr := getEnv("SYNC_INTERVAL", "10m")
	insecureSkipVerifyStr := getEnv("QBIT_INSECURE_SKIP_VERIFY", "false")
	caCertFile := getEnv("QBIT_CA_CERT_FILE", "")

	insecureSkipVerify, _ := strconv.ParseBool(insecureSkipVerifyStr)

	syncInterval, err := time.ParseDuration(syncIntervalStr)
	if err != nil {
		logger.Warn("Invalid SYNC_INTERVAL format, defaulting to 10m", "val", syncIntervalStr, "err", err)
		syncInterval = 10 * time.Minute
	}

	state := &SyncState{
		reconciliationInterval: syncInterval,
	}

	// Initialize qBitTorrent Client with security/TLS/Auth options
	qbitOpts := qbit.ClientOptions{
		InsecureSkipVerify: insecureSkipVerify,
		CACertFile:         caCertFile,
		APIKey:             qbitAPIKey,
		APIKeyHeader:       qbitAPIKeyHeader,
		Timeout:            10 * time.Second,
	}
	qbitClient, err := qbit.NewClientWithOptions(qbitAddr, qbitUser, qbitPass, qbitOpts)
	if err != nil {
		logger.Fatal("Failed to initialize qBitTorrent client", "err", err)
	}

	// Callback to sync port
	syncPortFunc := func(port int) {
		state.mu.RLock()
		if port == state.currentPort && state.qbittorrentReachable {
			state.mu.RUnlock()
			logger.Debug("Port is already synced, skipping", "port", port)
			return
		}
		state.mu.RUnlock()

		logger.Info("Syncing new port to qBitTorrent", "port", port)

		var syncErr error
		maxRetries := 5
		backoff := 1 * time.Second

		for i := 0; i < maxRetries; i++ {
			if ctx.Err() != nil {
				logger.Warn("Context cancelled during sync retry", "port", port)
				return
			}

			syncErr = qbitClient.SetListenPort(ctx, port)
			if syncErr == nil {
				logger.Info("Successfully set port in qBitTorrent", "port", port)
				state.recordSuccess(port)
				return
			}

			logger.Warn("Failed to set port in qBitTorrent", "attempt", i+1, "maxRetries", maxRetries, "err", syncErr)
			if i < maxRetries-1 {
				logger.Info("Retrying...", "backoff", backoff)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
					backoff *= 2
				}
			}
		}

		state.recordFailure(syncErr)
		logger.Error("Exhausted all retries. Failed to sync port to qBitTorrent", "port", port, "err", syncErr)
	}

	// Initial check in case file already exists
	watcher.CheckFileNow(portFile, syncPortFunc)

	// Start file watcher with context lifecycle
	logger.Info("Starting file watcher", "file", portFile)
	if err := watcher.WatchFile(ctx, portFile, syncPortFunc); err != nil {
		logger.Warn("Failed to start file watcher", "err", err)
	}

	// Start periodic reconciliation loop if configured
	if syncInterval > 0 {
		logger.Info("Starting reconciliation loop", "interval", syncInterval)
		go runReconciliationLoop(ctx, portFile, syncPortFunc, syncInterval)
	}

	mux := setupMux(state)
	bindAddr := net.JoinHostPort(listenAddr, listenPort)

	logger.Info("Starting sidecar server", "bindAddr", bindAddr, "qbitAddr", qbitAddr)
	server := &http.Server{
		Addr:              bindAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	// Run HTTP server in background
	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
		close(serverErrCh)
	}()

	// Wait for shutdown signal or fatal server error
	select {
	case <-ctx.Done():
		logger.Info("Shutdown signal received, shutting down gracefully...")
	case err := <-serverErrCh:
		logger.Fatal("HTTP server failed unexpectedly", "err", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server shutdown failed", "err", err)
	} else {
		logger.Info("Server stopped cleanly")
	}
}

func runReconciliationLoop(ctx context.Context, portFile string, syncPortFunc func(port int), interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Debug("Reconciliation ticker: checking port file", "file", portFile)
			watcher.CheckFileNow(portFile, syncPortFunc)
		}
	}
}

func setupMux(state *SyncState) *http.ServeMux {
	mux := http.NewServeMux()

	// /healthz - Liveness probe
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// /readyz - Readiness probe
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if state == nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		ready, reason := state.isReady()
		if ready {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("READY"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, "NOT READY: %s", reason)
	})

	// /status - JSON diagnostic endpoint
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if state != nil {
			_ = json.NewEncoder(w).Encode(state.getStatus())
		} else {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	})

	// /metrics - Prometheus exposition endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if state != nil {
			_, _ = w.Write([]byte(state.getMetrics()))
		}
	})

	return mux
}

func isHealthCheckMode(args []string) bool {
	for _, arg := range args[1:] {
		if arg == "-healthcheck" || arg == "--healthcheck" {
			return true
		}
	}
	return false
}

func runCLIHealthCheck(listenAddr, listenPort string) int {
	host := listenAddr
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	targetURL := fmt.Sprintf("http://%s/healthz", net.JoinHostPort(host, listenPort))

	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Health check error creating request: %v\n", err)
		return 1
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Health check OK")
		return 0
	}

	fmt.Fprintf(os.Stderr, "Health check returned status: %d\n", resp.StatusCode)
	return 1
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// getSecret resolves a secret either from a file (e.g. QBIT_PASS_FILE) or from an env var (QBIT_PASS).
func getSecret(envKey, fileEnvKey, fallback string) string {
	if filePath, exists := os.LookupEnv(fileEnvKey); exists && strings.TrimSpace(filePath) != "" {
		cleanPath := filepath.Clean(filePath)
		//nolint:gosec // Secret file path is explicitly provided by admin configuration
		data, err := os.ReadFile(cleanPath)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
		logger.Warn("Failed to read secret file, falling back to environment variable", "fileKey", fileEnvKey, "path", cleanPath, "err", err)
	}
	return getEnv(envKey, fallback)
}
