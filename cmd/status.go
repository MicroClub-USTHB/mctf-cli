package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/status"
)

var flagStatusTimeout time.Duration

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show runtime state for every challenge in the event",
	Long: "Walks the event repo and reports the runtime state of each\n" +
		"challenge by interrogating `docker compose ps`.\n\n" +
		"Static challenges report state=\"static\" (no runtime). Instanced\n" +
		"challenges report state=\"instanced\" — they're spawned per team\n" +
		"by the instancer at game time. Service-archetype challenges\n" +
		"report the actual compose-project state (running, exited, etc.).\n\n" +
		"With --json, emits machine-readable output suitable for dashboards.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), flagStatusTimeout)
		defer cancel()

		rows, err := status.Run(ctx, cfg, challs, nil)
		if err != nil {
			return err
		}

		if flagJSON {
			body, err := status.JSON(rows)
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		}
		fmt.Print(status.Render(rows))
		return nil
	},
}

func init() {
	statusCmd.Flags().DurationVar(&flagStatusTimeout, "timeout", 30*time.Second, "kill the docker compose ps probe after this duration")
	rootCmd.AddCommand(statusCmd)
}
