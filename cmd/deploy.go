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
	mctfgit "github.com/MicroClub-USTHB/mctf-cli/internal/git"
	"github.com/MicroClub-USTHB/mctf-cli/internal/kubernetes"
	"github.com/MicroClub-USTHB/mctf-cli/internal/notify"
)

var (
	flagDeployDryRun   bool
	flagDeployCat      string
	flagDeploySlug     string
	flagDeployRollback bool
	flagDeployPushTags bool
	flagDeployMode     string
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Bring up service-archetype challenges via docker compose or kubectl",
	Long: "Runs `docker compose up -d` (or `kubectl apply`) for every deployable,\n" +
		"service-archetype challenge in the event repo. Each challenge gets its own compose\n" +
		"project named mctf-<event>-<category>-<slug>.\n\n" +
		"Instanced challenges are intentionally skipped — they are spawned\n" +
		"per-team at game time by the instancer, not at the event-deploy step.\n\n" +
		"For service challenges with internal_port set, PORT_<internal> is\n" +
		"stamped into the env so the compose's ${PORT_N:-N} convention picks\n" +
		"the right host port.\n\n" +
		"Examples:\n" +
		"  mctf deploy --dry-run\n" +
		"  mctf deploy --category misc\n" +
		"  mctf deploy --slug welcome\n" +
		"  mctf deploy --mode kubernetes --dry-run",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}

		// Kubernetes mode: delegate entirely to the K8s planner.
		if deployMode(cfg, flagDeployMode) == "kubernetes" {
			challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
			if err != nil {
				return err
			}
			return applyKube(cmd.Context(), cfg, challs, "up", flagDeployDryRun,
				flagDeployCat, flagDeploySlug)
		}

		hasRegistry := cfg.Registry != nil && cfg.Registry.URL != ""

		// Local deploy lock to prevent concurrent runs
		lockFile := filepath.Join(flagDir, ".mctf", "deploy.lock")
		// Ensure lock directory exists
		if err := os.MkdirAll(filepath.Dir(lockFile), 0755); err != nil {
			return fmt.Errorf("failed to create lock directory: %w", err)
		}
		// Check for existing lock
		if _, err := os.Stat(lockFile); err == nil {
			return fmt.Errorf("deploy already in progress (remove %s if stale)", lockFile)
		}
		// Create lock file
		if err := os.WriteFile(lockFile, []byte("locked"), 0644); err != nil {
			return fmt.Errorf("failed to create lock file: %w", err)
		}
		defer func() {
			if err := os.Remove(lockFile); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "warning: failed to remove lock file %s: %v\n", lockFile, err)
			}
		}()

		// Determine target SHA and dir
		targetDir := flagDir
		targetSHA := ""
		if flagDeployRollback {
			if !hasRegistry {
				return fmt.Errorf("--rollback requires registry.url to be configured in mctf.config.yml")
			}
			targetSHA, err = mctfgit.GetSHA(flagDir, "mctf-deploy-previous")
			if err != nil {
				return fmt.Errorf("could not resolve mctf-deploy-previous: %w", err)
			}
			// Create temporary directory for rollback extraction
			tempDir, err := os.MkdirTemp("", "mctf-rollback-")
			if err != nil {
				return err
			}
			// Ensure cleanup of temporary directory
			defer func() {
				if err := os.RemoveAll(tempDir); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to clean up temp dir: %v\n", err)
				}
			}()
			if err := mctfgit.Archive(flagDir, "mctf-deploy-previous", tempDir); err != nil {
				return fmt.Errorf("failed to extract rollback commit: %w", err)
			}
			targetDir = tempDir
		} else if hasRegistry {
			targetSHA, err = mctfgit.GetSHA(flagDir, "HEAD")
			if err != nil {
				return fmt.Errorf("could not resolve HEAD: %w", err)
			}
		}

		// Load target plan
		plan, runner, _, err := loadPlanAndRunner(targetDir, targetSHA, flagDeployDryRun)
		if err != nil {
			return err
		}

		// Diff with active state for orphan cleanup (registry mode only)
		if hasRegistry {
			activeSHA, err := mctfgit.GetSHA(flagDir, "mctf-deploy-active")
			if err == nil && activeSHA != "" {
				tempDirActive, err := os.MkdirTemp("", "mctf-active-")
				if err == nil {
					defer func() {
						if err := os.RemoveAll(tempDirActive); err != nil && !os.IsNotExist(err) {
							fmt.Fprintf(os.Stderr, "warning: failed to remove temp dir %s: %v\n", tempDirActive, err)
						}
					}()
					if err := mctfgit.Archive(flagDir, "mctf-deploy-active", tempDirActive); err == nil {
						activePlan, activeRunner, _, err := loadPlanAndRunner(tempDirActive, activeSHA, flagDeployDryRun)
						if err == nil {
							// Compute orphans: in activePlan but not in plan
							targetMap := make(map[string]bool)
							for _, u := range plan {
								targetMap[u.ProjectName] = true
							}
							var orphans []deploy.Unit
							for _, au := range activePlan {
								if !targetMap[au.ProjectName] {
									orphans = append(orphans, au)
								}
							}
							if len(orphans) > 0 {
								fmt.Printf("found %d orphan(s) to clean up...\n", len(orphans))
								for _, o := range orphans {
									if err := deploy.TranslateCompose(o, cfg.Registry.URL); err != nil {
										fmt.Fprintf(os.Stderr, "warning: failed to translate compose for orphan %s: %v\n", o.Challenge, err)
									} else {
										o.ComposeFile = filepath.Join(o.Cwd, "docker-compose.mctf.yml")
									}
								}
								if err := deploy.ApplyDown(context.Background(), activeRunner, orphans, false); err != nil {
									fmt.Fprintf(os.Stderr, "warning: orphan cleanup failed: %v\n", err)
								}
							}
						}
					}
				}
			}
		}

		if len(plan) == 0 {
			fmt.Println("nothing to deploy — no service-archetype challenges matched the filters")
			return nil
		}
		printPlan("up", plan)

		// Translate compose for registry-pinned deploys; simple mode
		// uses the original docker-compose.yml unchanged.
		if hasRegistry {
			for i, u := range plan {
				if !flagDeployDryRun {
					if err := deploy.TranslateCompose(u, cfg.Registry.URL); err != nil {
						return fmt.Errorf("translate %s: %w", u.Challenge, err)
					}
				}
				plan[i].ComposeFile = filepath.Join(u.Cwd, "docker-compose.mctf.yml")
			}
		}

		ctx := context.Background()
		if err := deploy.ApplyUp(ctx, runner, plan); err != nil {
			notifyDiscord(ctx, "deploy failed", "error", fmt.Sprintf("%s: %v", planSlugs(plan), err))
			return err
		}

		// Update deploy tags (registry mode only — tags track image SHAs)
		if !flagDeployDryRun && hasRegistry {
			if flagDeployRollback {
				// Swap active and previous
				oldActive, _ := mctfgit.GetSHA(flagDir, "mctf-deploy-active")
				if err := mctfgit.UpdateTag(flagDir, "mctf-deploy-active", targetSHA); err != nil {
					return fmt.Errorf("failed to update active tag: %w", err)
				}
				if oldActive != "" {
					if err := mctfgit.UpdateTag(flagDir, "mctf-deploy-previous", oldActive); err != nil {
						return fmt.Errorf("failed to update previous tag: %w", err)
					}
				}
			} else {
				// Normal deploy
				oldActive, _ := mctfgit.GetSHA(flagDir, "mctf-deploy-active")
				if oldActive != "" {
					if err := mctfgit.UpdateTag(flagDir, "mctf-deploy-previous", oldActive); err != nil {
						return fmt.Errorf("failed to update previous tag: %w", err)
					}
				}
				if err := mctfgit.UpdateTag(flagDir, "mctf-deploy-active", targetSHA); err != nil {
					return fmt.Errorf("failed to update active tag: %w", err)
				}
			}
			if flagDeployPushTags {
				if err := mctfgit.PushTags(flagDir, "origin", "mctf-deploy-active", "mctf-deploy-previous"); err != nil {
					return fmt.Errorf("failed to push tags: %w", err)
				}
			}
		}

		fmt.Println("\ndeploy complete")
		if !flagDeployDryRun {
			notifyDiscord(ctx, "deploy succeeded", "success", fmt.Sprintf("%d challenge(s) up:\n%s", len(plan), planSlugs(plan)))
		}
		return nil
	},
}

func loadPlanAndRunner(dir, sha string, dryRun bool) ([]deploy.Unit, deploy.Runner, *config.Config, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	challs, _, err := challenge.Discover(dir, cfg.ChallengesDir)
	if err != nil {
		return nil, nil, nil, err
	}
	opts := deploy.Options{
		CategoryFilter: flagDeployCat,
		SlugFilter:     flagDeploySlug,
		ImageTag:       sha,
	}
	plan, err := deploy.Plan(cfg, dir, challs, opts)
	if err != nil {
		return nil, nil, nil, err
	}
	runner, err := newDeployRunner(cfg, dryRun)
	if err != nil {
		return nil, nil, nil, err
	}
	return plan, runner, cfg, nil
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
	flagDownMode    string
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Tear down service-archetype challenges via docker compose or kubectl",
	Long: "Reverse of `mctf deploy`. Runs `docker compose down` (or `kubectl delete`)\n" +
		"for every deployable, service-archetype challenge in reverse-deploy order.\n\n" +
		"With --volumes, also strips named volumes (passes -v to compose).\n\n" +
		"Examples:\n" +
		"  mctf down --dry-run\n" +
		"  mctf down --volumes\n" +
		"  mctf down --mode kubernetes",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, challs, err := discoverAll()
		if err != nil {
			return err
		}

		if deployMode(cfg, flagDownMode) == "kubernetes" {
			return applyKube(cmd.Context(), cfg, challs, "down", flagDownDryRun,
				flagDownCat, flagDownSlug)
		}

		// Reuse the deploy planner with the down-specific filters.
		flagDeployCat, flagDeploySlug = flagDownCat, flagDownSlug
		targetSHA, _ := mctfgit.GetSHA(flagDir, "HEAD")
		plan, runner, cfg, err := loadPlanAndRunner(flagDir, targetSHA, flagDownDryRun)
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("nothing to tear down — no service-archetype challenges matched the filters")
			return nil
		}

		printPlan("down", plan)

		ctx := context.Background()
		// Use translated compose when registry is configured and the
		// generated file exists; otherwise fall back to the original.
		if cfg.Registry != nil && cfg.Registry.URL != "" {
			for i, u := range plan {
				mctfCompose := filepath.Join(u.Cwd, "docker-compose.mctf.yml")
				if _, err := os.Stat(mctfCompose); err == nil {
					plan[i].ComposeFile = mctfCompose
				}
			}
		}
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

// deployMode resolves the effective deploy mode: flag overrides config.
func deployMode(cfg *config.Config, flag string) string {
	if flag != "" {
		return flag
	}
	if cfg != nil && cfg.Deploy != nil {
		return cfg.Deploy.Mode
	}
	return "docker"
}

// discoverAll loads the config and discovers all challenges from flagDir.
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

// applyKube runs the kubernetes planner and applier for the given verb (up/down).
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

func newDeployRunner(cfg *config.Config, dryRun bool) (deploy.Runner, error) {
	if cfg == nil || cfg.Deploy == nil {
		return nil, fmt.Errorf("deploy config is missing")
	}
	switch cfg.Deploy.Mode {
	case "ssh":
		// SSHRunner owns its own DryRun behavior; no wrapper needed.
		return deploy.NewSSHRunner(cfg.Deploy.SSH, dryRun), nil
	case "docker":
		if dryRun {
			return deploy.NewDryRunner(), nil
		}
		return deploy.NewDockerRunner(), nil
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

func printKubePlan(verb string, plan []kubernetes.KubeUnit) {
	fmt.Printf("planned: %s %d challenge(s) [kubernetes]\n", verb, len(plan))
	for _, u := range plan {
		fmt.Printf("  %-30s -> %s/%s\n", u.Challenge, u.Namespace, u.Name)
	}
	fmt.Println()
}

func init() {
	deployCmd.Flags().BoolVar(&flagDeployDryRun, "dry-run", false, "print docker compose commands instead of executing")
	deployCmd.Flags().BoolVar(&flagDeployRollback, "rollback", false, "rollback to mctf-deploy-previous tag")
	deployCmd.Flags().BoolVar(&flagDeployPushTags, "push-tags", false, "push active and previous tags to origin")
	deployCmd.Flags().StringVar(&flagDeployMode, "mode", "", "override deploy mode: docker, ssh or kubernetes")
	deployCmd.Flags().StringVar(&flagDeployCat, "category", "", "deploy only challenges in this category")
	deployCmd.Flags().StringVar(&flagDeploySlug, "slug", "", "deploy only the challenge with this slug")
	rootCmd.AddCommand(deployCmd)

	downCmd.Flags().BoolVar(&flagDownDryRun, "dry-run", false, "print docker compose commands instead of executing")
	downCmd.Flags().StringVar(&flagDownMode, "mode", "", "override deploy mode: docker, ssh or kubernetes")
	downCmd.Flags().BoolVar(&flagDownVolumes, "volumes", false, "also strip named volumes (-v on docker compose down)")
	downCmd.Flags().StringVar(&flagDownCat, "category", "", "tear down only challenges in this category")
	downCmd.Flags().StringVar(&flagDownSlug, "slug", "", "tear down only the challenge with this slug")
	rootCmd.AddCommand(downCmd)
}
