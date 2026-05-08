// Package notify posts deploy/build outcome messages to optional
// webhooks. Discord is the only target in v0; the Notifier interface
// exists so future targets (Slack, generic webhook) plug in without
// touching call sites.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Notifier sends one human-readable status message somewhere.
//
// Notifiers are best-effort: a transport error from one target should
// never abort an MTCF command. Callers log + ignore.
type Notifier interface {
	Notify(ctx context.Context, msg Message) error
}

// Message is one notification payload.
type Message struct {
	// Title is shown bold at the top of the embed.
	Title string
	// Body is markdown-ish, multi-line OK.
	Body string
	// Level is "info" | "success" | "warn" | "error" — drives the
	// colored sidebar on Discord embeds.
	Level string
}

// Discord posts to a Discord-compatible webhook URL.
//
// Webhooks are typically created at Server Settings → Integrations →
// Webhooks; the URL contains a secret, so treat it as a credential.
type Discord struct {
	URL  string
	HTTP *http.Client
}

// NewDiscord returns a Discord notifier with a 10s timeout. URL of ""
// means "no-op" — see Disabled.
func NewDiscord(url string) *Discord {
	return &Discord{URL: url, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Disabled reports whether this notifier will silently no-op (useful
// to avoid building a payload when there's no webhook configured).
func (d *Discord) Disabled() bool { return strings.TrimSpace(d.URL) == "" }

// Notify posts the message as a Discord embed.
func (d *Discord) Notify(ctx context.Context, m Message) error {
	if d.Disabled() {
		return nil
	}
	embed := map[string]any{
		"title":       m.Title,
		"description": m.Body,
		"color":       colorFor(m.Level),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	body := map[string]any{
		"username": "MTCF",
		"embeds":   []any{embed},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", d.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return errors.New("discord webhook " + resp.Status + ": " + truncate(respBody))
	}
	return nil
}

// colorFor maps a Level to a Discord embed color (decimal RGB).
func colorFor(level string) int {
	switch strings.ToLower(level) {
	case "success":
		return 0x2ecc71 // green
	case "warn":
		return 0xf1c40f // yellow
	case "error":
		return 0xe74c3c // red
	default:
		return 0x3498db // blue (info)
	}
}

func truncate(b []byte) string {
	const max = 256
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
