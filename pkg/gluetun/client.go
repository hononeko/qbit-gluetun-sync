package gluetun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client interacts with the Gluetun Control Server REST API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a new Gluetun API client.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type portResponse struct {
	Port int `json:"port"`
}

// GetForwardedPort queries the Gluetun API for the current forwarded port.
func (c *Client) GetForwardedPort(ctx context.Context) (int, error) {
	if c.BaseURL == "" {
		return 0, fmt.Errorf("gluetun base URL is empty")
	}

	apiURL, err := url.JoinPath(c.BaseURL, "/v1/openvpn/portforwarded")
	if err != nil {
		return 0, fmt.Errorf("failed to construct gluetun portforwarded URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create gluetun request: %w", err)
	}

	//nolint:gosec // URL is internally constructed via config
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("gluetun API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("gluetun API returned status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return 0, fmt.Errorf("failed to read gluetun API response: %w", err)
	}

	trimmed := strings.TrimSpace(string(bodyBytes))

	// Try parsing JSON payload {"port": 12345}
	var pr portResponse
	if jsonErr := json.Unmarshal([]byte(trimmed), &pr); jsonErr == nil && pr.Port > 0 {
		if pr.Port > 65535 {
			return 0, fmt.Errorf("port %d out of valid range (1-65535)", pr.Port)
		}
		return pr.Port, nil
	}

	// Fallback to plain integer parsing
	port, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("failed to parse port from gluetun response %q: %w", trimmed, err)
	}

	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("port %d out of valid range (1-65535)", port)
	}

	return port, nil
}
