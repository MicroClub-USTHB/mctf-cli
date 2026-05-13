package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/generate"
)

func TestGenerateCheckJSON_InSync(t *testing.T) {
	body, err := generateCheckJSON(nil, "/event")
	if err != nil {
		t.Fatal(err)
	}
	var got generateReport
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if got.Mode != "check" {
		t.Errorf("mode = %q, want check", got.Mode)
	}
	if !got.InSync {
		t.Errorf("in_sync should be true with no drift")
	}
	if len(got.Drift) != 0 {
		t.Errorf("drift should be empty, got %v", got.Drift)
	}
}

func TestGenerateCheckJSON_WithDrift(t *testing.T) {
	drifts := []generate.Drift{
		{Path: "/event/challenges/web/cub3/challenge.yml", Reason: "stale"},
		{Path: "/event/challenges/misc/welcome/instance.toml", Reason: "missing"},
	}
	body, err := generateCheckJSON(drifts, "/event")
	if err != nil {
		t.Fatal(err)
	}
	var got generateReport
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if got.InSync {
		t.Error("in_sync should be false when drift present")
	}
	if len(got.Drift) != 2 {
		t.Fatalf("expected 2 drift entries, got %d", len(got.Drift))
	}
	// Paths must be relative to eventRoot.
	for _, d := range got.Drift {
		if strings.HasPrefix(d.Path, "/") {
			t.Errorf("path should be relative, got %q", d.Path)
		}
	}
	// Sorted alphabetically by path so CI diffs stay stable.
	if got.Drift[0].Path > got.Drift[1].Path {
		t.Errorf("drift not sorted: %+v", got.Drift)
	}
}

func TestGenerateApplyJSON_Written(t *testing.T) {
	written := []string{
		"/event/challenges/web/cub3/challenge.yml",
		"/event/challenges/misc/welcome/challenge.yml",
	}
	removed := []string{"/event/challenges/web/old/instance.toml"}
	body, err := generateApplyJSON(written, removed, "/event")
	if err != nil {
		t.Fatal(err)
	}
	var got generateReport
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if got.Mode != "apply" {
		t.Errorf("mode = %q", got.Mode)
	}
	if !got.InSync {
		t.Error("apply mode should always report in_sync=true on success")
	}
	if len(got.Written) != 2 || len(got.Removed) != 1 {
		t.Errorf("written=%d removed=%d, want 2/1", len(got.Written), len(got.Removed))
	}
	if got.Written[0] > got.Written[1] {
		t.Errorf("written paths not sorted: %v", got.Written)
	}
	// No absolute paths leaked.
	for _, p := range append(got.Written, got.Removed...) {
		if strings.HasPrefix(p, "/") {
			t.Errorf("path should be relative, got %q", p)
		}
	}
}

func TestGenerateApplyJSON_NothingToDo(t *testing.T) {
	body, err := generateApplyJSON(nil, nil, "/event")
	if err != nil {
		t.Fatal(err)
	}
	var got generateReport
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if got.Mode != "apply" {
		t.Errorf("mode = %q", got.Mode)
	}
	if len(got.Written) != 0 || len(got.Removed) != 0 {
		t.Errorf("expected empty written/removed, got %+v", got)
	}
	// `in_sync: true` and empty arrays — clean idempotent run.
	if !got.InSync {
		t.Error("in_sync should be true when nothing changed")
	}
}
