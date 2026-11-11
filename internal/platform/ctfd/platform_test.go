package ctfd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform"
)

var _ platform.Platform = (*Platform)(nil)

func TestFindUnsyncTarget_ByManifestName(t *testing.T) {
	existing := []Challenge{
		{ID: 1, Category: "web", Name: "cub3"},
		{ID: 2, Category: "misc", Name: "Welcome"},
		{ID: 3, Category: "misc", Name: "Other"},
	}
	got := FindUnsyncTarget(existing, "misc", "welcome", "Welcome")
	if got == nil || got.ID != 2 {
		t.Fatalf("expected to match id=2 (misc/Welcome), got %+v", got)
	}
}

func TestFindUnsyncTarget_FallsBackToSlug(t *testing.T) {
	// Local challenge gone (wantName == "") → match by category +
	// slug-from-name.
	existing := []Challenge{
		{ID: 1, Category: "misc", Name: "Welcome"},
		{ID: 2, Category: "web", Name: "cub3"},
	}
	got := FindUnsyncTarget(existing, "misc", "welcome", "")
	if got == nil || got.ID != 1 {
		t.Fatalf("expected to match misc/welcome via slug, got %+v", got)
	}
}

func TestFindUnsyncTarget_CategoryMustMatch(t *testing.T) {
	// Same name in two categories — the wrong category must NOT match.
	existing := []Challenge{
		{ID: 1, Category: "web", Name: "welcome"},
		{ID: 2, Category: "misc", Name: "welcome"},
	}
	got := FindUnsyncTarget(existing, "misc", "welcome", "welcome")
	if got == nil || got.ID != 2 {
		t.Fatalf("expected to match the misc/welcome (id=2), got %+v", got)
	}
}

func TestFindUnsyncTarget_NoMatch(t *testing.T) {
	existing := []Challenge{
		{ID: 1, Category: "web", Name: "cub3"},
	}
	if got := FindUnsyncTarget(existing, "misc", "welcome", "Welcome"); got != nil {
		t.Errorf("expected no match, got %+v", got)
	}
}

func TestFindUnsyncTarget_NameMatchPreferredOverSlug(t *testing.T) {
	// If both name and slug could match different entries, the explicit
	// manifest name wins (deterministic — the slug fallback only runs
	// when the name lookup found nothing).
	existing := []Challenge{
		{ID: 1, Category: "misc", Name: "Welcome"}, // matches slug "welcome"
		{ID: 2, Category: "misc", Name: "Welcome!"},
	}
	got := FindUnsyncTarget(existing, "misc", "welcome", "Welcome!")
	if got == nil || got.ID != 2 {
		t.Fatalf("expected exact-name match (id=2), got %+v", got)
	}
}

func TestPlatform_SyncDryRunPrintsPlan(t *testing.T) {
	srv := newSyncFake(t, syncFakeState{})
	defer srv.close()
	challs := []*challenge.Challenge{sampleStatic()}

	want, err := Diff(context.Background(), New(srv.URL, "tok"), sampleConfig(), challs, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	p := NewPlatform(srv.URL, "tok")
	if err := p.Sync(context.Background(), platform.SyncRequest{
		Config: sampleConfig(), Challenges: challs, DryRun: true, Out: &buf,
	}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != want.Render() {
		t.Errorf("output:\n%s\nwant:\n%s", buf.String(), want.Render())
	}
	if n := len(srv.snapshot().existing); n != 0 {
		t.Errorf("dry-run created %d challenge(s)", n)
	}
}

func TestPlatform_SyncJSON(t *testing.T) {
	srv := newSyncFake(t, syncFakeState{})
	defer srv.close()
	var buf bytes.Buffer
	p := NewPlatform(srv.URL, "tok")
	if err := p.Sync(context.Background(), platform.SyncRequest{
		Config: sampleConfig(), Challenges: []*challenge.Challenge{sampleStatic()},
		DryRun: true, JSON: true, Out: &buf,
	}); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, buf.String())
	}
	if len(rows) != 1 || rows[0]["action"] != "create" || rows[0]["slug"] != "crypto/rsa-baby" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestPlatform_ResolveAndUnsync(t *testing.T) {
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/challenges":
			writeEnvelope(w, []Challenge{{ID: 7, Category: "misc", Name: "Welcome"}})
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/v1/challenges/"):
			deleted = r.URL.Path
			writeEnvelope(w, nil)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := NewPlatform(srv.URL, "tok")
	tgt, err := p.ResolveUnsync(context.Background(), "misc", "welcome", "Welcome")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.ID != "7" || tgt.Description != `delete CTFd challenge id=7 category="misc" name="Welcome"` || tgt.Done != "deleted id=7" {
		t.Errorf("target = %+v", tgt)
	}
	if err := p.Unsync(context.Background(), tgt); err != nil {
		t.Fatal(err)
	}
	if deleted != "/api/v1/challenges/7" {
		t.Errorf("deleted path = %q", deleted)
	}
}

func TestPlatform_ResolveUnsyncNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, []Challenge{})
	}))
	defer srv.Close()
	_, err := NewPlatform(srv.URL, "tok").ResolveUnsync(context.Background(), "misc", "welcome", "Welcome")
	if err == nil || err.Error() != `no CTFd challenge matched misc/welcome (looked for category=misc, name="Welcome")` {
		t.Errorf("err = %v", err)
	}
}
