package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"MCTF 6.0":               "mctf-6-0",
		"  Hello   World!  ":     "hello-world",
		"":                       "event",
		"already-good":           "already-good",
		"Mixed_Case_Stuff/Slash": "mixed-case-stuff-slash",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEvent_WritesExpectedTree(t *testing.T) {
	dir := t.TempDir()
	vars := NewEventVars("MCTF 6.0")
	vars.Domain = "mctf.example.com"
	res, err := Event(dir, vars, EventOptions{})
	if err != nil {
		t.Fatalf("Event: %v", err)
	}
	if len(res.Written) == 0 {
		t.Fatalf("no files written")
	}

	wantFiles := []string{
		"mtcf.config.yml",
		"README.md",
		".gitignore",
		".env.example",
		".github/workflows/ci.yml",
		"templates/challenge_name/challenge/challenge.yml",
		"templates/challenge_name/challenge/Dockerfile",
		"templates/challenge_name/challenge/docker-compose.yml",
		"templates/challenge_name/challenge/src/app.py",
		"templates/challenge_name/solution/writeup.md",
		"templates/challenge_name/solution/hints.md",
		"templates/challenge_name/other/README.md",
		"templates/challenge_name/assets/.gitkeep",
		"challenges/.gitkeep",
	}
	for _, rel := range wantFiles {
		p := filepath.Join(dir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing file %s: %v", rel, err)
		}
	}

	cfg, err := os.ReadFile(filepath.Join(dir, "mtcf.config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(cfg)
	if !strings.Contains(body, `name: "MCTF 6.0"`) {
		t.Errorf("event name not interpolated: %s", body)
	}
	if !strings.Contains(body, `slug: "mctf-6-0"`) {
		t.Errorf("event slug not interpolated: %s", body)
	}
	if !strings.Contains(body, `domain: "mctf.example.com"`) {
		t.Errorf("domain not interpolated: %s", body)
	}
}

func TestEvent_RefusesIfInitialized(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mtcf.config.yml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Event(dir, NewEventVars("Existing"), EventOptions{})
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("want ErrAlreadyInitialized, got %v", err)
	}
}

func TestEvent_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mtcf.config.yml"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Event(dir, NewEventVars("New"), EventOptions{Force: true})
	if err != nil {
		t.Fatalf("Event: %v", err)
	}
	if len(res.Written) == 0 {
		t.Fatalf("no files written under --force")
	}
	body, err := os.ReadFile(filepath.Join(dir, "mtcf.config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `name: "New"`) {
		t.Errorf("config not overwritten: %s", body)
	}
}

func TestEvent_NoForceSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed README.md so it's a partial init.
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# pre-existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Event(dir, NewEventVars("Test"), EventOptions{})
	if err != nil {
		t.Fatalf("Event: %v", err)
	}
	body, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "# pre-existing") {
		t.Errorf("pre-existing README was overwritten")
	}
	found := false
	for _, p := range res.Skipped {
		if p == readme {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("README.md not reported as skipped: %v", res.Skipped)
	}
}
