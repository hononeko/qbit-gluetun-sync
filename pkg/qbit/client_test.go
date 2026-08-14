package qbit

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClient_SetListenPort(t *testing.T) {
	currentConfiguredPort := 8080

	// Mock qBitTorrent Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "username=admin") && strings.Contains(string(body), "password=adminadmin") {
				//nolint:gosec // Mock server for testing
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-cookie"})
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("Ok."))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Fails."))
			return
		}

		if r.URL.Path == "/api/v2/app/setPreferences" {
			cookie, err := r.Cookie("SID")
			if err != nil || cookie.Value != "test-cookie" {
				w.WriteHeader(http.StatusForbidden)
				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB limit for testing
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			jsonStr := r.FormValue("json")
			var prefs map[string]interface{}
			if err := json.Unmarshal([]byte(jsonStr), &prefs); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if port, ok := prefs["listen_port"].(float64); ok && port == 12345 {
				currentConfiguredPort = int(port)
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if r.URL.Path == "/api/v2/app/preferences" {
			cookie, err := r.Cookie("SID")
			if err != nil || cookie.Value != "test-cookie" {
				w.WriteHeader(http.StatusForbidden)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"listen_port": currentConfiguredPort,
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewClient(server.URL, "admin", "adminadmin")

	// Successful port setting
	err := client.SetListenPort(ctx, 12345)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify GetPreferences
	prefs, err := client.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("expected no error from GetPreferences, got %v", err)
	}
	if p, ok := prefs["listen_port"].(float64); !ok || int(p) != 12345 {
		t.Fatalf("expected listen_port 12345, got %v", prefs["listen_port"])
	}

	// Test invalid port validation
	err = client.SetListenPort(ctx, 0)
	if err == nil {
		t.Fatalf("expected error for port 0, got nil")
	}

	err = client.SetListenPort(ctx, 70000)
	if err == nil {
		t.Fatalf("expected error for port 70000, got nil")
	}

	// Test invalid auth returning "Fails."
	badClient := NewClient(server.URL, "admin", "wrong")
	err = badClient.SetListenPort(ctx, 12345)
	if err == nil {
		t.Fatalf("expected auth error on bad password, got nil")
	}
}

func TestClient_APIKeyAuth(t *testing.T) {
	// Mock Server with API Key Verification
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKeyHeader := r.Header.Get("X-Custom-Key")
		authHeader := r.Header.Get("Authorization")

		if apiKeyHeader != "secret_api_token" || authHeader != "Bearer secret_api_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/api/v2/app/setPreferences" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//nolint:gosec // Mock test token
	client, err := NewClientWithOptions(server.URL, "", "", ClientOptions{
		APIKey:       "secret_api_token",
		APIKeyHeader: "X-Custom-Key",
	})
	if err != nil {
		t.Fatalf("failed to create client with API key: %v", err)
	}

	err = client.SetListenPort(ctx, 23456)
	if err != nil {
		t.Fatalf("expected successful API key auth, got %v", err)
	}

	// Bad API key
	//nolint:gosec // Mock test token
	badKeyClient, err := NewClientWithOptions(server.URL, "", "", ClientOptions{
		APIKey:       "wrong_token",
		APIKeyHeader: "X-Custom-Key",
	})
	if err != nil {
		t.Fatalf("failed to create client with bad API key: %v", err)
	}
	err = badKeyClient.SetListenPort(ctx, 23456)
	if err == nil {
		t.Fatalf("expected 401 error with wrong API key, got nil")
	}
}

func TestClient_TLSOptions(t *testing.T) {
	// Mock TLS Server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"listen_port": 50000}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Default client should fail TLS verification on self-signed cert
	defaultClient := NewClient(server.URL, "", "")
	_, err := defaultClient.GetPreferences(ctx)
	if err == nil {
		t.Fatalf("expected TLS verification failure on self-signed cert, got nil")
	}

	// Client with InsecureSkipVerify should succeed
	tlsClient, err := NewClientWithOptions(server.URL, "", "", ClientOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("failed to create client with InsecureSkipVerify: %v", err)
	}

	prefs, err := tlsClient.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("expected success with InsecureSkipVerify, got %v", err)
	}
	if p, ok := prefs["listen_port"].(float64); !ok || int(p) != 50000 {
		t.Fatalf("expected listen_port 50000, got %v", prefs["listen_port"])
	}

	// Test valid custom CA cert
	tempDir := t.TempDir()
	caCertFile := filepath.Join(tempDir, "server_ca.crt")
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})
	if err := os.WriteFile(caCertFile, certPEM, 0600); err != nil {
		t.Fatalf("failed to write custom CA file: %v", err)
	}

	caClient, err := NewClientWithOptions(server.URL, "", "", ClientOptions{
		CACertFile: caCertFile,
	})
	if err != nil {
		t.Fatalf("failed to create client with custom CA cert: %v", err)
	}
	prefsWithCA, err := caClient.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("expected success with valid custom CA cert, got %v", err)
	}
	if p, ok := prefsWithCA["listen_port"].(float64); !ok || int(p) != 50000 {
		t.Fatalf("expected listen_port 50000 with custom CA, got %v", prefsWithCA["listen_port"])
	}

	// Test invalid CA cert file path
	_, err = NewClientWithOptions(server.URL, "", "", ClientOptions{
		CACertFile: "/path/to/nonexistent/ca.crt",
	})
	if err == nil {
		t.Fatalf("expected error for non-existent CA cert file, got nil")
	}

	// Test corrupt CA cert file
	corruptCA := filepath.Join(tempDir, "corrupt.crt")
	_ = os.WriteFile(corruptCA, []byte("NOT_A_VALID_PEM"), 0600)
	_, err = NewClientWithOptions(server.URL, "", "", ClientOptions{
		CACertFile: corruptCA,
	})
	if err == nil {
		t.Fatalf("expected error for invalid PEM CA cert, got nil")
	}
}
