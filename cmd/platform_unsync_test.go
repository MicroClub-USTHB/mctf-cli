package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/platform/ctfd"
)

func TestFindUnsyncTarget_ByManifestName(t *testing.T) {
	existing := []ctfd.Challenge{
		{ID: 1, Category: "web", Name: "cub3"},
		{ID: 2, Category: "misc", Name: "Welcome"},
		{ID: 3, Category: "misc", Name: "Other"},
	}
	got := findUnsyncTarget(existing, "misc", "welcome", "Welcome")
	if got == nil || got.ID != 2 {
		t.Fatalf("expected to match id=2 (misc/Welcome), got %+v", got)
	}
}

func TestFindUnsyncTarget_FallsBackToSlug(t *testing.T) {
	// Local challenge gone (wantName == "") → match by category +
	// slug-from-name.
	existing := []ctfd.Challenge{
		{ID: 1, Category: "misc", Name: "Welcome"},
		{ID: 2, Category: "web", Name: "cub3"},
	}
	got := findUnsyncTarget(existing, "misc", "welcome", "")
	if got == nil || got.ID != 1 {
		t.Fatalf("expected to match misc/welcome via slug, got %+v", got)
	}
}

func TestFindUnsyncTarget_CategoryMustMatch(t *testing.T) {
	// Same name in two categories — the wrong category must NOT match.
	existing := []ctfd.Challenge{
		{ID: 1, Category: "web", Name: "welcome"},
		{ID: 2, Category: "misc", Name: "welcome"},
	}
	got := findUnsyncTarget(existing, "misc", "welcome", "welcome")
	if got == nil || got.ID != 2 {
		t.Fatalf("expected to match the misc/welcome (id=2), got %+v", got)
	}
}

func TestFindUnsyncTarget_NoMatch(t *testing.T) {
	existing := []ctfd.Challenge{
		{ID: 1, Category: "web", Name: "cub3"},
	}
	if got := findUnsyncTarget(existing, "misc", "welcome", "Welcome"); got != nil {
		t.Errorf("expected no match, got %+v", got)
	}
}

func TestFindUnsyncTarget_NameMatchPreferredOverSlug(t *testing.T) {
	// If both name and slug could match different entries, the explicit
	// manifest name wins (deterministic — the slug fallback only runs
	// when the name lookup found nothing).
	existing := []ctfd.Challenge{
		{ID: 1, Category: "misc", Name: "Welcome"}, // matches slug "welcome"
		{ID: 2, Category: "misc", Name: "Welcome!"},
	}
	got := findUnsyncTarget(existing, "misc", "welcome", "Welcome!")
	if got == nil || got.ID != 2 {
		t.Fatalf("expected exact-name match (id=2), got %+v", got)
	}
}

func TestConfirmYes(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"  yes  \n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"maybe\n", false},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		got := confirmYes(strings.NewReader(tc.in), &out, "delete?")
		if got != tc.want {
			t.Errorf("confirmYes(%q) = %v, want %v", tc.in, got, tc.want)
		}
		if !strings.Contains(out.String(), "delete? [y/N]") {
			t.Errorf("prompt should appear in writer, got: %q", out.String())
		}
	}
}
