package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/scaffold"
)

var (
	flagInitForce   bool
	flagInitDomain  string
	flagInitProfile string
	flagInitSlug    string
)

var initCmd = &cobra.Command{
	Use:   "init [event-name]",
	Short: "Scaffold a new MTCF event repository",
	Long: "Creates a new event repo at the path given by -C (default: cwd):\n\n" +
		"  mtcf.config.yml         # event config (filled with the name + slug)\n" +
		"  README.md               # internal handbook for the event repo\n" +
		"  .gitignore              # MTCF working dir + secrets\n" +
		"  .env.example            # registry / CTFd / webhook env vars\n" +
		"  .github/workflows/      # CI that runs `mtcf validate` + `generate --check`\n" +
		"  challenges/             # empty — drop your first challenge here\n" +
		"  templates/challenge_name/   # local starter template\n\n" +
		"Refuses to overwrite an existing mtcf.config.yml unless --force is set.\n\n" +
		"Examples:\n" +
		"  mtcf init                              # uses dir name as event name\n" +
		"  mtcf init \"MCTF 6.0\"                   # explicit name\n" +
		"  mtcf init \"MCTF 6.0\" --domain mctf.microclub.info --profile instanced\n" +
		"  mtcf init -C ./my-event \"MCTF 6.0\"",
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
		fmt.Printf("  mtcf validate\n")
		fmt.Printf("  cp -r templates/challenge_name challenges/<category>/<your-slug>\n")
		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&flagInitForce, "force", false, "overwrite existing files (destructive)")
	initCmd.Flags().StringVar(&flagInitDomain, "domain", "", "event domain (default: ctf.example.com)")
	initCmd.Flags().StringVar(&flagInitProfile, "profile", "", "deploy profile: simple | instanced | pro (default: instanced)")
	initCmd.Flags().StringVar(&flagInitSlug, "slug", "", "event slug override (default: derived from name)")
	rootCmd.AddCommand(initCmd)
}
