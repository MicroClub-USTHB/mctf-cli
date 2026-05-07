package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChallenge_FromEmbedded(t *testing.T) {
	dir := t.TempDir()
	// Pretend EventRoot has no templates/ — fall back to embedded.
	res, err := Challenge(
		ChallengeVars{Slug: "rsa-baby", Category: "crypto", Author: "test"},
		ChallengeOptions{EventRoot: dir, ChallengesDir: "challenges"},
	)
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}
	if len(res.Written) == 0 {
		t.Fatal("no files written")
	}

	manifestPath := filepath.Join(dir, "challenges", "crypto", "rsa-baby", "challenge", "challenge.yml")
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	got := string(body)
	for _, want := range []string{
		`name: "rsa-baby"`,
		`category: crypto`,
		`difficulty: easy`,
		`author: "test"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("manifest missing %q\n%s", want, got)
		}
	}

	composeBody, err := os.ReadFile(filepath.Join(dir, "challenges", "crypto", "rsa-baby", "challenge", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(composeBody), "name: rsa-baby") {
		t.Errorf("compose name not rewritten:\n%s", composeBody)
	}

	writeup, err := os.ReadFile(filepath.Join(dir, "challenges", "crypto", "rsa-baby", "solution", "writeup.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(writeup), "# rsa-baby — writeup") {
		t.Errorf("writeup title not rewritten:\n%s", writeup)
	}
}

func TestChallenge_FromEventLocalTemplates(t *testing.T) {
	dir := t.TempDir()
	// Seed event-local template; the scaffolder must prefer it over the embed.
	tmpl := filepath.Join(dir, "templates", "challenge_name", "challenge")
	if err := os.MkdirAll(tmpl, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `schema: 1
challenge:
  name: "challenge_name"
  author: "your-handle"
  category: misc
  difficulty: easy
  description: "test"
  wave: 1
  flags:
    - { flag: "mctf{x}", case_sensitive: true }
  need_deploy: false
`
	if err := os.WriteFile(filepath.Join(tmpl, "challenge.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Challenge(
		ChallengeVars{Slug: "demo", Category: "web", Name: "Demo", Author: "alice"},
		ChallengeOptions{EventRoot: dir, ChallengesDir: "challenges"},
	)
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}
	if len(res.Written) != 1 {
		t.Fatalf("want 1 file written from event-local template, got %v", res.Written)
	}
	body, err := os.ReadFile(filepath.Join(dir, "challenges", "web", "demo", "challenge", "challenge.yml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, `name: "Demo"`) || !strings.Contains(got, "category: web") || !strings.Contains(got, `author: "alice"`) {
		t.Errorf("event-local manifest not rewritten:\n%s", got)
	}
}

func TestChallenge_RefusesIfExists(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "challenges", "misc", "welcome")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Challenge(
		ChallengeVars{Slug: "welcome", Category: "misc"},
		ChallengeOptions{EventRoot: dir, ChallengesDir: "challenges"},
	)
	if !errors.Is(err, ErrChallengeExists) {
		t.Fatalf("want ErrChallengeExists, got %v", err)
	}
}

func TestChallenge_NeedsCategoryAndSlug(t *testing.T) {
	_, err := Challenge(ChallengeVars{Slug: "foo"}, ChallengeOptions{})
	if err == nil {
		t.Error("expected error for missing category")
	}
	_, err = Challenge(ChallengeVars{Category: "web"}, ChallengeOptions{})
	if err == nil {
		t.Error("expected error for missing slug")
	}
}

func TestRewriteManifest_PreservesComments(t *testing.T) {
	in := `# top comment
schema: 1
challenge:
  name: "challenge_name"            # change this
  author: "your-handle"             # change this
  category: misc                    # one of: web | pwn | ...
  difficulty: easy                  # easy | medium | hard | extreme
  wave: 1
`
	out := rewriteManifest(in, ChallengeVars{Slug: "x", Category: "web", Name: "X", Author: "a", Difficulty: "hard"})
	for _, want := range []string{
		`# top comment`,
		`name: "X"            # change this`,
		`author: "a"             # change this`,
		`category: web                    # one of: web | pwn | ...`,
		`difficulty: hard                  # easy | medium | hard | extreme`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q\n--- got ---\n%s", want, out)
		}
	}
}
