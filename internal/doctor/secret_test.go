package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepoWith creates a temp git repo with the given files staged (tracked),
// which is all `git grep` needs — no commit (and no git user config) required.
func gitRepoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	return dir
}

func TestCheckCommittedSecrets_WarnsOnRealToken(t *testing.T) {
	dir := gitRepoWith(t, map[string]string{
		"deploy.env": "MCTF_DISCORD_TOKEN=aB3xQ9zLmN0pK7wR\n",
	})
	got := checkCommittedSecrets(dir)
	if got.Status != StatusWarn {
		t.Fatalf("status = %q, want warn (detail: %s)", got.Status, got.Detail)
	}
	if got.ID != "D043" {
		t.Errorf("id = %q, want D043", got.ID)
	}
	// Location is reported; the value must NOT be.
	if !strings.Contains(got.Detail, "deploy.env:1") {
		t.Errorf("detail should name the location, got %q", got.Detail)
	}
	if strings.Contains(got.Detail, "aB3xQ9zLmN0pK7wR") {
		t.Error("detail leaked the secret value")
	}
}

func TestCheckCommittedSecrets_IgnoresPlaceholdersAndRefs(t *testing.T) {
	cases := map[string]string{
		"placeholder": "MCTF_DISCORD_TOKEN=changeme\n",
		"your-here":   "MCTF_CTFD_TOKEN=your-token-here\n",
		"env-ref":     "MCTF_CTFD_TOKEN=${CTFD_TOKEN}\n",
		"too-short":   "MCTF_X_KEY=abc\n",
		"empty":       "MCTF_X_SECRET=\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := gitRepoWith(t, map[string]string{"a.env": content})
			if got := checkCommittedSecrets(dir); got.Status != StatusOK {
				t.Errorf("status = %q, want ok (detail: %s)", got.Status, got.Detail)
			}
		})
	}
}

func TestCheckCommittedSecrets_ExcludesExampleAndDocs(t *testing.T) {
	dir := gitRepoWith(t, map[string]string{
		".env.example":       "MCTF_DISCORD_TOKEN=aB3xQ9zLmN0pK7wR\n",
		"docs/x.md":          "MCTF_CTFD_TOKEN=aB3xQ9zLmN0pK7wR\n",
		"README.md":          "MCTF_REGISTRY_TOKEN=aB3xQ9zLmN0pK7wR\n",
		"internal/x_test.go": "const tok = \"MCTF_DISCORD_TOKEN=aB3xQ9zLmN0pK7wR\"\n",
	})
	if got := checkCommittedSecrets(dir); got.Status != StatusOK {
		t.Errorf("status = %q, want ok — example/docs/md/_test.go excluded (detail: %s)", got.Status, got.Detail)
	}
}

func TestCheckCommittedSecrets_SkipsNonGitDir(t *testing.T) {
	if got := checkCommittedSecrets(t.TempDir()); got.Status != StatusSkip {
		t.Errorf("status = %q, want skip for a non-git dir", got.Status)
	}
}
