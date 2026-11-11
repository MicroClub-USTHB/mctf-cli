package webhook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

var fixedNow = time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)

func testConfig() *config.Config {
	return &config.Config{
		Version: 1,
		Event: config.Event{
			Slug:    "mctf-5",
			Domain:  "ctf.example.com",
			Scoring: &config.Scoring{Type: "dynamic"},
		},
	}
}

func testChallenge(category, slug string) *challenge.Challenge {
	return &challenge.Challenge{
		Slug: slug, CategoryDir: category,
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: strings.ToUpper(slug), Author: "alice", Category: category,
				Difficulty: "easy", Wave: 1, Description: "desc",
			},
		},
		Archetype: challenge.ArchetypeStatic,
	}
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBuildSync_FullEntry(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	internal := 8000
	c := testChallenge("web", "cub3")
	c.Archetype = challenge.ArchetypeInstanced
	c.AssetsDir = assets
	c.Manifest.Challenge.NeedDeploy = true
	c.Manifest.Challenge.Flags = []challenge.Flag{{Flag: "MCTF{x}", CaseSensitive: true}}
	c.Manifest.Challenge.Hints = []challenge.Hint{{Content: "look closer", Cost: 50}}
	c.Manifest.Challenge.Tags = []string{"crypto"}
	c.Manifest.Deploy = &challenge.DeployBlock{Subdomain: "cub3", InternalPort: &internal}

	got := marshal(t, BuildSync(testConfig(), []*challenge.Challenge{c}, "", "", fixedNow))
	for _, want := range []string{
		`"schema":1`, `"event":"sync"`, `"event_slug":"mctf-5"`, `"sent_at":"2026-11-11T20:00:00Z"`,
		`"category":"web"`, `"slug":"cub3"`, `"name":"CUB3"`, `"archetype":"instanced"`, `"hidden":false`,
		`"description":"desc\n\n**Author: alice**"`,
		`"scoring":{"type":"dynamic","initial":500,"decay":10,"minimum":100}`,
		`"flags":[{"flag":"MCTF{x}","case_sensitive":true}]`,
		`"tags":["wave 1","crypto"]`,
		`"hints":[{"content":"look closer","cost":50}]`,
		`"connection_info":"https://instancer.ctf.example.com"`,
		`"files":[{"name":"a.txt","sha256":"8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4"}]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("payload missing %s\n%s", want, got)
		}
	}
}

func TestBuildSync_EmptySlicesNotNull(t *testing.T) {
	got := marshal(t, BuildSync(testConfig(), []*challenge.Challenge{testChallenge("misc", "a")}, "", "", fixedNow))
	for _, want := range []string{`"flags":[]`, `"hints":[]`, `"files":[]`} {
		if !strings.Contains(got, want) {
			t.Errorf("payload missing %s\n%s", want, got)
		}
	}
	empty := marshal(t, BuildSync(testConfig(), nil, "", "", fixedNow))
	if !strings.Contains(empty, `"challenges":[]`) {
		t.Errorf("empty payload: %s", empty)
	}
}

func TestBuildSync_FiltersAndSorts(t *testing.T) {
	challs := []*challenge.Challenge{testChallenge("web", "b"), testChallenge("web", "a"), testChallenge("pwn", "c")}
	unnamed := testChallenge("web", "z")
	unnamed.Manifest.Challenge.Name = ""
	challs = append(challs, unnamed)

	slugs := func(p SyncPayload) []string {
		var out []string
		for _, c := range p.Challenges {
			out = append(out, c.Category+"/"+c.Slug)
		}
		return out
	}
	if got := slugs(BuildSync(testConfig(), challs, "", "", fixedNow)); strings.Join(got, ",") != "pwn/c,web/a,web/b" {
		t.Errorf("all: %v", got)
	}
	if got := slugs(BuildSync(testConfig(), challs, "web", "", fixedNow)); strings.Join(got, ",") != "web/a,web/b" {
		t.Errorf("category filter: %v", got)
	}
	if got := slugs(BuildSync(testConfig(), challs, "", "c", fixedNow)); strings.Join(got, ",") != "pwn/c" {
		t.Errorf("slug filter: %v", got)
	}
}

func TestBuildSync_StaticScoringShape(t *testing.T) {
	c := testChallenge("misc", "a")
	c.Manifest.Challenge.Scoring = &challenge.Scoring{Type: "static", Points: 200}
	got := marshal(t, BuildSync(testConfig(), []*challenge.Challenge{c}, "", "", fixedNow))
	if !strings.Contains(got, `"scoring":{"type":"static","points":200}`) {
		t.Errorf("payload: %s", got)
	}
}

func TestBuildSync_SentAtUTC(t *testing.T) {
	plusOne := time.Date(2026, 11, 11, 21, 0, 0, 0, time.FixedZone("UTC+1", 3600))
	p := BuildSync(testConfig(), nil, "", "", plusOne)
	if p.SentAt != "2026-11-11T20:00:00Z" {
		t.Errorf("sent_at = %q", p.SentAt)
	}
}

func TestUnsyncAndConfigurePayloadShape(t *testing.T) {
	u := marshal(t, UnsyncPayload{Envelope: newEnvelope("unsync", "mctf-5", fixedNow), Category: "web", Slug: "a"})
	if u != `{"schema":1,"event":"unsync","event_slug":"mctf-5","sent_at":"2026-11-11T20:00:00Z","category":"web","slug":"a"}` {
		t.Errorf("unsync: %s", u)
	}
	c := marshal(t, ConfigurePayload{Envelope: newEnvelope("configure", "mctf-5", fixedNow), Name: "N"})
	if c != `{"schema":1,"event":"configure","event_slug":"mctf-5","sent_at":"2026-11-11T20:00:00Z","name":"N"}` {
		t.Errorf("configure: %s", c)
	}
}
