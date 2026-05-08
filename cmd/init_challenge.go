package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/scaffold"
	"github.com/MicroClub-USTHB/mctf-cli/internal/validate"
)

var (
	flagInitChallengeForce      bool
	flagInitChallengeAuthor     string
	flagInitChallengeName       string
	flagInitChallengeDifficulty string
	flagInitChallengeArchetype  string
)

var initChallengeCmd = &cobra.Command{
	Use:   "challenge <category>/<slug>",
	Short: "Scaffold a single challenge inside an event repo",
	Long: "Copies the event-local templates/challenge_name/ (or the embedded\n" +
		"fallback) into challenges/<category>/<slug>/ and rewrites the well-\n" +
		"known fields (name, category, difficulty, author) so the result\n" +
		"validates without further edits.\n\n" +
		"Refuses if the target directory exists; pass --force to overwrite.\n\n" +
		"Examples:\n" +
		"  mtcf init challenge web/sql-101 --author $h3rL0ck\n" +
		"  mtcf init challenge crypto/rsa-baby --name \"RSA Baby\" --difficulty medium",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		category, slug, ok := splitCategorySlug(args[0])
		if !ok {
			return fmt.Errorf("argument must be <category>/<slug>, got %q", args[0])
		}
		if !isValidSlug(slug) {
			return fmt.Errorf("invalid slug %q — use lowercase letters, digits, and - or _", slug)
		}

		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}

		cats := validate.ResolveCategories(cfg)
		if _, ok := cats[category]; !ok {
			return fmt.Errorf("unknown category %q\nknown: %s\n(extend with mtcf.config.yml::categories.extended or .custom)",
				category, strings.Join(sortedKeys(cats), ", "))
		}

		display := flagInitChallengeName
		if display == "" {
			display = slug
		}

		switch flagInitChallengeArchetype {
		case "", "static", "service", "instanced":
		default:
			return fmt.Errorf("invalid --archetype %q (want static | service | instanced)", flagInitChallengeArchetype)
		}

		vars := scaffold.ChallengeVars{
			Slug:       slug,
			Category:   category,
			Name:       display,
			Author:     flagInitChallengeAuthor,
			Difficulty: flagInitChallengeDifficulty,
			Archetype:  flagInitChallengeArchetype,
		}
		opts := scaffold.ChallengeOptions{
			EventRoot:     flagDir,
			ChallengesDir: cfg.ChallengesDir,
			Force:         flagInitChallengeForce,
		}

		res, err := scaffold.Challenge(vars, opts)
		if err != nil {
			if errors.Is(err, scaffold.ErrChallengeExists) {
				return err
			}
			return fmt.Errorf("scaffold challenge: %w", err)
		}

		abs, _ := filepath.Abs(flagDir)
		for _, p := range res.Written {
			fmt.Printf("  wrote   %s\n", relPath(abs, p))
		}
		fmt.Printf("\nchallenge %s/%s scaffolded\n", category, slug)
		fmt.Printf("next:\n")
		fmt.Printf("  edit %s/%s/%s/challenge/challenge.yml\n", cfg.ChallengesDir, category, slug)
		fmt.Printf("  mtcf validate\n")
		return nil
	},
}

func splitCategorySlug(s string) (category, slug string, ok bool) {
	i := strings.Index(s, "/")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// didYouMean returns a "\ndid you mean: …" block when query is close to
// one or more known challenges, or "" if there are no near matches.
// Use as a suffix on a "no challenge at <key>" error message.
func didYouMean(challs []*challenge.Challenge, query string) string {
	near := challenge.Suggest(challs, query)
	if len(near) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\ndid you mean:\n")
	for _, k := range near {
		fmt.Fprintf(&sb, "  %s\n", k)
	}
	return sb.String()
}

// isValidSlug enforces the same shape as the spec (DNS-label-ish but
// underscores allowed since MCTF 5.0 uses them).
func isValidSlug(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
			if i == 0 || i == len(s)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]validate.CategoryInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func init() {
	initChallengeCmd.Flags().BoolVar(&flagInitChallengeForce, "force", false, "overwrite an existing challenge directory (destructive)")
	initChallengeCmd.Flags().StringVar(&flagInitChallengeAuthor, "author", "", "author handle (filled into challenge.yml)")
	initChallengeCmd.Flags().StringVar(&flagInitChallengeName, "name", "", "display name (default: slug)")
	initChallengeCmd.Flags().StringVar(&flagInitChallengeDifficulty, "difficulty", "easy", "difficulty: easy | medium | hard | extreme")
	initChallengeCmd.Flags().StringVar(&flagInitChallengeArchetype, "archetype", "", "scaffold a deployable variant: service | instanced (default: static)")
	initCmd.AddCommand(initChallengeCmd)
}
