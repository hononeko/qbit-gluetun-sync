package qbit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
