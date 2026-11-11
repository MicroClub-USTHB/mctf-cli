package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadCommitSubjectsWithoutTags(t *testing.T) {
	dir := setupTestRepository(t)

	createCommit(t, dir, "first.txt", "feat: first feature")
	createCommit(t, dir, "second.txt", "fix: second feature")

	subjects, previousTag, err := ReadCommitSubjects(dir)
	if err != nil {
		t.Fatalf("ReadCommitSubjects returned error: %v", err)
	}

	if previousTag != "" {
		t.Errorf("previous tag = %q, want empty string", previousTag)
	}

	expected := []string{
		"fix: second feature",
		"feat: first feature",
	}
	if !reflect.DeepEqual(subjects, expected) {
		t.Errorf("subjects = %v, want %v", subjects, expected)
	}
}

func TestReadCommitSubjectsSincePreviousTag(t *testing.T) {
	dir := setupTestRepository(t)

	createCommit(t, dir, "first.txt", "feat: initial feature")
	runGit(t, dir, "tag", "-a", "v0.1.0", "-m", "Release v0.1.0")

	createCommit(t, dir, "second.txt", "fix: repair login")
	createCommit(t, dir, "third.txt", "docs: explain login")

	subjects, previousTag, err := ReadCommitSubjects(dir)
	if err != nil {
		t.Fatalf("ReadCommitSubjects returned error: %v", err)
	}

	if previousTag != "v0.1.0" {
		t.Errorf("previous tag = %q, want %q", previousTag, "v0.1.0")
	}

	expected := []string{
		"docs: explain login",
		"fix: repair login",
	}
	if !reflect.DeepEqual(subjects, expected) {
		t.Errorf("subjects = %v, want %v", subjects, expected)
	}
}

func setupTestRepository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Release Test")
	runGit(t, dir, "config", "user.email", "release-test@example.com")

	return dir
}

func createCommit(t *testing.T, dir, filename, subject string) {
	t.Helper()

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(subject), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	runGit(t, dir, "add", filename)
	runGit(t, dir, "commit", "-m", subject)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	gitArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", gitArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"git %v failed: %v\n%s",
			args,
			err,
			output,
		)
	}
}
