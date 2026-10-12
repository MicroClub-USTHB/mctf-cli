package instancer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RemoteInstance is one instance as reported by the Python instancer's admin
// API (GET /admin/api/instances). Field names match that API's JSON. Ports
// arrive as string keys because JSON object keys are always strings.
type RemoteInstance struct {
	InstanceID  string         `json:"instance_id"`
	ChallengeID string         `json:"challenge_id"`
	UserID      string         `json:"user_id"`
	Username    string         `json:"username"`
	TeamID      string         `json:"team_id"`
	TeamName    string         `json:"team_name"`
	Status      string         `json:"status"`
	Ports       map[string]int `json:"ports"`
	PublicURL   string         `json:"public_url"`
	CreatedAt   string         `json:"created_at"`
	ExpiresAt   string         `json:"expires_at"`
}

// APIClient is the control-plane client for the Python instancer's admin API.
// It authenticates with the X-Admin-Key header and is used by `mctf instance`
// when instancer.url is configured, instead of the local on-disk store.
type APIClient struct {
	baseURL  string
	adminKey string
	http     *http.Client
}

// NewAPIClient builds a client for baseURL (e.g. https://instancer.example.com)
// authenticating with adminKey. A nil http client defaults to a 15s timeout.
func NewAPIClient(baseURL, adminKey string) *APIClient {
	return &APIClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		adminKey: adminKey,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

// List returns every active instance known to the remote instancer.
func (c *APIClient) List(ctx context.Context) ([]RemoteInstance, error) {
	var resp struct {
		Instances []RemoteInstance `json:"instances"`
	}
	if err := c.do(ctx, http.MethodGet, "/admin/api/instances", &resp); err != nil {
		return nil, err
	}
	return resp.Instances, nil
}

// Destroy force-stops one instance by id and returns the server's message.
// A well-formed response with success=false is surfaced as an error.
func (c *APIClient) Destroy(ctx context.Context, id string) (string, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	path := "/admin/api/instances/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodDelete, path, &resp); err != nil {
		return "", err
	}
	if !resp.Success {
		msg := resp.Message
		if msg == "" {
			msg = "instancer reported failure"
		}
		return "", fmt.Errorf("destroy %s: %s", id, msg)
	}
	return resp.Message, nil
}

// do performs the request, sets auth headers, checks for a 2xx status, and
// decodes the JSON body into out (when non-nil).
func (c *APIClient) do(ctx context.Context, method, path string, out any) error {
	if c.baseURL == "" {
		return fmt.Errorf("instancer url is not set")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Admin-Key", c.adminKey)
	req.Header.Set("Accept", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("instancer %s %s: %s", method, path, serverError(res.StatusCode, body))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return nil
}

// serverError renders a readable message from a non-2xx response, preferring
// FastAPI's {"detail": ...} field, with a hint for the common auth failures.
func serverError(status int, body []byte) string {
	msg := fmt.Sprintf("HTTP %d", status)
	var parsed struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &parsed) == nil && len(parsed.Detail) > 0 {
		var s string
		if json.Unmarshal(parsed.Detail, &s) == nil && s != "" {
			msg = fmt.Sprintf("%s — %s", msg, s)
		} else {
			msg = fmt.Sprintf("%s — %s", msg, bytes.TrimSpace(parsed.Detail))
		}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		msg += " (check MCTF_INSTANCER_ADMIN_KEY)"
	}
	return msg
}
