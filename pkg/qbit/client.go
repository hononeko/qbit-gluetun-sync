package qbit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client handles communication with the qBitTorrent API.
type Client struct {
	BaseURL  string
	Username string
	//nolint:gosec // Field name requires matching JSON payload
	Password   string
	HTTPClient *http.Client
}

// NewClient creates a new qBitTorrent client.
func NewClient(baseURL, user, pass string) *Client {
	return &Client{
		BaseURL:  baseURL,
		Username: user,
		Password: pass,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// authenticate retrieves the auth cookie, if credentials are provided.
// In many sidecar setups, auth bypass is configured for localhost,
// so this might not always be needed, but we support it.
func (c *Client) authenticate() (string, error) {
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

	req, err := http.NewRequest("POST", loginURL, bytes.NewBufferString(data.Encode()))
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

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SID" {
			return cookie.Value, nil
		}
	}

	// It's possible qBitTorrent returns an empty body ifauth is bypassed
	// or no SID cookie is set if it was already authenticated somehow,
	// but generally a successful login returns a SID cookie.
	return "", nil
}

// SetPreferences sets the given preferences in qBitTorrent.
func (c *Client) SetPreferences(preferences map[string]interface{}) error {
	cookie, err := c.authenticate()
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

	req, err := http.NewRequest("POST", prefsURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create setPreferences request: %w", err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	if cookie != "" {
		//nolint:gosec // Cookie is used for client request, not server response
		req.AddCookie(&http.Cookie{Name: "SID", Value: cookie})
	}

	//nolint:gosec // URL is internally constructed via config
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("setPreferences request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("setPreferences failed with status: %d, and failed to read body: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("setPreferences failed with status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// SetListenPort sets the listen port.
func (c *Client) SetListenPort(port int) error {
	prefs := map[string]interface{}{
		"listen_port": port,
	}
	return c.SetPreferences(prefs)
}
