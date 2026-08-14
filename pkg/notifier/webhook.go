package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Notifier dispatches port update notifications to configured webhooks.
type Notifier struct {
	HTTPClient *http.Client
}

// NewNotifier creates a new webhook notifier.
func NewNotifier() *Notifier {
	return &Notifier{
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type genericPayload struct {
	Event        string `json:"event"`
	PreviousPort int    `json:"previous_port"`
	NewPort      int    `json:"new_port"`
	Timestamp    string `json:"timestamp"`
}

type discordEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       int    `json:"color"`
	Timestamp   string `json:"timestamp"`
}

type discordPayload struct {
	Username string         `json:"username,omitempty"`
	Embeds   []discordEmbed `json:"embeds"`
}

// SendPortUpdate sends a webhook notification with the new forwarded port.
func (n *Notifier) SendPortUpdate(ctx context.Context, webhookURL string, prevPort, newPort int) error {
	trimmedURL := strings.TrimSpace(webhookURL)
	if trimmedURL == "" {
		return nil // No-op if webhook is not configured
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var body []byte
	var err error

	if strings.Contains(trimmedURL, "discord.com/api/webhooks") {
		payload := discordPayload{
			Username: "qBit-Gluetun Sync",
			Embeds: []discordEmbed{
				{
					Title:       "Forwarded Port Updated",
					Description: fmt.Sprintf("qBitTorrent listening port successfully updated from `%d` to `%d`.", prevPort, newPort),
					Color:       3066993, // Green
					Timestamp:   now,
				},
			},
		}
		body, err = json.Marshal(payload)
	} else {
		payload := genericPayload{
			Event:        "port_synced",
			PreviousPort: prevPort,
			NewPort:      newPort,
			Timestamp:    now,
		}
		body, err = json.Marshal(payload)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, trimmedURL, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	//nolint:gosec // URL is user-configured webhook endpoint
	resp, err := n.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute webhook request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook responded with non-2xx status code: %d", resp.StatusCode)
	}

	return nil
}
