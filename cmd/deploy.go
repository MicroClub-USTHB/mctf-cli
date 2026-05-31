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
	"github.com/MicroClub-USTHB/mctf-cli/internal/kubernetes"
	"github.com/MicroClub-USTHB/mctf-cli/internal/notify"
)

var (
	flagDeployDryRun bool
	flagDeployCat    string
	flagDeploySlug   string
	flagDeployMode   string
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
		cfg, challs, err := discoverAll()
		if err != nil {
			return err
		}

		if deployMode(cfg, flagDeployMode) == "kubernetes" {
			return applyKube(context.Background(), cfg, challs, "up", flagDeployDryRun,
				flagDeployCat, flagDeploySlug)
		}

		plan, err := deploy.Plan(cfg, flagDir, challs, deploy.Options{
			CategoryFilter: flagDeployCat,
			SlugFilter:     flagDeploySlug,
		})
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

// notifyDiscord posts to MTCF_DISCORD_WEBHOOK if set. Best-effort:
// errors are surfaced on stderr but never fail the deploy.
func notifyDiscord(ctx context.Context, title, level, body string) {
	url := os.Getenv("MTCF_DISCORD_WEBHOOK")
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
	flagDownMode    string
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
		cfg, challs, err := discoverAll()
		if err != nil {
			return err
		}

		if deployMode(cfg, flagDownMode) == "kubernetes" {
			return applyKube(context.Background(), cfg, challs, "down", flagDownDryRun,
				flagDownCat, flagDownSlug)
		}

		// Reuse the deploy planner with the down-specific filters.
		flagDeployCat, flagDeploySlug = flagDownCat, flagDownSlug
		plan, err := deploy.Plan(cfg, flagDir, challs, deploy.Options{
			CategoryFilter: flagDeployCat,
			SlugFilter:     flagDeploySlug,
		})
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

func deployMode(cfg *config.Config, flag string) string {
	if flag != "" {
		return flag
	}
	if cfg.Deploy != nil {
		return cfg.Deploy.Mode
	}
	return "docker"
}

func discoverAll() (*config.Config, []*challenge.Challenge, error) {
	cfg, err := config.Load(flagDir)
	if err != nil {
		return nil, nil, err
	}
	challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
	if err != nil {
		return nil, nil, err
	}
	return cfg, challs, nil
}

func applyKube(ctx context.Context, cfg *config.Config, challs []*challenge.Challenge,
	verb string, dry bool, cat, slug string) error {

	if cfg.Deploy == nil {
		cfg.Deploy = &config.Deploy{}
	}
	cfg.Deploy.Mode = "kubernetes"

	plan, err := kubernetes.Plan(cfg, challs, kubernetes.Options{
		CategoryFilter: cat,
		SlugFilter:     slug,
	})
	if err != nil {
		return err
	}
	if len(plan) == 0 {
		fmt.Printf("nothing to %s — no service-archetype challenges matched the filters\n", verb)
		return nil
	}

	printKubePlan(verb, plan)

	kubeconfig := ""
	if cfg.Deploy.Kubernetes != nil {
		kubeconfig = cfg.Deploy.Kubernetes.Kubeconfig
	}
	var runner kubernetes.KubeApplier
	if dry {
		runner = kubernetes.NewDryKubeRunner()
	} else {
		runner = kubernetes.NewKubeRunner(kubeconfig)
	}

	if verb == "up" {
		if err := kubernetes.ApplyUp(ctx, runner, plan); err != nil {
			notifyDiscord(ctx, "deploy failed", "error", err.Error())
			return err
		}
		fmt.Println("\ndeploy complete")
		if !dry {
			notifyDiscord(ctx, "deploy succeeded", "success",
				fmt.Sprintf("%d challenge(s) up [kubernetes]", len(plan)))
		}
	} else {
		if err := kubernetes.ApplyDown(ctx, runner, plan); err != nil {
			notifyDiscord(ctx, "teardown failed", "error", err.Error())
			return err
		}
		fmt.Println("\ndown complete")
		if !dry {
			notifyDiscord(ctx, "teardown complete", "info",
				fmt.Sprintf("%d challenge(s) down [kubernetes]", len(plan)))
		}
	}
	return nil
}

func printPlan(verb string, plan []deploy.Unit) {
	abs, _ := filepath.Abs(flagDir)
	fmt.Printf("planned: %s %d challenge(s)\n", verb, len(plan))
	for _, u := range plan {
		fmt.Printf("  %-30s -> %s\n", relPath(abs, u.Cwd), u.ProjectName)
	}
	fmt.Println()
}

func printKubePlan(verb string, plan []kubernetes.KubeUnit) {
	fmt.Printf("planned: %s %d challenge(s) [kubernetes]\n", verb, len(plan))
	for _, u := range plan {
		fmt.Printf("  %-30s -> %s/%s\n", u.Challenge, u.Namespace, u.Name)
	}
	fmt.Println()
}

func init() {
	deployCmd.Flags().BoolVar(&flagDeployDryRun, "dry-run", false, "print docker compose commands instead of executing")
	deployCmd.Flags().StringVar(&flagDeployMode, "mode", "", "override deploy mode: docker or kubernetes")
	deployCmd.Flags().StringVar(&flagDeployCat, "category", "", "deploy only challenges in this category")
	deployCmd.Flags().StringVar(&flagDeploySlug, "slug", "", "deploy only the challenge with this slug")
	rootCmd.AddCommand(deployCmd)

	downCmd.Flags().BoolVar(&flagDownDryRun, "dry-run", false, "print docker compose commands instead of executing")
	downCmd.Flags().StringVar(&flagDownMode, "mode", "", "override deploy mode: docker or kubernetes")
	downCmd.Flags().BoolVar(&flagDownVolumes, "volumes", false, "also strip named volumes (-v on docker compose down)")
	downCmd.Flags().StringVar(&flagDownCat, "category", "", "tear down only challenges in this category")
	downCmd.Flags().StringVar(&flagDownSlug, "slug", "", "tear down only the challenge with this slug")
	rootCmd.AddCommand(downCmd)
}
