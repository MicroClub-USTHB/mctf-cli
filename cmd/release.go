package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/release"
	"github.com/spf13/cobra"
)

var releaseCmd = &cobra.Command{
	Use:   "release <version>",
	Short: "Create a versioned release and push its tag",
	Long: `Prepare a release using conventional commit messages.
The command requires a clean working tree and the main or develop branch.
Use --dry-run to preview the release without creating or pushing a tag.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRelease(cmd, args[0], releaseDryRun)
	},
}

var releaseDryRun bool

func init() {
	releaseCmd.Flags().BoolVar(
		&releaseDryRun,
		"dry-run",
		false,
		"preview release notes without creating or pushing a tag",
	)
	rootCmd.AddCommand(releaseCmd)
}

func runRelease(cmd *cobra.Command, version string, dryRun bool) error {
	if err := release.ValidateVersion(version); err != nil {
		return err
	}

	dir := flagDir

	branch, err := gitCommand(dir, "branch", "--show-current")
	if err != nil {
		return fmt.Errorf("check current branch: %w", err)
	}
	branch = strings.TrimSpace(branch)

	if branch != "main" && branch != "develop" {
		return fmt.Errorf(
			"release must be run from main or develop; current branch is %q",
			branch,
		)
	}

	status, err := gitCommand(dir, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("check working tree: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf(
			"working tree is not clean; commit or stash your changes first",
		)
	}

	tags, err := gitCommand(dir, "tag", "--list", version)
	if err != nil {
		return fmt.Errorf("check existing release tag: %w", err)
	}
	for _, tag := range strings.Fields(tags) {
		if tag == version {
			return fmt.Errorf("tag %q already exists; refusing to overwrite it", version)
		}
	}

	subjects, previousTag, err := release.ReadCommitSubjects(dir)
	if err != nil {
		return fmt.Errorf("generate release notes: %w", err)
	}

	notes := release.FormatNotes(version, subjects)
	out := cmd.OutOrStdout()

	if previousTag == "" {
		fmt.Fprintln(out, "No previous version tag found; using the full Git history.")
	} else {
		fmt.Fprintf(out, "Previous release: %s\n", previousTag)
	}

	fmt.Fprintf(out, "\n%s\n", notes)

	if dryRun {
		fmt.Fprintf(out, "\nDry run: planned tag is %s\n", version)
		fmt.Fprintln(out, "No tag was created and nothing was pushed.")
		return nil
	}

	if !confirmYes(
		os.Stdin,
		cmd.ErrOrStderr(),
		fmt.Sprintf("Create annotated tag %s and push it to origin?", version),
	) {
		fmt.Fprintln(out, "Release cancelled. No tag was created or pushed.")
		return nil
	}

	if _, err := gitCommand(
		dir,
		"tag", "-a", version, "-m", "Release "+version,
	); err != nil {
		return fmt.Errorf("create annotated tag %s: %w", version, err)
	}

	if _, err := gitCommand(dir, "push", "origin", version); err != nil {
		return fmt.Errorf(
			"tag %s was created locally, but pushing it failed: %w",
			version,
			err,
		)
	}

	fmt.Fprintf(out, "Release tag %s was created and pushed to origin.\n", version)
	return nil
}

func gitCommand(dir string, args ...string) (string, error) {
	gitArgs := append([]string{"-C", dir}, args...)
	gitCmd := exec.Command("git", gitArgs...)

	output, err := gitCmd.CombinedOutput()
	if err != nil {
		details := strings.TrimSpace(string(output))
		if details != "" {
			return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), details, err)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return string(output), nil
}
