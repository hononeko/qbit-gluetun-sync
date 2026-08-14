package qbit

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClientOptions configures optional settings for the qBitTorrent client.
type ClientOptions struct {
	InsecureSkipVerify bool
	CACertFile         string
	Timeout            time.Duration
	//nolint:gosec // Field name for API token
	APIKey       string
	APIKeyHeader string
}

// Client handles communication with the qBitTorrent API.
type Client struct {
	BaseURL  string
	Username string
	//nolint:gosec // Field name requires matching JSON payload
	Password string
	//nolint:gosec // Field name for API token
	APIKey       string
	APIKeyHeader string
	HTTPClient   *http.Client
}

// NewClient creates a new qBitTorrent client with default options.
func NewClient(baseURL, user, pass string) *Client {
	client, _ := NewClientWithOptions(baseURL, user, pass, ClientOptions{})
	return client
}

// NewClientWithOptions creates a new qBitTorrent client with custom TLS, timeout, and API Key options.
func NewClientWithOptions(baseURL, user, pass string, opts ClientOptions) (*Client, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: opts.InsecureSkipVerify, //nolint:gosec // Configurable for internal/self-signed certs
	}

	if opts.CACertFile != "" {
		cleanPath := filepath.Clean(opts.CACertFile)
		caCert, err := os.ReadFile(cleanPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read custom CA cert file %s: %w", cleanPath, err)
		}
		caCertPool, err := x509.SystemCertPool()
		if err != nil || caCertPool == nil {
			caCertPool = x509.NewCertPool()
		}
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append custom CA cert from %s: invalid PEM format", cleanPath)
		}
		tlsConfig.RootCAs = caCertPool
	}

	timeout := 10 * time.Second
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   5,
	}

	authHeaderName := strings.TrimSpace(opts.APIKeyHeader)
	if authHeaderName == "" {
		//nolint:gosec // Header key name, not a secret credential
		authHeaderName = "X-Api-Key"
	}

	return &Client{
		BaseURL:      baseURL,
		Username:     user,
		Password:     pass,
		APIKey:       strings.TrimSpace(opts.APIKey),
		APIKeyHeader: authHeaderName,
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
	}, nil
}

// applyAuth attaches credentials (API Key or SID session cookie) to the request.
func (c *Client) applyAuth(req *http.Request, cookie string) {
	if c.APIKey != "" {
		req.Header.Set(c.APIKeyHeader, c.APIKey)
		// If custom header is not Authorization, also set Authorization Bearer for reverse proxy compatibility
		if !strings.EqualFold(c.APIKeyHeader, "Authorization") {
			req.Header.Set("Authorization", "Bearer "+c.APIKey)
		}
		return
	}

	if cookie != "" {
		//nolint:gosec // Cookie is used for client request, not server response
		req.AddCookie(&http.Cookie{Name: "SID", Value: cookie})
	}
}

// authenticate retrieves the auth cookie if credentials are provided and API Key is not set.
func (c *Client) authenticate(ctx context.Context) (string, error) {
	if c.APIKey != "" {
		return "", nil // API Key auth takes precedence, bypasses login endpoint
	}

	if c.Username == "" && c.Password == "" {
		return "", nil // No auth required
	}

	data := url.Values{}
	data.Set("username", c.Username)
	data.Set("password", c.Password)

	loginURL, err := url.JoinPath(c.BaseURL, "/api/v2/auth/login")
	if err != nil {
		return "", fmt.Errorf("failed to join login URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	//nolint:gosec // URL is internally constructed via config
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed with status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", fmt.Errorf("failed to read login response: %w", err)
	}
	bodyStr := strings.TrimSpace(string(bodyBytes))

	if strings.EqualFold(bodyStr, "Fails.") {
		return "", fmt.Errorf("login rejected by qBitTorrent: invalid credentials")
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SID" {
			return cookie.Value, nil
		}
	}

	// If credentials were provided and status is 200 without SID, ensure body was Ok
	if strings.EqualFold(bodyStr, "Ok.") {
		return "", nil
	}

	return "", fmt.Errorf("login failed: no SID cookie returned")
}

// SetPreferences sets the given preferences in qBitTorrent.
func (c *Client) SetPreferences(ctx context.Context, preferences map[string]interface{}) error {
	cookie, err := c.authenticate(ctx)
	if err != nil {
		return fmt.Errorf("authentication error: %w", err)
	}

	prefJSON, err := json.Marshal(preferences)
	if err != nil {
		return fmt.Errorf("failed to marshal preferences: %w", err)
	}

	data := url.Values{}
	data.Set("json", string(prefJSON))

	prefsURL, err := url.JoinPath(c.BaseURL, "/api/v2/app/setPreferences")
	if err != nil {
		return fmt.Errorf("failed to join setPreferences URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, prefsURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create setPreferences request: %w", err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	c.applyAuth(req, cookie)

	//nolint:gosec // URL is internally constructed via config
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("setPreferences request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("setPreferences failed with status: %d, and failed to read body: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("setPreferences failed with status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// GetPreferences retrieves the current preferences from qBitTorrent.
func (c *Client) GetPreferences(ctx context.Context) (map[string]interface{}, error) {
	cookie, err := c.authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication error: %w", err)
	}

	prefsURL, err := url.JoinPath(c.BaseURL, "/api/v2/app/preferences")
	if err != nil {
		return nil, fmt.Errorf("failed to join preferences URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, prefsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create preferences request: %w", err)
	}
	c.applyAuth(req, cookie)

	//nolint:gosec // URL is internally constructed via config
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("preferences request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("preferences request failed with status: %d", resp.StatusCode)
	}

	var prefs map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&prefs); err != nil {
		return nil, fmt.Errorf("failed to decode preferences response: %w", err)
	}

	return prefs, nil
}

// SetListenPort sets the listen port in qBitTorrent with validation.
func (c *Client) SetListenPort(ctx context.Context, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port number: %d (must be between 1 and 65535)", port)
	}

	prefs := map[string]interface{}{
		"listen_port": port,
	}
	return c.SetPreferences(ctx, prefs)
}
