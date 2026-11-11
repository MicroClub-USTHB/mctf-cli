package release

import (
	"fmt"
	"os/exec"
	"strings"
)

// ReadCommitSubjects returns commit subjects since the latest reachable
// version tag. If no version tag exists, it reads the full history.
// It also returns the previous tag, or an empty string if there isn't one.
func ReadCommitSubjects(dir string) ([]string, string, error) {
	tagCmd := exec.Command(
		"git", "-C", dir,
		"tag", "--merged", "HEAD",
		"--sort=-version:refname",
	)

	tagOutput, err := tagCmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("list Git tags: %w", err)
	}

	var previousTag string
	for _, tag := range strings.Fields(string(tagOutput)) {
		if ValidateVersion(tag) == nil {
			previousTag = tag
			break
		}
	}

	args := []string{"-C", dir, "log", "--format=%s"}

	if previousTag != "" {
		args = append(args, previousTag+"..HEAD")
	} else {
		args = append(args, "HEAD")
	}

	logCmd := exec.Command("git", args...)
	logOutput, err := logCmd.Output()
	if err != nil {
		return nil, previousTag, fmt.Errorf("read commit subjects: %w", err)
	}

	output := strings.TrimSpace(string(logOutput))
	if output == "" {
		return []string{}, previousTag, nil
	}

	return strings.Split(output, "\n"), previousTag, nil
}
