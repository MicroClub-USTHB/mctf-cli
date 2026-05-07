package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/doctor"
)

var flagDoctorSkipEnv bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Preflight: verify the event repo and host are deploy-ready",
	Long: "Runs a series of preflight checks against the event repo and the\n" +
		"host environment:\n\n" +
		"  D001  event config valid\n" +
		"  D002  challenges directory exists with at least one challenge\n" +
		"  D003  every discovered challenge passes validation\n" +
		"  D010  docker binary present\n" +
		"  D011  docker daemon reachable\n" +
		"  D012  docker compose v2 available\n" +
		"  D013  git binary present\n" +
		"  D020  registry credentials env vars set\n" +
		"  D021  CTFd token env var set\n\n" +
		"Exits non-zero only when at least one check has failed.\n" +
		"Warnings are reported but never fail the run.",
	RunE: func(cmd *cobra.Command, args []string) error {
		checks := doctor.Run(flagDir, doctor.Options{SkipEnv: flagDoctorSkipEnv})
		if flagJSON {
			out, err := doctor.JSON(checks)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
		} else {
			fmt.Print(doctor.Render(checks))
		}
		_, fail, _, _ := doctor.Counts(checks)
		if fail > 0 {
			return fmt.Errorf("doctor: %d failing check(s)", fail)
		}
		return nil
	},
}

func init() {
	doctorCmd.Flags().BoolVar(&flagDoctorSkipEnv, "skip-env", false, "skip host-environment probes (docker, env vars)")
	rootCmd.AddCommand(doctorCmd)
}
