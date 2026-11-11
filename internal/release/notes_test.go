package release

import (
	"strings"
	"testing"
)

func TestValidateVersion(t *testing.T) {
	valid := []string{
		"v0.2.0",
		"v1.0.0",
		"v1.2.3",
		"v1.2.3-rc.1",
		"v1.2.3+build.5",
	}

	for _, version := range valid {
		t.Run(version, func(t *testing.T) {
			if err := ValidateVersion(version); err != nil {
				t.Fatalf("ValidateVersion(%q) returned error: %v", version, err)
			}
		})
	}
}

func TestValidateVersionRejectsInvalidVersions(t *testing.T) {
	invalid := []string{
		"",
		"1.2.3",
		"v1",
		"v1.2",
		"v01.2.3",
		"version1.2.3",
	}

	for _, version := range invalid {
		t.Run(version, func(t *testing.T) {
			if err := ValidateVersion(version); err == nil {
				t.Fatalf("ValidateVersion(%q) should fail", version)
			}
		})
	}
}

func TestFormatNotesGroupsCommits(t *testing.T) {
	subjects := []string{
		"feat(cli): add release command",
		"fix: reject dirty working tree",
		"docs: explain release process",
		"chore: tidy dependencies",
		"Merge pull request #12 from example/feature",
	}

	notes := FormatNotes("v0.2.0", subjects)

	expected := []string{
		"# v0.2.0",
		"## Features",
		"- cli: add release command",
		"## Bug Fixes",
		"- reject dirty working tree",
		"## Documentation",
		"- explain release process",
		"## Chores",
		"- tidy dependencies",
		"## Other Changes",
		"- Merge pull request #12 from example/feature",
	}

	position := 0
	for _, item := range expected {
		found := strings.Index(notes[position:], item)
		if found == -1 {
			t.Errorf("release notes are missing %q or it is out of order", item)
			continue
		}
		position += found + len(item)
	}
}

func TestFormatNotesHandlesEmptyCommits(t *testing.T) {
	notes := FormatNotes("v0.2.0", []string{"", "  "})

	if !strings.Contains(notes, "# v0.2.0") {
		t.Error("release notes should contain the version heading")
	}
	if strings.Contains(notes, "## Features") {
		t.Error("release notes should not contain an empty Features section")
	}
}
