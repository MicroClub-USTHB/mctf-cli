package rctf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxErrorBody caps how much of a non-2xx response body is quoted in errors.
const maxErrorBody = 512

// Client communicates with the rCTF REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient validates rawURL and returns an authenticated rCTF API client.
func NewClient(rawURL, token string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("platform.url is not a valid URL (want https://host or http://host)")
	}

	return &Client{
		baseURL: strings.TrimRight(rawURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// ListChallenges returns all challenges configured on the rCTF instance.
func (c *Client) ListChallenges(ctx context.Context) ([]Challenge, error) {
	endpoint := c.baseURL + "/api/v1/challs"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	var resp Response[[]Challenge]
	if err := c.do(req, &resp); err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	return resp.Data, nil
}

// PutChallenge creates or updates a challenge on the rCTF instance.
func (c *Client) PutChallenge(ctx context.Context, id string, chall Challenge) (*Challenge, error) {
	endpoint := fmt.Sprintf("%s/api/v1/challs/%s", c.baseURL, url.PathEscape(id))
	body, err := json.Marshal(chall)
	if err != nil {
		return nil, fmt.Errorf("encode challenge payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp Response[Challenge]
	if err := c.do(req, &resp); err != nil {
		return nil, fmt.Errorf("put challenge %q: %w", id, err)
	}
	return &resp.Data, nil
}

// DeleteChallenge deletes a challenge from the rCTF instance.
func (c *Client) DeleteChallenge(ctx context.Context, id string) error {
	endpoint := fmt.Sprintf("%s/api/v1/challs/%s", c.baseURL, url.PathEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	var resp Response[any]
	if err := c.do(req, &resp); err != nil {
		return fmt.Errorf("delete challenge %q: %w", id, err)
	}
	return nil
}

// Configure updates event-level metadata (name, start/end timestamps).
func (c *Client) Configure(ctx context.Context, meta ConfigMeta) error {
	endpoint := c.baseURL + "/api/v1/admin/config"
	body, err := json.Marshal(AdminConfigUpdate{Meta: meta})
	if err != nil {
		return fmt.Errorf("encode config payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp Response[any]
	if err := c.do(req, &resp); err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	return nil
}

// do sends the request with authentication headers and parses the JSON response.
func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && (errResp.Message != "" || errResp.Kind != "") {
			return fmt.Errorf("HTTP %d (%s): %s", resp.StatusCode, errResp.Kind, errResp.Message)
		}
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > maxErrorBody {
			snippet = snippet[:maxErrorBody] + "..."
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
