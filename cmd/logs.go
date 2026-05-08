package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/deploy"
)

var (
	flagLogsFollow bool
	flagLogsTail   int
)

var logsCmd = &cobra.Command{
	Use:   "logs <category>/<slug>",
	Short: "Stream logs from a deployed challenge's compose project",
	Long: "Thin wrapper around `docker compose logs` for the project that\n" +
		"`mtcf deploy` brought up.\n\n" +
		"Examples:\n" +
		"  mtcf logs misc/welcome\n" +
		"  mtcf logs misc/welcome -f\n" +
		"  mtcf logs misc/welcome --tail 50",
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
		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		var found *challenge.Challenge
		for _, c := range challs {
			if c.CategoryDir == category && c.Slug == slug {
				found = c
				break
			}
		}
		if found == nil {
			return fmt.Errorf("no challenge at %s/%s%s", category, slug, didYouMean(challs, args[0]))
		}
		if !found.Manifest.Challenge.NeedDeploy {
			return fmt.Errorf("%s/%s is a static challenge — nothing to log", category, slug)
		}
		if !found.HasCompose() {
			return fmt.Errorf("%s/%s: challenge/docker-compose.yml is missing", category, slug)
		}

		project := deploy.ProjectName(cfg, found)
		composeFile := found.ChallengeDir + "/docker-compose.yml"

		dockerArgs := []string{"compose", "-p", project, "-f", composeFile, "logs"}
		if flagLogsFollow {
			dockerArgs = append(dockerArgs, "-f")
		}
		if flagLogsTail > 0 {
			dockerArgs = append(dockerArgs, "--tail", strconv.Itoa(flagLogsTail))
		}

		// Forward SIGINT/SIGTERM to the docker subprocess so Ctrl-C
		// doesn't drop a stray dockerd-managed `compose logs -f`.
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		c := exec.CommandContext(ctx, "docker", dockerArgs...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		if err := c.Run(); err != nil {
			// In follow mode, exit-on-signal is normal; suppress that case.
			if flagLogsFollow && ctx.Err() != nil {
				return nil
			}
			return err
		}
		return nil
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&flagLogsFollow, "follow", "f", false, "follow log output (Ctrl-C to stop)")
	logsCmd.Flags().IntVar(&flagLogsTail, "tail", 0, "show only the last N lines (0 = all)")
	rootCmd.AddCommand(logsCmd)
}
