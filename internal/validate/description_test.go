package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// fixtureChallenge builds a manifest+filesystem skeleton large enough
// to clear the schema-level checks in checkOne so the W010 rule can be
// exercised in isolation.
func fixtureChallenge(t *testing.T, root, slug, description string) *challenge.Challenge {
	t.Helper()
	chRoot := filepath.Join(root, "misc", slug)
	chDir := filepath.Join(chRoot, "challenge")
	solDir := filepath.Join(chRoot, "solution")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(solDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(solDir, "writeup.md"), []byte("# w\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &challenge.Challenge{
		Slug: slug, CategoryDir: "misc", RootDir: chRoot, ChallengeDir: chDir,
		SolutionDir: solDir,
		AssetsDir:   filepath.Join(chRoot, "assets"),
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: slug, Author: "a", Category: "misc", Difficulty: "easy",
				Wave: 1, Description: description,
				Flags:      []challenge.Flag{{Flag: "mctf{x}", CaseSensitive: true}},
				NeedDeploy: false,
			},
		},
		Archetype: challenge.ArchetypeStatic,
	}
}

func TestW010_ShortDescriptionOK(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "ok", "short and sweet")
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W010", SeverityWarn) || hasRule(r, "W010", SeverityError) {
		t.Errorf("W010 should not fire for short description: %s", r.String())
	}
}

func TestW010_LongDescriptionWarns(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "long", strings.Repeat("a", 1600))
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W010", SeverityWarn) {
		t.Errorf("W010 should warn at >1500 chars: %s", r.String())
	}
}

func TestW010_VeryLongDescriptionFails(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "vlong", strings.Repeat("a", 4100))
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W010", SeverityError) {
		t.Errorf("W010 should hard-fail at >4000 chars: %s", r.String())
	}
}
