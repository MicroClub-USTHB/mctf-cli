package cmd

import (
	"path/filepath"

	"github.com/spf13/cobra"
)

// relPath renders p relative to base for human display, falling back
// gracefully when p and base aren't in compatible forms (one absolute,
// the other relative). Always returns a non-empty string.
func relPath(base, p string) string {
	absP, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	rel, err := filepath.Rel(base, absP)
	if err != nil {
		return absP
	}
	return rel
}

var (
	flagVerbose bool
	flagJSON    bool
	flagDir     string
)

var rootCmd = &cobra.Command{
	Use:   "mctf",
	Short: "MCTF — Modern CTF Framework CLI",
	Long: `MCTF turns a CTF into reproducible Infrastructure-as-Code.

A single mctf.config.yml at the event repo root, plus the per-challenge
on-disk contract under challenges/, gives you scaffolding, validation,
build, deploy, instancing, and platform integration through one binary.

See docs/spec/ for the contract MCTF operates on.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose logging")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "machine-readable JSON output where supported")
	rootCmd.PersistentFlags().StringVarP(&flagDir, "dir", "C", ".", "event repo root (where mctf.config.yml lives)")
}

// Execute runs the root command and returns any error to main.
func Execute() error {
	return rootCmd.Execute()
}
