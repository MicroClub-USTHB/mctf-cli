// Package ctfd is a thin, typed wrapper around the CTFd REST API.
//
// Scope is intentionally narrow: just the endpoints needed by
// `mtcf platform sync` (challenges, flags, tags, hints). File upload
// and team/user APIs are not in v0; they ship as follow-ups.
package ctfd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to a CTFd instance over HTTPS using a bearer token.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a Client with sensible defaults (15s timeout, real HTTP).
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Challenge is the CTFd-side challenge view, mapped to our needs.
type Challenge struct {
	ID          int    `json:"id,omitempty"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Type        string `json:"type"` // "standard" | "dynamic"
	State       string `json:"state,omitempty"`
	Value       *int   `json:"value,omitempty"`

	// Dynamic-scoring fields. Only set when Type == "dynamic".
	Initial *int `json:"initial,omitempty"`
	Decay   *int `json:"decay,omitempty"`
	Minimum *int `json:"minimum,omitempty"`

	ConnectionInfo string `json:"connection_info,omitempty"`
}

// Flag is one CTFd flag (always type "static" for v0).
type Flag struct {
	ID          int    `json:"id,omitempty"`
	ChallengeID int    `json:"challenge_id,omitempty"`
	Type        string `json:"type"`            // "static"
	Content     string `json:"content"`
	Data        string `json:"data,omitempty"`  // "case_sensitive" | "" (CTFd convention)
}

// Tag is one CTFd tag.
type Tag struct {
	ID          int    `json:"id,omitempty"`
	ChallengeID int    `json:"challenge_id,omitempty"`
	Value       string `json:"value"`
}

// Hint is one cost-tiered CTFd hint.
type Hint struct {
	ID          int    `json:"id,omitempty"`
	ChallengeID int    `json:"challenge_id,omitempty"`
	Title       string `json:"title,omitempty"`
	Content     string `json:"content"`
	Cost        int    `json:"cost"`
}

// envelope is CTFd's standard response wrapper.
type envelope[T any] struct {
	Success bool          `json:"success"`
	Data    T             `json:"data"`
	Errors  any           `json:"errors,omitempty"`
	Message string        `json:"message,omitempty"`
}

// ListChallenges returns every challenge visible to the token (admin view).
func (c *Client) ListChallenges(ctx context.Context) ([]Challenge, error) {
	var env envelope[[]Challenge]
	if err := c.do(ctx, "GET", "/api/v1/challenges?view=admin", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// GetChallenge fetches one challenge by id.
func (c *Client) GetChallenge(ctx context.Context, id int) (*Challenge, error) {
	var env envelope[Challenge]
	if err := c.do(ctx, "GET", fmt.Sprintf("/api/v1/challenges/%d", id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// CreateChallenge creates a challenge and returns it with the assigned id.
func (c *Client) CreateChallenge(ctx context.Context, in Challenge) (*Challenge, error) {
	var env envelope[Challenge]
	if err := c.do(ctx, "POST", "/api/v1/challenges", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// UpdateChallenge PATCHes the named fields onto an existing challenge.
func (c *Client) UpdateChallenge(ctx context.Context, id int, in Challenge) (*Challenge, error) {
	var env envelope[Challenge]
	if err := c.do(ctx, "PATCH", fmt.Sprintf("/api/v1/challenges/%d", id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ListFlags returns flags currently attached to a challenge.
func (c *Client) ListFlags(ctx context.Context, challengeID int) ([]Flag, error) {
	var env envelope[[]Flag]
	if err := c.do(ctx, "GET", fmt.Sprintf("/api/v1/challenges/%d/flags", challengeID), nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// AddFlag creates a flag attached to challengeID.
func (c *Client) AddFlag(ctx context.Context, challengeID int, content string, caseSensitive bool) (*Flag, error) {
	in := Flag{
		ChallengeID: challengeID,
		Type:        "static",
		Content:     content,
	}
	if caseSensitive {
		in.Data = "case_sensitive"
	}
	var env envelope[Flag]
	if err := c.do(ctx, "POST", "/api/v1/flags", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// DeleteFlag removes a flag by id.
func (c *Client) DeleteFlag(ctx context.Context, flagID int) error {
	return c.do(ctx, "DELETE", fmt.Sprintf("/api/v1/flags/%d", flagID), nil, nil)
}

// ListTags returns tags attached to a challenge.
func (c *Client) ListTags(ctx context.Context, challengeID int) ([]Tag, error) {
	var env envelope[[]Tag]
	if err := c.do(ctx, "GET", fmt.Sprintf("/api/v1/challenges/%d/tags", challengeID), nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// AddTag creates a tag attached to challengeID.
func (c *Client) AddTag(ctx context.Context, challengeID int, value string) (*Tag, error) {
	var env envelope[Tag]
	if err := c.do(ctx, "POST", "/api/v1/tags", Tag{ChallengeID: challengeID, Value: value}, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// DeleteTag removes a tag by id.
func (c *Client) DeleteTag(ctx context.Context, tagID int) error {
	return c.do(ctx, "DELETE", fmt.Sprintf("/api/v1/tags/%d", tagID), nil, nil)
}

// ListHints returns hints attached to a challenge.
func (c *Client) ListHints(ctx context.Context, challengeID int) ([]Hint, error) {
	var env envelope[[]Hint]
	if err := c.do(ctx, "GET", fmt.Sprintf("/api/v1/challenges/%d/hints", challengeID), nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// AddHint creates a hint attached to challengeID.
func (c *Client) AddHint(ctx context.Context, challengeID, cost int, content string) (*Hint, error) {
	in := Hint{ChallengeID: challengeID, Content: content, Cost: cost}
	var env envelope[Hint]
	if err := c.do(ctx, "POST", "/api/v1/hints", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// DeleteHint removes a hint by id.
func (c *Client) DeleteHint(ctx context.Context, hintID int) error {
	return c.do(ctx, "DELETE", fmt.Sprintf("/api/v1/hints/%d", hintID), nil, nil)
}

// do performs one HTTP round-trip and decodes the JSON response into out
// (which can be nil for endpoints that return no body of interest).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	url := c.BaseURL + path
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Token "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return &APIError{
			Status:  resp.StatusCode,
			Method:  method,
			Path:    path,
			Body:    string(respBody),
		}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w (body: %s)", method, path, err, truncateForError(respBody))
	}
	// CTFd sets success=false on logical errors with 200; surface those.
	type successCheck struct {
		Success *bool `json:"success"`
	}
	var sc successCheck
	if err := json.Unmarshal(respBody, &sc); err == nil && sc.Success != nil && !*sc.Success {
		return &APIError{Status: resp.StatusCode, Method: method, Path: path, Body: string(respBody)}
	}
	return nil
}

func truncateForError(b []byte) string {
	const max = 256
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}

// APIError is returned when CTFd responds with a non-2xx status or
// success=false.
type APIError struct {
	Status int
	Method string
	Path   string
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ctfd %s %s: status %d, body: %s",
		e.Method, e.Path, e.Status, truncateForError([]byte(e.Body)))
}
