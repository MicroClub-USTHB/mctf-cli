package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts for mctf",
	Long: "Generate the auto-completion script for the named shell.\n\n" +
		"bash:\n" +
		"  source <(mctf completion bash)\n" +
		"  # or persist:\n" +
		"  mctf completion bash > /etc/bash_completion.d/mctf\n\n" +
		"zsh:\n" +
		"  echo \"autoload -U compinit; compinit\" >> ~/.zshrc\n" +
		"  mctf completion zsh > \"${fpath[1]}/_mctf\"\n\n" +
		"fish:\n" +
		"  mctf completion fish > ~/.config/fish/completions/mctf.fish\n\n" +
		"powershell:\n" +
		"  mctf completion powershell > mctf.ps1\n" +
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
