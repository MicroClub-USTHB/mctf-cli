package cmd

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/instancer"
)

func TestInspectContainers_PreservesOrderAndMarksMissing(t *testing.T) {
	// Override the docker shell-out with a stub that knows about two
	// container IDs but not the third.
	prev := inspectContainersFn
	t.Cleanup(func() { inspectContainersFn = prev })
	inspectContainersFn = func(_ context.Context, ids []string) ([]containerState, error) {
		known := map[string]containerState{
			"aaaaaaaaaaaa1234": {ID: "aaaaaaaaaaaa1234", Name: "x-app-1", Status: "Up 5 minutes (healthy)", Image: "alpine:3.20", Found: true},
			"bbbbbbbbbbbb5678": {ID: "bbbbbbbbbbbb5678", Name: "x-db-1", Status: "Up 5 minutes", Image: "postgres:16", Found: true},
		}
		out := make([]containerState, 0, len(ids))
		for _, id := range ids {
			if cs, ok := known[id]; ok {
				out = append(out, cs)
				continue
			}
			out = append(out, containerState{ID: id, Status: "(gone)"})
		}
		return out, nil
	}

	got, err := inspectContainersFn(context.Background(),
		[]string{"aaaaaaaaaaaa1234", "ccccccccccccdead", "bbbbbbbbbbbb5678"})
	if err != nil {
		t.Fatal(err)
	}
	want := []containerState{
		{ID: "aaaaaaaaaaaa1234", Name: "x-app-1", Status: "Up 5 minutes (healthy)", Image: "alpine:3.20", Found: true},
		{ID: "ccccccccccccdead", Status: "(gone)"},
		{ID: "bbbbbbbbbbbb5678", Name: "x-db-1", Status: "Up 5 minutes", Image: "postgres:16", Found: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestPrintInstancePs_Format(t *testing.T) {
	inst := instancer.Instance{
		ID:            "welcome-team1-abc",
		ChallengeSlug: "misc/welcome",
		TeamID:        "team1",
		Ports:         map[int]int{8000: 31044},
		SpawnedAt:     time.Now().Add(-12 * time.Minute).UTC(),
		ExpiresAt:     time.Now().Add(30 * time.Minute).UTC(),
		Flag:          "mctf{should_not_be_printed}",
	}
	states := []containerState{
		{ID: "abc123def4567890", Name: "welcome-team1-abc-app-1", Status: "Up 12 minutes (healthy)", Image: "python:3.12-alpine", Found: true},
	}
	var buf bytes.Buffer
	if err := printInstancePs(&buf, inst, states); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Required fields
	for _, want := range []string{
		"welcome-team1-abc",
		"misc/welcome",
		"team1",
		"8000->31044",
		"abc123def456", // 12-char truncation of container ID
		"Up 12 minutes (healthy)",
		"python:3.12-alpine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}

	// Flag must not leak through `ps` output — that's what
	// `instance flag` is for.
	if strings.Contains(out, "mctf{") {
		t.Errorf("flag must not appear in ps output:\n%s", out)
	}
}

func TestPrintInstancePs_NoContainers(t *testing.T) {
	inst := instancer.Instance{
		ID:            "x",
		ChallengeSlug: "misc/x",
		SpawnedAt:     time.Now().UTC(),
		ExpiresAt:     time.Now().Add(30 * time.Minute).UTC(),
	}
	var buf bytes.Buffer
	if err := printInstancePs(&buf, inst, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no containers recorded") {
		t.Errorf("expected empty-containers note, got:\n%s", buf.String())
	}
}

func TestPrintInstancePs_ExpiredShown(t *testing.T) {
	inst := instancer.Instance{
		ID:            "old-team-xyz",
		ChallengeSlug: "misc/old",
		SpawnedAt:     time.Now().Add(-2 * time.Hour).UTC(),
		ExpiresAt:     time.Now().Add(-30 * time.Minute).UTC(),
	}
	var buf bytes.Buffer
	if err := printInstancePs(&buf, inst, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "expired ") {
		t.Errorf("expected 'expired' indicator for past expiry, got:\n%s", buf.String())
	}
}

func TestScrubFlag_BlanksFlagOnly(t *testing.T) {
	in := instancer.Instance{
		ID:            "x",
		ChallengeSlug: "misc/x",
		Flag:          "mctf{secret}",
		Ports:         map[int]int{1: 2},
	}
	got := scrubFlag(in)
	if got.Flag != "" {
		t.Errorf("Flag should be blanked, got %q", got.Flag)
	}
	if got.ID != in.ID || got.ChallengeSlug != in.ChallengeSlug {
		t.Errorf("non-flag fields must be preserved: %+v", got)
	}
	// Caller's record must not be mutated (scrub returns a copy).
	if in.Flag == "" {
		t.Errorf("scrubFlag must not mutate its argument")
	}
}
