package main

import (
	"context"
	"errors"
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

var (
	currentPort int
	portMu      sync.Mutex
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logLevel := getEnv("LOG_LEVEL", "info")
	logger.Init(logLevel)

	// Parse environment variables and secret files
	qbitAddr := getEnv("QBIT_ADDR", "http://localhost:8080")
	qbitUser := getSecret("QBIT_USER", "QBIT_USER_FILE", "")
	qbitPass := getSecret("QBIT_PASS", "QBIT_PASS_FILE", "")
	qbitAPIKey := getSecret("QBIT_API_KEY", "QBIT_API_KEY_FILE", "")
	qbitAPIKeyHeader := getEnv("QBIT_API_KEY_HEADER", "X-Api-Key")
	portFile := getEnv("PORT_FILE", "/tmp/gluetun/forwarded_port")
	listenAddr := getEnv("LISTEN_ADDR", "")
	listenPort := getEnv("LISTEN_PORT", "9090")
	syncIntervalStr := getEnv("SYNC_INTERVAL", "10m")
	insecureSkipVerifyStr := getEnv("QBIT_INSECURE_SKIP_VERIFY", "false")
	caCertFile := getEnv("QBIT_CA_CERT_FILE", "")

	insecureSkipVerify, _ := strconv.ParseBool(insecureSkipVerifyStr)

	syncInterval, err := time.ParseDuration(syncIntervalStr)
	if err != nil {
		logger.Warn("Invalid SYNC_INTERVAL format, defaulting to 10m", "val", syncIntervalStr, "err", err)
		syncInterval = 10 * time.Minute
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
		portMu.Lock()
		if port == currentPort {
			portMu.Unlock()
			logger.Debug("Port is already synced, skipping", "port", port)
			return
		}
		portMu.Unlock()

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
				portMu.Lock()
				currentPort = port
				portMu.Unlock()
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

	mux := setupMux()
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

func setupMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			logger.Error("Failed to write healthz response", "err", err)
		}
	})
	return mux
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
