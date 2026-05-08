package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/info"
)

var flagInfoAll bool

var infoCmd = &cobra.Command{
	Use:   "info [<category>/<slug>]",
	Short: "Print derived facts for a single challenge (or every challenge with --all)",
	Long: "Resolves a challenge and prints what the CLI knows about it:\n" +
		"archetype, ingress URL, image-tag template, flag, file inventory.\n\n" +
		"With --all, emits one block per discovered challenge — useful as a\n" +
		"single-shot review document. With --json, emits machine-readable\n" +
		"output (an object for one challenge, an array under --all).\n\n" +
		"Examples:\n" +
		"  mtcf info crypto/rsa-baby\n" +
		"  mtcf info web/cub3-clone --json\n" +
		"  mtcf info --all --json",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, loadErrs, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		if flagInfoAll {
			return runInfoAll(cfg, challs, loadErrs)
		}
		if len(args) != 1 {
			return fmt.Errorf("either pass <category>/<slug> or use --all")
		}
		return runInfoOne(cfg, args[0], challs, loadErrs)
	},
}

func runInfoOne(cfg *config.Config, arg string, challs []*challenge.Challenge, loadErrs map[string]error) error {
	category, slug, ok := splitCategorySlug(arg)
	if !ok {
		return fmt.Errorf("argument must be <category>/<slug>, got %q", arg)
	}
	var found *challenge.Challenge
	for _, c := range challs {
		if c.CategoryDir == category && c.Slug == slug {
			found = c
			break
		}
	}
	if found == nil {
		return fmt.Errorf("no challenge at %s/%s\n%s", category, slug, suggest(challs, arg))
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
}

func runInfoAll(cfg *config.Config, challs []*challenge.Challenge, loadErrs map[string]error) error {
	var views []info.View
	for _, c := range challs {
		if _, bad := loadErrs[c.ManifestPath()]; bad {
			continue
		}
		if c.Manifest.Challenge.Name == "" {
			continue
		}
		views = append(views, info.Build(cfg, c))
	}
	if flagJSON {
		body, err := json.MarshalIndent(views, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(body))
		return nil
	}
	for i, v := range views {
		if i > 0 {
			fmt.Println()
		}
		fmt.Print(v.Render())
	}
	return nil
}

func suggest(challs []*challenge.Challenge, query string) string {
	if len(challs) == 0 {
		return "(no challenges discovered)"
	}
	if hint := didYouMean(challs, query); hint != "" {
		return strings.TrimPrefix(hint, "\n")
	}
	var sb strings.Builder
	sb.WriteString("known challenges:\n")
	for _, c := range challs {
		fmt.Fprintf(&sb, "  %s/%s\n", c.CategoryDir, c.Slug)
	}
	return sb.String()
}

func init() {
	infoCmd.Flags().BoolVar(&flagInfoAll, "all", false, "show every challenge instead of just one")
	rootCmd.AddCommand(infoCmd)
}
