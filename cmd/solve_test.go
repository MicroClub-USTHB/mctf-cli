package cmd

import (
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/instancer"
)

// fakeStore is an in-memory instanceGetter for resolution tests.
type fakeStore map[string]instancer.Instance

func (f fakeStore) Get(id string) (instancer.Instance, bool) {
	v, ok := f[id]
	return v, ok
}

func mkChalls(keys ...string) []*challenge.Challenge {
	out := make([]*challenge.Challenge, 0, len(keys))
	for _, k := range keys {
		cat, slug, _ := splitCategorySlug(k)
		out = append(out, &challenge.Challenge{CategoryDir: cat, Slug: slug})
	}
	return out
}

func TestResolveInstanceTarget_OK(t *testing.T) {
	store := fakeStore{
		"id-1": {
			ID:            "id-1",
			ChallengeSlug: "web/stateful",
			TeamID:        "team1",
			Ports:         map[int]int{8000: 31044},
			Flag:          "mctf{per_team}",
		},
	}
	challs := mkChalls("misc/welcome", "web/stateful")

	c, target, err := resolveInstanceTarget(store, challs, "id-1")
	if err != nil {
		t.Fatal(err)
	}
	if c.CategoryDir != "web" || c.Slug != "stateful" {
		t.Errorf("resolved wrong challenge: %s/%s", c.CategoryDir, c.Slug)
	}
	if target.ID != "id-1" || target.TeamID != "team1" || target.Flag != "mctf{per_team}" {
		t.Errorf("target fields wrong: %+v", target)
	}
	if target.Ports[8000] != 31044 {
		t.Errorf("ports not carried over: %+v", target.Ports)
	}
}

func TestResolveInstanceTarget_NoSuchInstance(t *testing.T) {
	_, _, err := resolveInstanceTarget(fakeStore{}, nil, "missing")
	if err == nil || !strings.Contains(err.Error(), `no instance with id "missing"`) {
		t.Errorf("want no-instance error, got %v", err)
	}
}

func TestResolveInstanceTarget_UnknownChallenge(t *testing.T) {
	store := fakeStore{"id-1": {ID: "id-1", ChallengeSlug: "web/gone"}}
	_, _, err := resolveInstanceTarget(store, mkChalls("misc/welcome"), "id-1")
	if err == nil || !strings.Contains(err.Error(), "references unknown challenge") {
		t.Errorf("want unknown-challenge error, got %v", err)
	}
}

func TestResolveInstanceTarget_MalformedSlug(t *testing.T) {
	store := fakeStore{"id-1": {ID: "id-1", ChallengeSlug: "noslash"}}
	_, _, err := resolveInstanceTarget(store, nil, "id-1")
	if err == nil || !strings.Contains(err.Error(), "malformed challenge slug") {
		t.Errorf("want malformed-slug error, got %v", err)
	}
}
