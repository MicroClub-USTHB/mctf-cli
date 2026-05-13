package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform/ctfd"
)

var platformCmd = &cobra.Command{
	Use:   "platform",
	Short: "Sync challenges to a CTF platform (CTFd in v0)",
}

var (
	flagSyncDryRun bool
	flagSyncCat    string
	flagSyncSlug   string
	flagSyncUpdate bool
)

var platformSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync the event's challenges to the configured CTF platform",
	Long: "Reads platform.url from mctf.config.yml and the API token from\n" +
		"$MCTF_CTFD_TOKEN, then creates challenges that are missing on the\n" +
		"platform.\n\n" +
		"By default, challenges that already exist are skipped — pass\n" +
		"--update to reconcile their fields and child resources (flags, tags,\n" +
		"hints). With --dry-run, prints the plan without making any\n" +
		"network calls beyond the read-only diff.\n\n" +
		"Token: export MCTF_CTFD_TOKEN=<your-token> before running.\n\n" +
		"Examples:\n" +
		"  mctf platform sync --dry-run\n" +
		"  mctf platform sync\n" +
		"  mctf platform sync --update\n" +
		"  mctf platform sync --category web --update\n" +
		"  mctf platform sync --json",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		if cfg.Platform == nil || cfg.Platform.Type != "ctfd" {
			return fmt.Errorf("platform.type must be 'ctfd' (got %q) — only CTFd is supported in v0", platformType(cfg))
		}
		if cfg.Platform.URL == "" {
			return fmt.Errorf("platform.url is empty in mctf.config.yml")
		}
		token := os.Getenv("MCTF_CTFD_TOKEN")
		if token == "" {
			return fmt.Errorf("MCTF_CTFD_TOKEN env var is empty — required to authenticate against %s", cfg.Platform.URL)
		}

		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		client := ctfd.New(cfg.Platform.URL, token)
		ctx := context.Background()

		plan, err := ctfd.Diff(ctx, client, cfg, challs, ctfd.PlanOptions{
			CategoryFilter: flagSyncCat,
			SlugFilter:     flagSyncSlug,
			AllowUpdate:    flagSyncUpdate,
		})
		if err != nil {
			return err
		}

		if flagJSON {
			if err := printPlanJSON(plan); err != nil {
				return err
			}
		} else {
			fmt.Print(plan.Render())
		}

		if flagSyncDryRun {
			return nil
		}

		applied, err := ctfd.Apply(ctx, client, plan)
		if err != nil {
			return err
		}
		if !flagJSON {
			fmt.Printf("\napplied %d action(s)\n", applied)
		}
		return nil
	},
}

type planRow struct {
	Slug       string         `json:"slug"`
	Action     ctfd.Action    `json:"action"`
	Reason     string         `json:"reason"`
	ExistingID int            `json:"existing_id,omitempty"`
	Payload    ctfd.Challenge `json:"payload"`
}

func printPlanJSON(plan *ctfd.Plan) error {
	rows := make([]planRow, 0, len(plan.Items))
	for _, it := range plan.Items {
		rows = append(rows, planRow{
			Slug:       it.Slug,
			Action:     it.Action,
			Reason:     it.Reason,
			ExistingID: it.ExistingID,
			Payload:    it.Payload,
		})
	}
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(body))
	return nil
}

func platformType(cfg *config.Config) string {
	if cfg.Platform == nil {
		return ""
	}
	return cfg.Platform.Type
}

var (
	flagConfigureDryRun bool
)

var platformConfigureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Sync event-level CTFd config (ctf_name) from mctf.config.yml",
	Long: "Pushes ctf_name to CTFd's /api/v1/configs from the loaded\n" +
		"event config. Theme settings (theme_name, theme_color, custom\n" +
		"HTML) are intentionally not synced — those are typically a one-\n" +
		"time UI setup, not per-event automation.\n\n" +
		"Token: export MCTF_CTFD_TOKEN=<your-token> before running.\n\n" +
		"Examples:\n" +
		"  mctf platform configure --dry-run\n" +
		"  mctf platform configure",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		if cfg.Platform == nil || cfg.Platform.Type != "ctfd" {
			return fmt.Errorf("platform.type must be 'ctfd' (got %q)", platformType(cfg))
		}
		if cfg.Platform.URL == "" {
			return fmt.Errorf("platform.url is empty in mctf.config.yml")
		}
		token := os.Getenv("MCTF_CTFD_TOKEN")
		if token == "" {
			return fmt.Errorf("MCTF_CTFD_TOKEN env var is empty")
		}
		if cfg.Event.Name == "" {
			return fmt.Errorf("event.name is empty in mctf.config.yml — nothing to push")
		}

		fmt.Printf("planned config:\n")
		fmt.Printf("  ctf_name = %q\n", cfg.Event.Name)
		if flagConfigureDryRun {
			return nil
		}

		client := ctfd.New(cfg.Platform.URL, token)
		ctx := context.Background()
		if err := client.SetConfig(ctx, "ctf_name", cfg.Event.Name); err != nil {
			return fmt.Errorf("set ctf_name: %w", err)
		}
		fmt.Println("\nconfig pushed")
		return nil
	},
}

var (
	flagUnsyncDryRun bool
	flagUnsyncYes    bool
)

var platformUnsyncCmd = &cobra.Command{
	Use:   "unsync <category>/<slug>",
	Short: "Remove a challenge from the configured CTF platform",
	Long: "Looks up the CTFd challenge that corresponds to <category>/<slug>\n" +
		"and DELETEs it via /api/v1/challenges/<id>. CTFd cascades the\n" +
		"delete to flags, tags, hints, and files.\n\n" +
		"Local files in challenges/<category>/<slug>/ are NOT touched —\n" +
		"this only undoes the platform-side state. Use git for local\n" +
		"cleanup.\n\n" +
		"The local manifest's `name:` is preferred for matching; when the\n" +
		"challenge directory is gone, the command falls back to matching\n" +
		"on category + slug-from-name.\n\n" +
		"Examples:\n" +
		"  mctf platform unsync misc/welcome --dry-run\n" +
		"  mctf platform unsync misc/welcome\n" +
		"  mctf platform unsync misc/welcome --yes",
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
		if cfg.Platform == nil || cfg.Platform.Type != "ctfd" {
			return fmt.Errorf("platform.type must be 'ctfd' (got %q)", platformType(cfg))
		}
		if cfg.Platform.URL == "" {
			return fmt.Errorf("platform.url is empty in mctf.config.yml")
		}
		token := os.Getenv("MCTF_CTFD_TOKEN")
		if token == "" {
			return fmt.Errorf("MCTF_CTFD_TOKEN env var is empty")
		}

		// Prefer the local manifest's display name when available — the
		// directory may have been removed already, in which case we fall
		// back to slug-based matching against the platform's list.
		var wantName string
		if challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir); err == nil {
			for _, c := range challs {
				if c.CategoryDir == category && c.Slug == slug {
					wantName = c.Manifest.Challenge.Name
					break
				}
			}
		}

		client := ctfd.New(cfg.Platform.URL, token)
		ctx := context.Background()
		existing, err := client.ListChallenges(ctx)
		if err != nil {
			return fmt.Errorf("list challenges: %w", err)
		}

		target := findUnsyncTarget(existing, category, slug, wantName)
		if target == nil {
			return fmt.Errorf("no CTFd challenge matched %s/%s (looked for category=%s, name=%q)",
				category, slug, category, wantName)
		}

		fmt.Printf("will delete CTFd challenge id=%d category=%q name=%q\n",
			target.ID, target.Category, target.Name)
		if flagUnsyncDryRun {
			return nil
		}
		if !flagUnsyncYes {
			if !confirmYes(promptIn, os.Stderr, "delete?") {
				fmt.Println("aborted")
				return nil
			}
		}
		if err := client.DeleteChallenge(ctx, target.ID); err != nil {
			return fmt.Errorf("delete challenge id=%d: %w", target.ID, err)
		}
		fmt.Printf("deleted id=%d\n", target.ID)
		return nil
	},
}

// findUnsyncTarget picks one CTFd challenge from `existing` that
// corresponds to <category>/<slug>. It tries:
//
//  1. category + exact name match (case-insensitive) using `wantName`
//     from the local manifest, if provided.
//  2. category + slug-derived name match (the fallback when the local
//     challenge has already been removed).
//
// Returns nil if no candidate is found.
func findUnsyncTarget(existing []ctfd.Challenge, category, slug, wantName string) *ctfd.Challenge {
	for i := range existing {
		ec := &existing[i]
		if !strings.EqualFold(ec.Category, category) {
			continue
		}
		if wantName != "" && strings.EqualFold(ec.Name, wantName) {
			return ec
		}
	}
	// Fallback: slug-from-name match.
	for i := range existing {
		ec := &existing[i]
		if !strings.EqualFold(ec.Category, category) {
			continue
		}
		if challenge.SlugFromName(ec.Name) == slug {
			return ec
		}
	}
	return nil
}

// confirmYes blocks on stdin for a y/N answer. Returns true only for
// explicit "y" / "yes" (case-insensitive). Default is no.
func confirmYes(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprintf(out, "%s [y/N] ", prompt)
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

func init() {
	platformSyncCmd.Flags().BoolVar(&flagSyncDryRun, "dry-run", false, "show the plan without applying it")
	platformSyncCmd.Flags().BoolVar(&flagSyncUpdate, "update", false, "reconcile existing challenges (default: skip them)")
	platformSyncCmd.Flags().StringVar(&flagSyncCat, "category", "", "sync only challenges in this category")
	platformSyncCmd.Flags().StringVar(&flagSyncSlug, "slug", "", "sync only the challenge with this slug")
	platformCmd.AddCommand(platformSyncCmd)

	platformConfigureCmd.Flags().BoolVar(&flagConfigureDryRun, "dry-run", false, "show what would be set without making the request")
	platformCmd.AddCommand(platformConfigureCmd)

	platformUnsyncCmd.Flags().BoolVar(&flagUnsyncDryRun, "dry-run", false, "show what would be deleted without sending the request")
	platformUnsyncCmd.Flags().BoolVarP(&flagUnsyncYes, "yes", "y", false, "skip the interactive confirmation prompt")
	platformCmd.AddCommand(platformUnsyncCmd)

	rootCmd.AddCommand(platformCmd)
}
