// Package webhook implements the generic-webhook platform target: each
// `mctf platform` command POSTs one JSON event to platform.url, for
// custom scoreboards. The payload contract is in docs/spec/platforms.md.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/platform"
)

// maxErrorBody caps how much of a non-2xx response body is quoted in
// the returned error.
const maxErrorBody = 512

// Platform posts sync, unsync and configure events to one endpoint.
type Platform struct {
	url       string
	display   string // url without userinfo or query, safe to print
	secret    string
	userAgent string
	client    *http.Client
	now       func() time.Time

	// EventSlug fills event_slug in unsync and configure payloads (sync
	// takes it from the request's config).
	EventSlug string
}

// New validates rawURL and returns a webhook target. Payloads carry
// flags, so plain http:// is refused unless the host is loopback or
// allowHTTP is set. A non-empty secret signs every body with
// HMAC-SHA256 in the X-MCTF-Signature header. Redirects are not
// followed, so a payload never reaches a host other than rawURL.
func New(rawURL, secret, userAgent string, allowHTTP bool) (*Platform, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		// rawURL may embed credentials, so it is not quoted back.
		return nil, errors.New("platform.url is not a valid URL (want https://host/path)")
	}
	if u.Scheme == "http" && !allowHTTP && !isLoopback(u.Hostname()) {
		return nil, errors.New("platform.url uses http:// — webhook payloads carry flags; use https or pass --insecure-http")
	}
	return &Platform{
		url:       rawURL,
		display:   displayURL(u),
		secret:    secret,
		userAgent: userAgent,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now: time.Now,
	}, nil
}

// displayURL drops userinfo, query and fragment, which may carry
// credentials, so the URL can be printed.
func displayURL(u *url.URL) string {
	d := *u
	d.User = nil
	d.RawQuery = ""
	d.ForceQuery = false
	d.Fragment = ""
	d.RawFragment = ""
	return d.String()
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Sync posts a snapshot of the filtered challenge set. There is no
// remote state to diff against, so req.AllowUpdate has no effect.
func (p *Platform) Sync(ctx context.Context, req platform.SyncRequest) error {
	payload := BuildSync(req.Config, req.Challenges, req.CategoryFilter, req.SlugFilter, p.now())
	n := len(payload.Challenges)
	if n == 0 && !req.DryRun {
		return errors.New("no challenges matched the filters — nothing to send")
	}

	if req.DryRun {
		body, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(req.Out, string(body))
		if !req.JSON {
			fmt.Fprintf(req.Out, "\ndry-run: would POST %d challenge(s) to %s\n", n, p.display)
		}
		return nil
	}

	if err := p.post(ctx, payload.Event, payload); err != nil {
		return err
	}
	if req.JSON {
		// The payload carries flags; only the result goes to stdout.
		body, err := json.Marshal(struct {
			Posted int    `json:"posted"`
			URL    string `json:"url"`
		}{n, p.display})
		if err != nil {
			return err
		}
		fmt.Fprintln(req.Out, string(body))
		return nil
	}
	fmt.Fprintf(req.Out, "posted %d challenge(s) to %s\n", n, p.display)
	return nil
}

// ResolveUnsync has nothing to look up; it describes the event Unsync
// will send.
func (p *Platform) ResolveUnsync(_ context.Context, category, slug, _ string) (platform.Target, error) {
	return platform.Target{
		Category:    category,
		Slug:        slug,
		Description: fmt.Sprintf("send unsync event for %s/%s to %s", category, slug, p.display),
		Done:        "sent unsync event",
	}, nil
}

// Unsync posts an unsync event for the target.
func (p *Platform) Unsync(ctx context.Context, t platform.Target) error {
	return p.post(ctx, "unsync", UnsyncPayload{
		Envelope: newEnvelope("unsync", p.EventSlug, p.now()),
		Category: t.Category,
		Slug:     t.Slug,
	})
}

// Configure posts the event-level metadata.
func (p *Platform) Configure(ctx context.Context, ev platform.EventConfig) error {
	return p.post(ctx, "configure", ConfigurePayload{
		Envelope:    newEnvelope("configure", p.EventSlug, p.now()),
		Name:        ev.Name,
		Description: ev.Description,
		StartAt:     ev.StartAt,
		EndAt:       ev.EndAt,
	})
}

func (p *Platform) post(ctx context.Context, event string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("webhook %s: encode payload: %w", event, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook %s: %w", event, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("X-MCTF-Event", event)
	if p.secret != "" {
		mac := hmac.New(sha256.New, []byte(p.secret))
		mac.Write(body)
		req.Header.Set("X-MCTF-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		// *url.Error quotes the full URL, query string included.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("webhook %s: POST %s: %w", event, p.display, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("webhook %s: HTTP %d: %s", event, resp.StatusCode, snippet)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
