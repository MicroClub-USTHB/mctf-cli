package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/build"
	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

var (
	flagBuildDryRun  bool
	flagBuildNoCache bool
	flagBuildPush    bool
	flagBuildCat     string
	flagBuildSlug    string
	flagBuildSHA     string
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build container images for every deployable challenge",
	Long: "Builds one image per challenge whose need_deploy is true.\n\n" +
		"Image tag format: <registry>/<event>-<category>-<slug>:<sha>\n" +
		"Plus a parallel <...>:latest tag.\n\n" +
		"Each image is labeled with mtcf.event, mtcf.category, mtcf.challenge,\n" +
		"and mtcf.archetype so the runtime tooling can identify them later.\n\n" +
		"With --dry-run, prints the planned `docker build` commands without\n" +
		"executing them — useful for review before a real build, and for CI\n" +
		"sanity checks.\n\n" +
		"Examples:\n" +
		"  mtcf build --dry-run\n" +
		"  mtcf build --category web\n" +
		"  mtcf build --slug cub3-clone --no-cache\n" +
		"  mtcf build --push",
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
		if err := build.Apply(ctx, runner, plan, opts); err != nil {
			return err
		}
		fmt.Println("\nbuild complete")
		return nil
	},
}

func init() {
	buildCmd.Flags().BoolVar(&flagBuildDryRun, "dry-run", false, "print the docker commands instead of running them")
	buildCmd.Flags().BoolVar(&flagBuildNoCache, "no-cache", false, "pass --no-cache to docker build")
	buildCmd.Flags().BoolVar(&flagBuildPush, "push", false, "push images to the registry after building")
	buildCmd.Flags().StringVar(&flagBuildCat, "category", "", "build only challenges in this category")
	buildCmd.Flags().StringVar(&flagBuildSlug, "slug", "", "build only the challenge with this slug")
	buildCmd.Flags().StringVar(&flagBuildSHA, "sha", "", "image tag sha override (default: short HEAD or 'dev')")
	rootCmd.AddCommand(buildCmd)
}
