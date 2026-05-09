package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/scaffold"
)

var (
	flagInitForce   bool
	flagInitDomain  string
	flagInitProfile string
	flagInitSlug    string
	flagInitNoTTY   bool
)

var initCmd = &cobra.Command{
	Use:   "init [event-name]",
	Short: "Scaffold a new MCTF event repository",
	Long: "Creates a new event repo at the path given by -C (default: cwd):\n\n" +
		"  mctf.config.yml         # event config (filled with the name + slug)\n" +
		"  README.md               # internal handbook for the event repo\n" +
		"  .gitignore              # MCTF working dir + secrets\n" +
		"  .env.example            # registry / CTFd / webhook env vars\n" +
		"  .github/workflows/      # CI that runs `mctf validate` + `generate --check`\n" +
		"  challenges/             # empty — drop your first challenge here\n" +
		"  templates/challenge_name/   # local starter template\n\n" +
		"Refuses to overwrite an existing mctf.config.yml unless --force is set.\n\n" +
		"Examples:\n" +
		"  mctf init                              # uses dir name as event name\n" +
		"  mctf init \"MCTF 6.0\"                   # explicit name\n" +
		"  mctf init \"MCTF 6.0\" --domain mctf.microclub.info --profile instanced\n" +
		"  mctf init -C ./my-event \"MCTF 6.0\"",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := flagDir
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}

		var name string
		if len(args) == 1 && args[0] != "" {
			name = args[0]
		} else {
			name = filepath.Base(abs)
		}

		vars := scaffold.NewEventVars(name)
		if flagInitSlug != "" {
			vars.EventSlug = scaffold.Slugify(flagInitSlug)
		}
		if flagInitDomain != "" {
			vars.Domain = flagInitDomain
		}
		if flagInitProfile != "" {
			vars.Profile = flagInitProfile
		}

		// Interactive prompts: only when stdin is a terminal AND the
		// user didn't pass the flag for that field. --non-interactive
		// forces the silent path even on a tty.
		interactive := !flagInitNoTTY && stdinIsTTY()
		if interactive {
			if len(args) == 0 {
				if got := promptString("event name", vars.EventName); got != "" {
					vars.EventName = got
					vars.EventSlug = scaffold.Slugify(got)
				}
			}
			if flagInitDomain == "" {
				if got := promptString("event domain", vars.Domain); got != "" {
					vars.Domain = got
				}
			}
			if flagInitProfile == "" {
				if got := promptChoice("profile", vars.Profile, []string{"simple", "instanced", "pro"}); got != "" {
					vars.Profile = got
				}
			}
		}

		opts := scaffold.EventOptions{Force: flagInitForce}
		res, err := scaffold.Event(dir, vars, opts)
		if err != nil {
			if errors.Is(err, scaffold.ErrAlreadyInitialized) {
				return err
			}
			return fmt.Errorf("scaffold: %w", err)
		}

		for _, p := range res.Written {
			fmt.Printf("  wrote   %s\n", relPath(abs, p))
		}
		for _, p := range res.Skipped {
			fmt.Fprintf(os.Stderr, "  skipped %s (already exists; --force to overwrite)\n", relPath(abs, p))
		}
		fmt.Printf("\nevent %q scaffolded at %s\n", vars.EventName, abs)
		fmt.Printf("next:\n")
		fmt.Printf("  cd %s\n", abs)
		fmt.Printf("  mctf validate\n")
		fmt.Printf("  cp -r templates/challenge_name challenges/<category>/<your-slug>\n")
		return nil
	},
}

// stdinIsTTY reports whether stdin is connected to a terminal. We avoid
// pulling in golang.org/x/term and instead use the Stat-based check,
// which is portable across darwin/linux/windows for our use case.
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

var promptIn io.Reader = os.Stdin // overridable in tests

// promptString asks the user for a value; an empty answer keeps the default.
func promptString(label, def string) string {
	fmt.Fprintf(os.Stderr, "%s [%s]: ", label, def)
	r := bufio.NewReader(promptIn)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}

// promptChoice asks the user to pick from `choices`. Empty answer
// keeps `def`. Invalid answers re-prompt.
func promptChoice(label, def string, choices []string) string {
	for {
		fmt.Fprintf(os.Stderr, "%s (%s) [%s]: ", label, strings.Join(choices, "/"), def)
		r := bufio.NewReader(promptIn)
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return ""
		}
		ans := strings.TrimSpace(line)
		if ans == "" {
			return ""
		}
		for _, c := range choices {
			if c == ans {
				return ans
			}
		}
		fmt.Fprintf(os.Stderr, "  invalid — pick one of %s\n", strings.Join(choices, ", "))
	}
}

func init() {
	initCmd.Flags().BoolVar(&flagInitForce, "force", false, "overwrite existing files (destructive)")
	initCmd.Flags().StringVar(&flagInitDomain, "domain", "", "event domain (default: ctf.example.com)")
	initCmd.Flags().StringVar(&flagInitProfile, "profile", "", "deploy profile: simple | instanced | pro (default: instanced)")
	initCmd.Flags().StringVar(&flagInitSlug, "slug", "", "event slug override (default: derived from name)")
	initCmd.Flags().BoolVar(&flagInitNoTTY, "non-interactive", false, "skip prompts even when stdin is a terminal")
	rootCmd.AddCommand(initCmd)
}
