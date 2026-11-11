package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform/ctfd"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform/rctf"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform/webhook"
)

var platformCmd = &cobra.Command{
	Use:   "platform",
	Short: "Sync challenges to a CTF platform (CTFd, webhook or rCTF)",
	Long: "Pushes the event to the platform named by platform.type in\n" +
		"mctf.config.yml:\n\n" +
		"  ctfd     CTFd API at platform.url; token from $MCTF_CTFD_TOKEN\n" +
		"  webhook  POSTs JSON events to platform.url; optional HMAC signing\n" +
		"           secret from $MCTF_WEBHOOK_SECRET\n" +
		"  rctf     rCTF API at platform.url; token from $MCTF_RCTF_TOKEN\n\n" +
		"See docs/spec/platforms.md for the platform contracts.",
}

var flagPlatformInsecureHTTP bool

// platformTypes lists the supported platform.type values, in the order
// error messages show them.
var platformTypes = []string{"ctfd", "webhook", "rctf"}

// newPlatform builds the target named by platform.type, reading its
// credentials from the environment.
func newPlatform(cfg *config.Config) (platform.Platform, error) {
	typ := platformType(cfg)
	switch typ {
	case "ctfd", "webhook", "rctf":
	default:
		return nil, fmt.Errorf("platform.type must be one of %s (got %q)", strings.Join(platformTypes, ", "), typ)
	}
	if cfg.Platform.URL == "" {
		return nil, fmt.Errorf("platform.url is empty in mctf.config.yml")
	}
	switch typ {
	case "ctfd":
		token := os.Getenv("MCTF_CTFD_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("MCTF_CTFD_TOKEN env var is empty — required to authenticate against %s", cfg.Platform.URL)
		}
		return ctfd.NewPlatform(cfg.Platform.URL, token), nil
	case "rctf":
		token := os.Getenv("MCTF_RCTF_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("MCTF_RCTF_TOKEN env var is empty — required to authenticate against %s", cfg.Platform.URL)
		}
		return rctf.NewPlatform(cfg.Platform.URL, token)
	case "webhook":
		p, err := webhook.New(cfg.Platform.URL, os.Getenv("MCTF_WEBHOOK_SECRET"), "mctf/"+version, flagPlatformInsecureHTTP)
		if err != nil {
			return nil, err
		}
		p.EventSlug = cfg.Event.Slug
		return p, nil
	default:
		return nil, fmt.Errorf("platform.type must be one of %s (got %q)", strings.Join(platformTypes, ", "), typ)
	}
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
	Long: "Reads platform.type and platform.url from mctf.config.yml and\n" +
		"pushes the event's challenges to that platform.\n\n" +
		"ctfd: creates challenges that are missing on the platform. By\n" +
		"default, challenges that already exist are skipped — pass --update\n" +
		"to reconcile their fields and child resources (flags, tags, hints).\n" +
		"With --dry-run, prints the plan without making any network calls\n" +
		"beyond the read-only diff. Token: export MCTF_CTFD_TOKEN=<token>.\n\n" +
		"webhook: POSTs one snapshot of the (filtered) challenges, flags\n" +
		"included. --update has no effect; --dry-run prints the payload\n" +
		"without sending it. Set MCTF_WEBHOOK_SECRET to sign the body.\n\n" +
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
		target, err := newPlatform(cfg)
		if err != nil {
			return err
		}

		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		return target.Sync(context.Background(), platform.SyncRequest{
			Config:         cfg,
			Challenges:     challs,
			CategoryFilter: flagSyncCat,
			SlugFilter:     flagSyncSlug,
			AllowUpdate:    flagSyncUpdate,
			DryRun:         flagSyncDryRun,
			JSON:           flagJSON,
			Out:            os.Stdout,
		})
	},
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
	Short: "Sync event-level config (name, description, start, end) from mctf.config.yml",
	Long: "ctfd: pushes ctf_name, ctf_description, start, and end timestamps to\n" +
		"CTFd's /api/v1/configs from the loaded event config. Theme settings\n" +
		"(theme_name, theme_color, custom HTML) are intentionally not synced —\n" +
		"those are typically a one-time UI setup, not per-event automation.\n" +
		"Token: export MCTF_CTFD_TOKEN=<your-token> before running.\n\n" +
		"webhook: POSTs a configure event with the same fields.\n\n" +
		"Examples:\n" +
		"  mctf platform configure --dry-run\n" +
		"  mctf platform configure",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		target, err := newPlatform(cfg)
		if err != nil {
			return err
		}
		if cfg.Event.Name == "" {
			return fmt.Errorf("event.name is empty in mctf.config.yml — nothing to push")
		}

		if cfg.Event.StartAt != "" && cfg.Event.EndAt != "" {
			start, err := time.Parse(time.RFC3339, cfg.Event.StartAt)
			if err != nil {
				return fmt.Errorf("invalid event.start_at: %w", err)
			}
			end, err := time.Parse(time.RFC3339, cfg.Event.EndAt)
			if err != nil {
				return fmt.Errorf("invalid event.end_at: %w", err)
			}
			if !start.Before(end) {
				return fmt.Errorf("event.start_at (%s) must be before event.end_at (%s)", cfg.Event.StartAt, cfg.Event.EndAt)
			}
		}

		fmt.Printf("planned config:\n")
		fmt.Printf("  ctf_name = %q\n", cfg.Event.Name)
		if cfg.Event.Description != "" {
			fmt.Printf("  ctf_description = %q\n", cfg.Event.Description)
		}
		if cfg.Event.StartAt != "" {
			fmt.Printf("  start = %q\n", cfg.Event.StartAt)
		}
		if cfg.Event.EndAt != "" {
			fmt.Printf("  end = %q\n", cfg.Event.EndAt)
		}
		if flagConfigureDryRun {
			return nil
		}

		if err := target.Configure(context.Background(), platform.EventConfig{
			Name:        cfg.Event.Name,
			Description: cfg.Event.Description,
			StartAt:     cfg.Event.StartAt,
			EndAt:       cfg.Event.EndAt,
		}); err != nil {
			return err
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
	Long: "ctfd: looks up the CTFd challenge that corresponds to\n" +
		"<category>/<slug> and DELETEs it via /api/v1/challenges/<id>. CTFd\n" +
		"cascades the delete to flags, tags, hints, and files.\n\n" +
		"webhook: POSTs an unsync event for <category>/<slug>.\n\n" +
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
		target, err := newPlatform(cfg)
		if err != nil {
			return err
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

		ctx := context.Background()
		t, err := target.ResolveUnsync(ctx, category, slug, wantName)
		if err != nil {
			return err
		}

		fmt.Printf("will %s\n", t.Description)
		if flagUnsyncDryRun {
			return nil
		}
		if !flagUnsyncYes {
			if !confirmYes(promptIn, os.Stderr, "delete?") {
				fmt.Println("aborted")
				return nil
			}
		}
		if err := target.Unsync(ctx, t); err != nil {
			return err
		}
		fmt.Println(t.Done)
		return nil
	},
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
	platformCmd.PersistentFlags().BoolVar(&flagPlatformInsecureHTTP, "insecure-http", false, "allow http:// webhook URLs (payloads include flags)")

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
