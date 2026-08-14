package notifier

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

func TestNotifier_SendPortUpdate_Generic(t *testing.T) {
	var receivedPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n := NewNotifier()
	err := n.SendPortUpdate(ctx, server.URL, 11111, 22222)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if receivedPayload["event"] != "port_synced" {
		t.Errorf("expected event 'port_synced', got %v", receivedPayload["event"])
	}
	if p, ok := receivedPayload["previous_port"].(float64); !ok || int(p) != 11111 {
		t.Errorf("expected previous_port 11111, got %v", receivedPayload["previous_port"])
	}
	if n, ok := receivedPayload["new_port"].(float64); !ok || int(n) != 22222 {
		t.Errorf("expected new_port 22222, got %v", receivedPayload["new_port"])
	}
}

func TestNotifier_SendPortUpdate_Discord(t *testing.T) {
	var bodyBytes []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n := NewNotifier()
	discordURL := server.URL + "/discord.com/api/webhooks/test"
	err := n.SendPortUpdate(ctx, discordURL, 33333, 44444)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	bodyStr := string(bodyBytes)
	if !strings.Contains(bodyStr, "qBit-Gluetun Sync") || !strings.Contains(bodyStr, "Forwarded Port Updated") {
		t.Errorf("expected discord embed structure, got: %s", bodyStr)
	}
}

func TestNotifier_EmptyURL(t *testing.T) {
	n := NewNotifier()
	err := n.SendPortUpdate(context.Background(), "", 1, 2)
	if err != nil {
		t.Fatalf("expected nil for empty URL, got %v", err)
	}
}

func TestNotifier_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	n := NewNotifier()
	err := n.SendPortUpdate(context.Background(), server.URL, 1, 2)
	if err == nil {
		t.Fatalf("expected error on 400 response, got nil")
	}
}
