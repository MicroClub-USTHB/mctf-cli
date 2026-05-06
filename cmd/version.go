package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Populated at build time via -ldflags.
var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the MTCF CLI version",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagJSON {
			fmt.Printf(`{"version":%q,"commit":%q,"date":%q}`+"\n", version, commit, date)
			return nil
		}
		fmt.Printf("mtcf %s (commit %s, built %s)\n", version, commit, date)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
