// Package webhook provides a lightweight webhook delivery system for RafikiClaw agents.
// It allows agents to send outbound webhook events to external systems and
// provides a server to receive and dispatch webhook events to registered handlers.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Event represents a webhook event payload.
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	AgentID   string                 `json:"agentId"`
	Timestamp string                 `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	SignedBy  string                 `json:"signedBy,omitempty"`
	Signature string                 `json:"signature,omitempty"`
}

// DeliveryReport holds the result of a webhook delivery attempt.
type DeliveryReport struct {
	EventID     string    `json:"eventId"`
	StatusCode int       `json:"statusCode"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	DeliveredAt time.Time `json:"deliveredAt"`
}

// Client sends outbound webhook events.
type Client struct {
	baseURL    string
	apiKey     string
	timeout    time.Duration
	httpClient http.Client
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithClientAPIKey sets the API key used for outbound webhook requests.
func WithClientAPIKey(key string) ClientOption {
	return func(c *Client) { c.apiKey = key }
}

// WithClientTimeout sets the timeout for webhook requests.
func WithClientTimeout(d time.Duration) ClientOption {
	return func(c *Client) { c.timeout = d }
}

// NewClient creates a webhook client that sends to baseURL.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		timeout: 30 * time.Second,
		httpClient: http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Send delivers an Event to the configured webhook endpoint.
// It returns a DeliveryReport with the outcome.
func (c *Client) Send(ctx context.Context, event Event) DeliveryReport {
	if event.ID == "" {
		event.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	body, err := json.Marshal(event)
	if err != nil {
		return DeliveryReport{
			EventID: event.ID,
			Success: false,
			Error:   fmt.Sprintf("marshal event: %v", err),
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/events", bytes.NewReader(body))
	if err != nil {
		return DeliveryReport{
			EventID: event.ID,
			Success: false,
			Error:   fmt.Sprintf("create request: %v", err),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DeliveryReport{
			EventID:     event.ID,
			Success:     false,
			Error:       fmt.Sprintf("request failed: %v", err),
			DeliveredAt: time.Now(),
		}
	}
	defer resp.Body.Close()

	// Read and discard body for keep-alive
	io.Copy(io.Discard, resp.Body)

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	return DeliveryReport{
		EventID:     event.ID,
		StatusCode:  resp.StatusCode,
		Success:    success,
		DeliveredAt: time.Now(),
	}
}

// SendWithRetry delivers an event with up to maxRetries retries on failure.
func (c *Client) SendWithRetry(ctx context.Context, event Event, maxRetries int) DeliveryReport {
	var last DeliveryReport
	for attempt := 0; attempt <= maxRetries; attempt++ {
		last = c.Send(ctx, event)
		if last.Success {
			return last
		}
		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				last.Error = "context cancelled during retry"
				return last
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}
	return last
}

// NotifyTaskComplete is a helper that sends a standard task-completion webhook.
func (c *Client) NotifyTaskComplete(ctx context.Context, agentID, taskID, result string) DeliveryReport {
	event := Event{
		Type:    "task.complete",
		AgentID: agentID,
		Payload: map[string]interface{}{
			"taskId": taskID,
			"result": result,
		},
	}
	return c.Send(ctx, event)
}

// NotifyError is a helper that sends an error notification webhook.
func (c *Client) NotifyError(ctx context.Context, agentID, taskID, errMsg string) DeliveryReport {
	event := Event{
		Type:    "agent.error",
		AgentID: agentID,
		Payload: map[string]interface{}{
			"taskId": taskID,
			"error":  errMsg,
		},
	}
	return c.Send(ctx, event)
}

// BuildEvent is a helper to construct a typed webhook event from environment context.
func BuildEvent(eventType, agentID string) Event {
	return Event{
		Type:      eventType,
		AgentID:   agentID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   make(map[string]interface{}),
	}
}

// LoadClientFromEnv creates a webhook client from environment variables.
// Reads RAFIKICLAW_WEBHOOK_URL, RAFIKICLAW_WEBHOOK_KEY (optional).
func LoadClientFromEnv() *Client {
	url := os.Getenv("RAFIKICLAW_WEBHOOK_URL")
	if url == "" {
		return nil
	}
	key := os.Getenv("RAFIKICLAW_WEBHOOK_KEY")
	return NewClient(url, WithClientAPIKey(key))
}
