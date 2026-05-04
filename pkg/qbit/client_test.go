package qbit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_SetListenPort(t *testing.T) {
	// Mock qBitTorrent Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			// Read body to verify
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "username=admin") && strings.Contains(string(body), "password=adminadmin") {
				//nolint:gosec // Mock server for testing
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-cookie"})
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusForbidden)
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
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "adminadmin")

	err := client.SetListenPort(12345)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	err = client.SetListenPort(99999)
	if err == nil {
		t.Fatalf("expected error for bad port parsing returning 400, got nil")
	}

	// Test invalid auth
	badClient := NewClient(server.URL, "admin", "wrong")
	err = badClient.SetListenPort(12345)
	if err == nil {
		t.Fatalf("expected auth error, got nil")
	}
}
