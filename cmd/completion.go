package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts for mtcf",
	Long: "Generate the auto-completion script for the named shell.\n\n" +
		"bash:\n" +
		"  source <(mtcf completion bash)\n" +
		"  # or persist:\n" +
		"  mtcf completion bash > /etc/bash_completion.d/mtcf\n\n" +
		"zsh:\n" +
		"  echo \"autoload -U compinit; compinit\" >> ~/.zshrc\n" +
		"  mtcf completion zsh > \"${fpath[1]}/_mtcf\"\n\n" +
		"fish:\n" +
		"  mtcf completion fish > ~/.config/fish/completions/mtcf.fish\n\n" +
		"powershell:\n" +
		"  mtcf completion powershell > mtcf.ps1\n" +
		"  # then source it from your profile",
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			return cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
