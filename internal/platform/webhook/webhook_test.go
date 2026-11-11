package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform"
)

var _ platform.Platform = (*Platform)(nil)

type captured struct {
	method string
	header http.Header
	body   []byte
}

type recorder struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []captured
}

func newRecorder(t *testing.T, status int, respBody string) *recorder {
	t.Helper()
	rec := &recorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, captured{method: r.Method, header: r.Header.Clone(), body: body})
		rec.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (r *recorder) requests() []captured {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]captured(nil), r.reqs...)
}

func newTestPlatform(t *testing.T, url, secret string) *Platform {
	t.Helper()
	p, err := New(url, secret, "mctf/test", false)
	if err != nil {
		t.Fatal(err)
	}
	p.now = func() time.Time { return fixedNow }
	return p
}

func syncReq(out io.Writer) platform.SyncRequest {
	return platform.SyncRequest{
		Config:     testConfig(),
		Challenges: []*challenge.Challenge{testChallenge("misc", "a")},
		Out:        out,
	}
}

func TestNew_URLRules(t *testing.T) {
	cases := []struct {
		url       string
		allowHTTP bool
		wantErr   string
	}{
		{"https://x.example/h", false, ""},
		{"http://127.0.0.1:9/h", false, ""},
		{"http://localhost/h", false, ""},
		{"http://[::1]/h", false, ""},
		{"http://x.example/h", false, "--insecure-http"},
		{"http://x.example/h", true, ""},
		{"ftp://x.example/h", false, "not a valid URL"},
		{"::bad", false, "not a valid URL"},
		{"https:///nohost", false, "not a valid URL"},
	}
	for _, tc := range cases {
		_, err := New(tc.url, "", "mctf/test", tc.allowHTTP)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s allowHTTP=%v: unexpected error %v", tc.url, tc.allowHTTP, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s allowHTTP=%v: err = %v, want containing %q", tc.url, tc.allowHTTP, err, tc.wantErr)
		}
	}
}

func TestSync_PostsSignedPayload(t *testing.T) {
	rec := newRecorder(t, http.StatusOK, "")
	var out bytes.Buffer
	if err := newTestPlatform(t, rec.URL, "s3cret").Sync(context.Background(), syncReq(&out)); err != nil {
		t.Fatal(err)
	}
	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.method != http.MethodPost {
		t.Errorf("method = %s", r.method)
	}
	for k, want := range map[string]string{
		"Content-Type": "application/json",
		"X-Mctf-Event": "sync",
		"User-Agent":   "mctf/test",
	} {
		if got := r.header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(r.body)
	if got, want := r.header.Get("X-MCTF-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}
	var p SyncPayload
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatal(err)
	}
	if p.Event != "sync" || len(p.Challenges) != 1 || p.SentAt != "2026-11-11T20:00:00Z" {
		t.Errorf("payload = %+v", p)
	}
	if !strings.Contains(out.String(), "posted 1 challenge(s) to "+rec.URL) {
		t.Errorf("output = %q", out.String())
	}
}

func TestSync_NoSecretNoSignatureHeader(t *testing.T) {
	rec := newRecorder(t, http.StatusNoContent, "")
	if err := newTestPlatform(t, rec.URL, "").Sync(context.Background(), syncReq(io.Discard)); err != nil {
		t.Fatal(err)
	}
	if h := rec.requests()[0].header.Get("X-MCTF-Signature"); h != "" {
		t.Errorf("unexpected signature header %q", h)
	}
}

func TestSync_DryRunSendsNothing(t *testing.T) {
	rec := newRecorder(t, http.StatusOK, "")
	var out bytes.Buffer
	req := syncReq(&out)
	req.DryRun = true
	if err := newTestPlatform(t, rec.URL, "").Sync(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("dry-run sent %d request(s)", n)
	}
	if !strings.Contains(out.String(), `"event": "sync"`) || !strings.Contains(out.String(), "dry-run: would POST 1 challenge(s) to "+rec.URL) {
		t.Errorf("output = %q", out.String())
	}
}

func TestSync_DryRunJSONPrintsPayloadOnly(t *testing.T) {
	rec := newRecorder(t, http.StatusOK, "")
	var out bytes.Buffer
	req := syncReq(&out)
	req.JSON = true
	req.DryRun = true
	if err := newTestPlatform(t, rec.URL, "").Sync(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var p SyncPayload
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		t.Fatalf("--json output is not the payload: %v\n%s", err, out.String())
	}
}

func TestSync_EmptySnapshotRefused(t *testing.T) {
	rec := newRecorder(t, http.StatusOK, "")
	req := syncReq(io.Discard)
	req.CategoryFilter = "nope"
	err := newTestPlatform(t, rec.URL, "").Sync(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "nothing to send") {
		t.Errorf("err = %v", err)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("sent %d request(s)", n)
	}
}

func TestSync_Non2xxTruncated(t *testing.T) {
	rec := newRecorder(t, http.StatusInternalServerError, strings.Repeat("x", 2000))
	err := newTestPlatform(t, rec.URL, "s3cret").Sync(context.Background(), syncReq(io.Discard))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "webhook sync: HTTP 500: ") || len(msg) > 600 || strings.Contains(msg, "s3cret") {
		t.Errorf("err = %q (len %d)", msg, len(msg))
	}
}

func TestUnsync_Body(t *testing.T) {
	rec := newRecorder(t, http.StatusOK, "")
	p := newTestPlatform(t, rec.URL, "")
	p.EventSlug = "mctf-5"
	tgt, err := p.ResolveUnsync(context.Background(), "web", "a", "A")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Description != "send unsync event for web/a to "+rec.URL || tgt.Done != "sent unsync event" {
		t.Errorf("target = %+v", tgt)
	}
	if n := len(rec.requests()); n != 0 {
		t.Fatalf("ResolveUnsync sent %d request(s)", n)
	}
	if err := p.Unsync(context.Background(), tgt); err != nil {
		t.Fatal(err)
	}
	r := rec.requests()[0]
	if r.header.Get("X-MCTF-Event") != "unsync" {
		t.Errorf("event header = %q", r.header.Get("X-MCTF-Event"))
	}
	var body map[string]any
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	if body["event"] != "unsync" || body["category"] != "web" || body["slug"] != "a" || body["event_slug"] != "mctf-5" {
		t.Errorf("body = %s", r.body)
	}
}

func TestConfigure_Body(t *testing.T) {
	rec := newRecorder(t, http.StatusOK, "")
	err := newTestPlatform(t, rec.URL, "").Configure(context.Background(), platform.EventConfig{Name: "N", StartAt: "2026-10-15T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	r := rec.requests()[0]
	if r.header.Get("X-MCTF-Event") != "configure" {
		t.Errorf("event header = %q", r.header.Get("X-MCTF-Event"))
	}
	var body map[string]any
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "N" || body["start_at"] != "2026-10-15T10:00:00Z" {
		t.Errorf("body = %s", r.body)
	}
	if _, ok := body["description"]; ok {
		t.Errorf("empty description should be omitted: %s", r.body)
	}
}

func TestSync_RedirectNotFollowed(t *testing.T) {
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		var targetHits int
		mux := http.NewServeMux()
		mux.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", code)
		})
		mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, r *http.Request) {
			targetHits++
		})
		srv := httptest.NewServer(mux)
		err := newTestPlatform(t, srv.URL+"/hook", "s3cret").Sync(context.Background(), syncReq(io.Discard))
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "HTTP "+strconv.Itoa(code)) {
			t.Errorf("%d: err = %v, want HTTP %d error", code, err, code)
		}
		if targetHits != 0 {
			t.Errorf("%d: redirect target received %d request(s)", code, targetHits)
		}
	}
}

func TestURLSecretsNotPrinted(t *testing.T) {
	const raw = "http://user:pw-secret@127.0.0.1:1/h?token=q-secret"
	assertClean := func(what, s string) {
		t.Helper()
		if strings.Contains(s, "pw-secret") || strings.Contains(s, "q-secret") {
			t.Errorf("%s leaks URL secret: %q", what, s)
		}
	}
	p := newTestPlatform(t, raw, "")

	var out bytes.Buffer
	req := syncReq(&out)
	req.DryRun = true
	if err := p.Sync(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	assertClean("dry-run output", out.String())

	tgt, _ := p.ResolveUnsync(context.Background(), "web", "a", "")
	assertClean("unsync description", tgt.Description)

	err := p.Sync(context.Background(), syncReq(io.Discard)) // port 1: connection refused
	if err == nil {
		t.Fatal("expected transport error")
	}
	assertClean("transport error", err.Error())

	_, err = New("https://user:pw-secret@x.example/h?token=q-secret\x7f", "", "mctf/test", false)
	if err == nil {
		t.Fatal("expected invalid URL error")
	}
	assertClean("invalid URL error", err.Error())
}

func TestSync_JSONRealPrintsResultWithoutFlags(t *testing.T) {
	rec := newRecorder(t, http.StatusOK, "")
	var out bytes.Buffer
	req := syncReq(&out)
	req.JSON = true
	req.Challenges[0].Manifest.Challenge.Flags = []challenge.Flag{{Flag: "mctf{do-not-log}"}}
	if err := newTestPlatform(t, rec.URL, "").Sync(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "mctf{do-not-log}") {
		t.Errorf("--json real sync printed flags: %s", out.String())
	}
	var res struct {
		Posted int `json:"posted"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil || res.Posted != 1 {
		t.Errorf("output = %q (err %v), want {\"posted\":1,...}", out.String(), err)
	}
}
