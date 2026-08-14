package gluetun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_GetForwardedPort(t *testing.T) {
	// Mock Gluetun Server returning JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/openvpn/portforwarded" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"port": 54321}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewClient(server.URL)
	port, err := client.GetForwardedPort(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if port != 54321 {
		t.Fatalf("expected port 54321, got %d", port)
	}

	// Mock Server returning plain integer string
	plainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("61234\n"))
	}))
	defer plainServer.Close()

	plainClient := NewClient(plainServer.URL)
	plainPort, err := plainClient.GetForwardedPort(ctx)
	if err != nil {
		t.Fatalf("expected no error on plain integer, got %v", err)
	}
	if plainPort != 61234 {
		t.Fatalf("expected port 61234, got %d", plainPort)
	}

	// Mock 500 error
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errServer.Close()

	errClient := NewClient(errServer.URL)
	_, err = errClient.GetForwardedPort(ctx)
	if err == nil {
		t.Fatalf("expected error on 500 status, got nil")
	}

	// Empty URL
	emptyClient := NewClient("")
	_, err = emptyClient.GetForwardedPort(ctx)
	if err == nil {
		t.Fatalf("expected error on empty URL, got nil")
	}
}
