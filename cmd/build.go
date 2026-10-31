package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/build"
	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/plugin"
)

var (
	flagBuildDryRun  bool
	flagBuildNoCache bool
	flagBuildPush    bool
	flagBuildCat     string
	flagBuildSlug    string
	flagBuildSHA     string
	flagBuildScan    bool
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build container images for every deployable challenge",
	Long: "Builds one image per challenge whose need_deploy is true.\n\n" +
		"Image tag format: <registry>/<event>-<category>-<slug>:<sha>\n" +
		"Plus a parallel <...>:latest tag.\n\n" +
		"Each image is labeled with mctf.event, mctf.category, mctf.challenge,\n" +
		"and mctf.archetype so the runtime tooling can identify them later.\n\n" +
		"With --dry-run, prints the planned `docker build` commands without\n" +
		"executing them — useful for review before a real build, and for CI\n" +
		"sanity checks.\n\n" +
		"Examples:\n" +
		"  mctf build --dry-run\n" +
		"  mctf build --category web\n" +
		"  mctf build --slug cub3-clone --no-cache\n" +
		"  mctf build --push",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		sha := flagBuildSHA
		if sha == "" {
			sha = build.GitSHA(flagDir)
		}

		opts := build.Options{
			CategoryFilter: flagBuildCat,
			SlugFilter:     flagBuildSlug,
			SHA:            sha,
			NoCache:        flagBuildNoCache,
			Push:           flagBuildPush,
		}

		plan, err := build.Plan(cfg, flagDir, challs, opts)
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("nothing to build — no deployable challenges matched the filters")
			return nil
		}

		abs, _ := filepath.Abs(flagDir)
		fmt.Printf("planned: %d image(s)\n", len(plan))
		for _, spec := range plan {
			fmt.Printf("  %-40s -> %s\n", relPath(abs, spec.Context), spec.Tags[0])
		}
		fmt.Println()

		var runner build.Runner
		if flagBuildDryRun {
			runner = build.NewDryRunner()
		} else {
			runner = build.NewDockerRunner()
		}

		ctx := context.Background()

		// Lifecycle plugins (best-effort; a no-op without a `plugins:` block).
		pm := plugin.Load(ctx, cfg.Plugins, plugin.Options{HostVersion: version, Dir: flagDir})
		defer pm.Close()
		if err := pm.Dispatch(ctx, plugin.HookBeforeBuild, buildHookPayload(cfg, opts, "before", flagBuildDryRun, plan, "", nil)); err != nil {
			return fmt.Errorf("build blocked by plugin: %w", err)
		}

		buildOpts := opts
		buildOpts.Push = false
		if err := build.Apply(ctx, runner, plan, buildOpts); err != nil {
			_ = pm.Dispatch(ctx, plugin.HookAfterBuild, buildHookPayload(cfg, opts, "after", flagBuildDryRun, plan, "failed", err))
			return err
		}

		if !flagBuildDryRun && flagBuildScan {
			if err := scanBuiltImages(ctx, plan); err != nil {
				_ = pm.Dispatch(ctx, plugin.HookAfterBuild, buildHookPayload(cfg, opts, "after", flagBuildDryRun, plan, "failed", err))
				return err
			}
		}

		if flagBuildPush {
			fmt.Printf("\npushing %d image(s) to registry...\n", len(plan))
			if err := build.ApplyPush(ctx, runner, plan, opts); err != nil {
				_ = pm.Dispatch(ctx, plugin.HookAfterBuild, buildHookPayload(cfg, opts, "after", flagBuildDryRun, plan, "failed", err))
				return err
			}
		}

		if err := pm.Dispatch(ctx, plugin.HookAfterBuild, buildHookPayload(cfg, opts, "after", flagBuildDryRun, plan, "success", nil)); err != nil {
			return fmt.Errorf("build completed, but plugin hook failed: %w", err)
		}
		fmt.Println("\nbuild complete")
		return nil
	},
}

// buildHookPayload builds the plugin hook body for a build run.
func buildHookPayload(cfg *config.Config, opts build.Options, phase string, dryRun bool, plan []build.ImageSpec, outcome string, err error) plugin.BuildPayload {
	eventSlug := ""
	if cfg != nil {
		eventSlug = cfg.Event.Slug
	}
	p := plugin.BuildPayload{
		Event:   eventSlug,
		Phase:   phase,
		SHA:     opts.SHA,
		NoCache: opts.NoCache,
		Push:    opts.Push,
		DryRun:  dryRun,
		Outcome: outcome,
	}
	if err != nil {
		p.Error = err.Error()
	}
	for _, spec := range plan {
		p.Images = append(p.Images, plugin.BuildImage{
			Challenge: spec.Challenge,
			Category:  spec.Category,
			Slug:      spec.Slug,
			Tags:      spec.Tags,
			Labels:    spec.Labels,
		})
	}
	return p
}

// scanBuiltImages runs the post-build leak scan for every spec in
// plan and aborts on the first hit.
func scanBuiltImages(ctx context.Context, plan []build.ImageSpec) error {
	for _, spec := range plan {
		if len(spec.FlagValues) == 0 || len(spec.Tags) == 0 {
			continue
		}
		fmt.Printf("\nscanning %s for leaked flag values...\n", spec.Tags[0])
		hits, err := build.ScanImage(ctx, spec.Tags[0], spec.FlagValues)
		if err != nil {
			return fmt.Errorf("%s: scan failed: %w", spec.Challenge, err)
		}
		if len(hits) > 0 {
			for _, h := range hits {
				fmt.Fprintf(os.Stderr, "  LEAK %s: %s contains %s\n", spec.Challenge, h.Path, h.Flag)
			}
			return fmt.Errorf("%s: %d file(s) contain a manifest flag value — fix and rebuild (or load the flag from env at runtime)",
				spec.Challenge, len(hits))
		}
		fmt.Printf("  ok — no flag values in image filesystem\n")
	}
	return nil
}

func init() {
	buildCmd.Flags().BoolVar(&flagBuildDryRun, "dry-run", false, "print the docker commands instead of running them")
	buildCmd.Flags().BoolVar(&flagBuildNoCache, "no-cache", false, "pass --no-cache to docker build")
	buildCmd.Flags().BoolVar(&flagBuildPush, "push", false, "push images to the registry after building")
	buildCmd.Flags().StringVar(&flagBuildCat, "category", "", "build only challenges in this category")
	buildCmd.Flags().StringVar(&flagBuildSlug, "slug", "", "build only the challenge with this slug")
	buildCmd.Flags().StringVar(&flagBuildSHA, "sha", "", "image tag sha override (default: short HEAD or 'dev')")
	buildCmd.Flags().BoolVar(&flagBuildScan, "scan", true, "post-build scan for manifest flag values inside the image (use --scan=false to disable)")
	rootCmd.AddCommand(buildCmd)
}
