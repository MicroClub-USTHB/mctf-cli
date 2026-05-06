package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/validate"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the event repo against the MTCF spec",
	Long: `Walks the event repo, parses mtcf.config.yml and every challenge
manifest, then runs every hard-fail and warning rule from
docs/spec/validation.md.

Exits non-zero on any error-severity rule violation. Warnings are reported
but do not fail the build.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, loadErrs, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}
		report := validate.Run(cfg, flagDir, challs, loadErrs)
		fmt.Print(report.String())

		if report.HasErrors() {
			e, w, _ := report.Counts()
			return fmt.Errorf("validation failed: %d error(s), %d warning(s)", e, w)
		}
		fmt.Printf("OK — %d challenge(s) validated\n", len(challs))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
