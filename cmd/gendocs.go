package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// genManCmd generates a man page per command. Hidden: it's a build/CI helper
// (`make man`), not part of the user-facing command set.
var genManCmd = &cobra.Command{
	Use:    "gen-man <dir>",
	Short:  "Generate man pages for every command into <dir>",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		header := &doc.GenManHeader{
			Title:   "MCTF",
			Section: "1",
			Source:  "MCTF CLI",
			Manual:  "MCTF Manual",
		}
		return doc.GenManTree(rootCmd, header, args[0])
	},
}

func init() {
	rootCmd.AddCommand(genManCmd)
}
