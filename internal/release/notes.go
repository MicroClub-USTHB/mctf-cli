package release

import (
	"fmt"
	"regexp"
	"strings"
)

var versionPattern = regexp.MustCompile(
	`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`,
)

// ValidateVersion checks that a version follows the vX.Y.Z format.
func ValidateVersion(version string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf(
			"invalid version %q: expected vX.Y.Z, for example v0.2.0",
			version,
		)
	}
	return nil
}

var conventionalCommitPattern = regexp.MustCompile(
	`^([a-zA-Z][a-zA-Z0-9-]*)(\(([^)]+)\))?(!)?:[[:space:]]*(.+)$`,
)

var categoryOrder = []string{
	"Features",
	"Bug Fixes",
	"Documentation",
	"Performance",
	"Refactoring",
	"Tests",
	"Build & CI",
	"Chores",
	"Other Changes",
}

var categoryTypes = map[string]string{
	"feat":     "Features",
	"fix":      "Bug Fixes",
	"docs":     "Documentation",
	"perf":     "Performance",
	"refactor": "Refactoring",
	"test":     "Tests",
	"build":    "Build & CI",
	"ci":       "Build & CI",
	"chore":    "Chores",
}

// FormatNotes groups conventional commit subjects into release notes.
func FormatNotes(version string, subjects []string) string {
	groups := make(map[string][]string)

	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}

		matches := conventionalCommitPattern.FindStringSubmatch(subject)
		if matches == nil {
			groups["Other Changes"] = append(groups["Other Changes"], subject)
			continue
		}

		commitType := strings.ToLower(matches[1])
		scope := matches[3]
		breaking := matches[4] == "!"
		description := matches[5]

		category, ok := categoryTypes[commitType]
		if !ok {
			category = "Other Changes"
		}

		if breaking {
			description += " (BREAKING CHANGE)"
		}
		if scope != "" {
			description = scope + ": " + description
		}

		groups[category] = append(groups[category], description)
	}

	var output strings.Builder
	fmt.Fprintf(&output, "# %s\n", version)

	for _, category := range categoryOrder {
		items := groups[category]
		if len(items) == 0 {
			continue
		}

		fmt.Fprintf(&output, "\n## %s\n", category)
		for _, item := range items {
			fmt.Fprintf(&output, "- %s\n", item)
		}
	}

	return strings.TrimSpace(output.String()) + "\n"
}
