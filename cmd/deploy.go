package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/deploy"
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
		"project named mtcf-<event>-<category>-<slug>.\n\n" +
		"Instanced challenges are intentionally skipped — they are spawned\n" +
		"per-team at game time by the instancer, not at the event-deploy step.\n\n" +
		"For service challenges with internal_port set, PORT_<internal> is\n" +
		"stamped into the env so the compose's ${PORT_N:-N} convention picks\n" +
		"the right host port.\n\n" +
		"Examples:\n" +
		"  mtcf deploy --dry-run\n" +
		"  mtcf deploy --category misc\n" +
		"  mtcf deploy --slug welcome",
	RunE: func(cmd *cobra.Command, args []string) error {
		plan, _, err := planDeploy()
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("nothing to deploy — no service-archetype challenges matched the filters")
			return nil
		}

		printPlan("up", plan)

		var runner deploy.Runner
		if flagDeployDryRun {
			runner = deploy.NewDryRunner()
		} else {
			runner = deploy.NewDockerRunner()
		}
		if err := deploy.ApplyUp(context.Background(), runner, plan); err != nil {
			return err
		}
		fmt.Println("\ndeploy complete")
		return nil
	},
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
	Long: "Reverse of `mtcf deploy`. Runs `docker compose down` for every\n" +
		"deployable, service-archetype challenge in reverse-deploy order.\n\n" +
		"With --volumes, also strips named volumes (passes -v to compose).\n\n" +
		"Examples:\n" +
		"  mtcf down --dry-run\n" +
		"  mtcf down --volumes",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Reuse the deploy planner with the down-specific filters.
		flagDeployCat, flagDeploySlug = flagDownCat, flagDownSlug
		plan, _, err := planDeploy()
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("nothing to tear down — no service-archetype challenges matched the filters")
			return nil
		}

		printPlan("down", plan)

		var runner deploy.Runner
		if flagDownDryRun {
			runner = deploy.NewDryRunner()
		} else {
			runner = deploy.NewDockerRunner()
		}
		if err := deploy.ApplyDown(context.Background(), runner, plan, flagDownVolumes); err != nil {
			return err
		}
		fmt.Println("\ndown complete")
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
