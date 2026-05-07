package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/info"
)

var infoCmd = &cobra.Command{
	Use:   "info <category>/<slug>",
	Short: "Print derived facts for a single challenge",
	Long: "Resolves a challenge and prints what the CLI knows about it:\n" +
		"archetype, ingress URL, image-tag template, flag, file inventory.\n\n" +
		"With --json, emits a machine-readable view (same shape as the\n" +
		"per-challenge entry in challenges/index.json).\n\n" +
		"Examples:\n" +
		"  mtcf info crypto/rsa-baby\n" +
		"  mtcf info web/cub3-clone --json",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		category, slug, ok := splitCategorySlug(args[0])
		if !ok {
			return fmt.Errorf("argument must be <category>/<slug>, got %q", args[0])
		}

		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, loadErrs, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		var found *challenge.Challenge
		for _, c := range challs {
			if c.CategoryDir == category && c.Slug == slug {
				found = c
				break
			}
		}
		if found == nil {
			return fmt.Errorf("no challenge at %s/%s\n%s", category, slug, suggest(challs))
		}
		if loadErr, ok := loadErrs[found.ManifestPath()]; ok {
			return fmt.Errorf("manifest failed to parse: %w", loadErr)
		}

		view := info.Build(cfg, found)

		if flagJSON {
			out, err := view.JSON()
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		}
		fmt.Print(view.Render())
		return nil
	},
}

func suggest(challs []*challenge.Challenge) string {
	if len(challs) == 0 {
		return "(no challenges discovered)"
	}
	var sb strings.Builder
	sb.WriteString("known challenges:\n")
	for _, c := range challs {
		fmt.Fprintf(&sb, "  %s/%s\n", c.CategoryDir, c.Slug)
	}
	return sb.String()
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
