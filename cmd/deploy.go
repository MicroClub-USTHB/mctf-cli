package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/deploy"
	"github.com/MicroClub-USTHB/mctf-cli/internal/notify"
)

var (
	flagDeployDryRun bool
	flagDeployCat    string
	flagDeploySlug   string
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Bring up service-archetype challenges via docker compose",
	Long: "Runs `docker compose up -d` for every deployable, service-archetype\n" +
		"challenge in the event repo. Each challenge gets its own compose\n" +
		"project named mctf-<event>-<category>-<slug>.\n\n" +
		"Instanced challenges are intentionally skipped — they are spawned\n" +
		"per-team at game time by the instancer, not at the event-deploy step.\n\n" +
		"For service challenges with internal_port set, PORT_<internal> is\n" +
		"stamped into the env so the compose's ${PORT_N:-N} convention picks\n" +
		"the right host port.\n\n" +
		"Examples:\n" +
		"  mctf deploy --dry-run\n" +
		"  mctf deploy --category misc\n" +
		"  mctf deploy --slug welcome",
	RunE: func(cmd *cobra.Command, args []string) error {
		plan, cfg, err := planDeploy()
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("nothing to deploy — no service-archetype challenges matched the filters")
			return nil
		}

		printPlan("up", plan)

		runner, err := newDeployRunner(cfg, flagDeployDryRun)
		if err != nil {
			return err
		}
		ctx := context.Background()
		if err := deploy.ApplyUp(ctx, runner, plan); err != nil {
			notifyDiscord(ctx, "deploy failed", "error",
				fmt.Sprintf("%s: %v", planSlugs(plan), err))
			return err
		}
		fmt.Println("\ndeploy complete")
		if !flagDeployDryRun {
			notifyDiscord(ctx, "deploy succeeded", "success",
				fmt.Sprintf("%d challenge(s) up:\n%s", len(plan), planSlugs(plan)))
		}
		return nil
	},
}

func planSlugs(plan []deploy.Unit) string {
	if len(plan) == 0 {
		return "(none)"
	}
	out := make([]string, len(plan))
	for i, u := range plan {
		out[i] = u.Challenge
	}
	return strings.Join(out, ", ")
}

// notifyDiscord posts to MCTF_DISCORD_WEBHOOK if set. Best-effort:
// errors are surfaced on stderr but never fail the deploy.
func notifyDiscord(ctx context.Context, title, level, body string) {
	url := os.Getenv("MCTF_DISCORD_WEBHOOK")
	if url == "" {
		return
	}
	n := notify.NewDiscord(url)
	if err := n.Notify(ctx, notify.Message{Title: title, Body: body, Level: level}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: discord notification failed: %v\n", err)
	}
}

var (
	flagDownDryRun  bool
	flagDownVolumes bool
	flagDownCat     string
	flagDownSlug    string
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Tear down service-archetype challenges via docker compose",
	Long: "Reverse of `mctf deploy`. Runs `docker compose down` for every\n" +
		"deployable, service-archetype challenge in reverse-deploy order.\n\n" +
		"With --volumes, also strips named volumes (passes -v to compose).\n\n" +
		"Examples:\n" +
		"  mctf down --dry-run\n" +
		"  mctf down --volumes",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Reuse the deploy planner with the down-specific filters.
		flagDeployCat, flagDeploySlug = flagDownCat, flagDownSlug
		plan, cfg, err := planDeploy()
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("nothing to tear down — no service-archetype challenges matched the filters")
			return nil
		}

		printPlan("down", plan)

		runner, err := newDeployRunner(cfg, flagDownDryRun)
		if err != nil {
			return err
		}

		ctx := context.Background()
		if err := deploy.ApplyDown(ctx, runner, plan, flagDownVolumes); err != nil {
			notifyDiscord(ctx, "teardown failed", "error",
				fmt.Sprintf("%s: %v", planSlugs(plan), err))
			return err
		}
		fmt.Println("\ndown complete")
		if !flagDownDryRun {
			notifyDiscord(ctx, "teardown complete", "info",
				fmt.Sprintf("%d challenge(s) down:\n%s", len(plan), planSlugs(plan)))
		}
		return nil
	},
}

func planDeploy() ([]deploy.Unit, *config.Config, error) {
	cfg, err := config.Load(flagDir)
	if err != nil {
		return nil, nil, err
	}
	challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
	if err != nil {
		return nil, cfg, err
	}
	plan, err := deploy.Plan(cfg, flagDir, challs, deploy.Options{
		CategoryFilter: flagDeployCat,
		SlugFilter:     flagDeploySlug,
	})
	return plan, cfg, err
}

func newDeployRunner(cfg *config.Config, dryRun bool) (deploy.Runner, error) {
	if cfg == nil || cfg.Deploy == nil {
		return nil, fmt.Errorf("deploy config is missing")
	}
	if cfg.Deploy.Mode == "ssh" {
		return deploy.NewSSHRunner(cfg.Deploy.SSH, dryRun), nil
	}
	if dryRun {
		return deploy.NewDryRunner(), nil
	}

	switch cfg.Deploy.Mode {
	case "docker":
		return deploy.NewDockerRunner(), nil
	case "ssh":
		return deploy.NewSSHRunner(cfg.Deploy.SSH, dryRun), nil
	default:
		return nil, fmt.Errorf("unsupported deploy mode %q", cfg.Deploy.Mode)
	}
}

func printPlan(verb string, plan []deploy.Unit) {
	abs, _ := filepath.Abs(flagDir)
	fmt.Printf("planned: %s %d challenge(s)\n", verb, len(plan))
	for _, u := range plan {
		fmt.Printf("  %-30s -> %s\n", relPath(abs, u.Cwd), u.ProjectName)
	}
	fmt.Println()
}

func init() {
	deployCmd.Flags().BoolVar(&flagDeployDryRun, "dry-run", false, "print docker compose commands instead of executing")
	deployCmd.Flags().StringVar(&flagDeployCat, "category", "", "deploy only challenges in this category")
	deployCmd.Flags().StringVar(&flagDeploySlug, "slug", "", "deploy only the challenge with this slug")
	rootCmd.AddCommand(deployCmd)

	downCmd.Flags().BoolVar(&flagDownDryRun, "dry-run", false, "print docker compose commands instead of executing")
	downCmd.Flags().BoolVar(&flagDownVolumes, "volumes", false, "also strip named volumes (-v on docker compose down)")
	downCmd.Flags().StringVar(&flagDownCat, "category", "", "tear down only challenges in this category")
	downCmd.Flags().StringVar(&flagDownSlug, "slug", "", "tear down only the challenge with this slug")
	rootCmd.AddCommand(downCmd)
}
