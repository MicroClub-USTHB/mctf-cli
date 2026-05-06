package cmd

import (
	"github.com/spf13/cobra"
)

var (
	flagVerbose bool
	flagJSON    bool
	flagDir     string
)

var rootCmd = &cobra.Command{
	Use:   "mtcf",
	Short: "MTCF — Modern CTF Framework CLI",
	Long: `MTCF turns a CTF into reproducible Infrastructure-as-Code.

A single mtcf.config.yml at the event repo root, plus the per-challenge
on-disk contract under challenges/, gives you scaffolding, validation,
build, deploy, instancing, and platform integration through one binary.

See docs/spec/ for the contract MTCF operates on.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose logging")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "machine-readable JSON output where supported")
	rootCmd.PersistentFlags().StringVarP(&flagDir, "dir", "C", ".", "event repo root (where mtcf.config.yml lives)")
}

// Execute runs the root command and returns any error to main.
func Execute() error {
	return rootCmd.Execute()
}
