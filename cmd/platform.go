package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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
	flagSyncDryRun  bool
	flagSyncCat     string
	flagSyncSlug    string
	flagSyncUpdate  bool
)

var platformSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync the event's challenges to the configured CTF platform",
	Long: "Reads platform.url from mtcf.config.yml and the API token from\n" +
		"$MTCF_CTFD_TOKEN, then creates challenges that are missing on the\n" +
		"platform.\n\n" +
		"By default, challenges that already exist are skipped — pass\n" +
		"--update to reconcile their fields and child resources (flags, tags,\n" +
		"hints). With --dry-run, prints the plan without making any\n" +
		"network calls beyond the read-only diff.\n\n" +
		"Token: export MTCF_CTFD_TOKEN=<your-token> before running.\n\n" +
		"Examples:\n" +
		"  mtcf platform sync --dry-run\n" +
		"  mtcf platform sync\n" +
		"  mtcf platform sync --update\n" +
		"  mtcf platform sync --category web --update\n" +
		"  mtcf platform sync --json",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		if cfg.Platform == nil || cfg.Platform.Type != "ctfd" {
			return fmt.Errorf("platform.type must be 'ctfd' (got %q) — only CTFd is supported in v0", platformType(cfg))
		}
		if cfg.Platform.URL == "" {
			return fmt.Errorf("platform.url is empty in mtcf.config.yml")
		}
		token := os.Getenv("MTCF_CTFD_TOKEN")
		if token == "" {
			return fmt.Errorf("MTCF_CTFD_TOKEN env var is empty — required to authenticate against %s", cfg.Platform.URL)
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

func init() {
	platformSyncCmd.Flags().BoolVar(&flagSyncDryRun, "dry-run", false, "show the plan without applying it")
	platformSyncCmd.Flags().BoolVar(&flagSyncUpdate, "update", false, "reconcile existing challenges (default: skip them)")
	platformSyncCmd.Flags().StringVar(&flagSyncCat, "category", "", "sync only challenges in this category")
	platformSyncCmd.Flags().StringVar(&flagSyncSlug, "slug", "", "sync only the challenge with this slug")
	platformCmd.AddCommand(platformSyncCmd)
	rootCmd.AddCommand(platformCmd)
}
