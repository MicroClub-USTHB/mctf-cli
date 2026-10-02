package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/generate"
	"github.com/MicroClub-USTHB/mctf-cli/internal/validate"
)

var (
	flagCheck bool
	flagClean bool
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "(Re)generate the derived files (CTFd manifest, instance.toml, CSV, index.json)",
	Long: `Reads each author manifest under the event repo and writes the
auto-generated files alongside them:

  • challenges/<cat>/<slug>/challenge.yml  (CTFd format)
  • challenges/<cat>/<slug>/instance.toml  (only for instanced challenges)
  • challenges/challenges.csv              (event-wide tracking sheet)
  • challenges/index.json                  (machine-parseable index)

With --check, exits non-zero if any committed derived file would change —
use this in CI to enforce that derived files stay in sync with their sources.

With --clean, removes stale derived files (e.g. an instance.toml from a
challenge that's no longer instanced).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, loadErrs, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		// Hard-fail before any generation if validation would reject this repo.
		report := validate.Run(cfg, flagDir, challs, loadErrs)
		if report.HasErrors() {
			fmt.Print(report.String())
			e, w, _ := report.Counts()
			return fmt.Errorf("validation failed: %d error(s), %d warning(s) — fix these before generating", e, w)
		}

		out, err := generate.Plan(cfg, flagDir, challs)
		if err != nil {
			return err
		}

		if flagCheck {
			drifts, err := out.Diff()
			if err != nil {
				return err
			}
			if flagJSON {
				body, err := generateCheckJSON(drifts, flagDir)
				if err != nil {
					return err
				}
				fmt.Println(string(body))
				if len(drifts) > 0 {
					return fmt.Errorf("%d derived file(s) out of sync — run `mctf generate`", len(drifts))
				}
				return nil
			}
			if len(drifts) == 0 {
				fmt.Println("derived files are up to date")
				return nil
			}
			for _, d := range drifts {
				rel, _ := filepath.Rel(flagDir, d.Path)
				fmt.Printf("  %-8s %s\n", d.Reason, rel)
			}
			return fmt.Errorf("%d derived file(s) out of sync — run `mctf generate`", len(drifts))
		}

		written, err := out.Apply()
		if err != nil {
			return err
		}
		var removed []string
		if flagClean {
			removed, err = out.CleanStale(cfg, flagDir, challs)
			if err != nil {
				return err
			}
		}
		if flagJSON {
			body, err := generateApplyJSON(written, removed, flagDir)
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		}
		for _, p := range written {
			rel, _ := filepath.Rel(flagDir, p)
			fmt.Printf("  wrote   %s\n", rel)
		}
		for _, p := range removed {
			rel, _ := filepath.Rel(flagDir, p)
			fmt.Printf("  removed %s\n", rel)
		}
		if len(written) == 0 && len(removed) == 0 {
			fmt.Println("nothing to do — derived files already in sync")
		}
		return nil
	},
}

// generateReport is the JSON shape for `mctf generate --json`. CI tooling
// keys off `mode` and `in_sync`; the rest is informational.
type generateReport struct {
	Mode    string     `json:"mode"`              // "apply" | "check"
	InSync  bool       `json:"in_sync"`           // false only in check mode when drift exists
	Written []string   `json:"written,omitempty"` // apply mode
	Removed []string   `json:"removed,omitempty"` // apply mode
	Drift   []driftRow `json:"drift,omitempty"`   // check mode
}

type driftRow struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func generateCheckJSON(drifts []generate.Drift, eventRoot string) ([]byte, error) {
	rep := generateReport{Mode: "check", InSync: len(drifts) == 0}
	for _, d := range drifts {
		rel, _ := filepath.Rel(eventRoot, d.Path)
		rep.Drift = append(rep.Drift, driftRow{Path: rel, Reason: d.Reason})
	}
	sort.Slice(rep.Drift, func(i, j int) bool { return rep.Drift[i].Path < rep.Drift[j].Path })
	return json.MarshalIndent(rep, "", "  ")
}

func generateApplyJSON(written, removed []string, eventRoot string) ([]byte, error) {
	rep := generateReport{Mode: "apply", InSync: true}
	for _, p := range written {
		rel, _ := filepath.Rel(eventRoot, p)
		rep.Written = append(rep.Written, rel)
	}
	for _, p := range removed {
		rel, _ := filepath.Rel(eventRoot, p)
		rep.Removed = append(rep.Removed, rel)
	}
	sort.Strings(rep.Written)
	sort.Strings(rep.Removed)
	return json.MarshalIndent(rep, "", "  ")
}

func init() {
	generateCmd.Flags().BoolVar(&flagCheck, "check", false, "exit non-zero if any derived file would change (CI mode)")
	generateCmd.Flags().BoolVar(&flagClean, "clean", false, "also remove stale derived files (orphaned instance.toml etc.)")
	rootCmd.AddCommand(generateCmd)
}
