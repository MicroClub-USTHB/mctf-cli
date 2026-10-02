package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/instancer"
	"github.com/MicroClub-USTHB/mctf-cli/internal/solve"
)

var (
	flagSolveAll      bool
	flagSolveTimeout  time.Duration
	flagSolveCategory string
	flagSolveInstance string
)

var solveCmd = &cobra.Command{
	Use:   "solve [<category>/<slug>]",
	Short: "Run a challenge's solver and verify the recovered flag",
	Long: "Smoke-tests a challenge end-to-end: runs solution/solve.sh\n" +
		"(or solve.py / solve binary) with HOST / PORT / URL / FLAG_PREFIX\n" +
		"env vars derived from the deploy block, and asserts that one of\n" +
		"the manifest flags appears in the output.\n\n" +
		"With --all, runs every discovered solver in turn and exits non-\n" +
		"zero if any one fails. Useful as a final CI gate after deploy.\n\n" +
		"With --against-instance <id>, runs the solver against a live per-\n" +
		"team instance (spawned via `mctf instance spawn`): HOST/PORT/URL\n" +
		"point at the instance's host-port mapping and its per-team flag is\n" +
		"accepted. The challenge is resolved from the instance record, so no\n" +
		"<category>/<slug> argument is needed.\n\n" +
		"Examples:\n" +
		"  mctf solve misc/welcome\n" +
		"  mctf solve --all\n" +
		"  mctf solve --all --category web --timeout 30s\n" +
		"  mctf solve --against-instance mctf-inst-rubik-team1-ab12cd34",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		if flagSolveInstance != "" {
			if flagSolveAll {
				return fmt.Errorf("--against-instance cannot be combined with --all")
			}
			if len(args) > 0 {
				return fmt.Errorf("--against-instance takes no <category>/<slug> argument; the challenge is resolved from the instance")
			}
		}

		var targets []*challenge.Challenge
		var instTarget *solve.InstanceTarget
		switch {
		case flagSolveInstance != "":
			store, err := openStore()
			if err != nil {
				return err
			}
			c, it, err := resolveInstanceTarget(store, challs, flagSolveInstance)
			if err != nil {
				return err
			}
			targets = append(targets, c)
			instTarget = it
		case flagSolveAll:
			for _, c := range challs {
				if flagSolveCategory != "" && c.CategoryDir != flagSolveCategory {
					continue
				}
				if _, err := solve.FindSolver(c); err == nil {
					targets = append(targets, c)
				}
			}
			if len(targets) == 0 {
				fmt.Println("no challenges with solvers matched the filters")
				return nil
			}
		case len(args) == 1:
			category, slug, ok := splitCategorySlug(args[0])
			if !ok {
				return fmt.Errorf("argument must be <category>/<slug>, got %q", args[0])
			}
			for _, c := range challs {
				if c.CategoryDir == category && c.Slug == slug {
					targets = append(targets, c)
					break
				}
			}
			if len(targets) == 0 {
				return fmt.Errorf("no challenge at %s/%s%s", category, slug, didYouMean(challs, args[0]))
			}
		default:
			return fmt.Errorf("either pass <category>/<slug> or use --all")
		}

		ctx := context.Background()
		opts := solve.Options{Timeout: flagSolveTimeout, Instance: instTarget}
		if !flagJSON {
			opts.Stdout = os.Stdout
			opts.Stderr = os.Stderr
		}

		var results []*solve.Result
		var failed int
		for _, c := range targets {
			res, err := solve.Run(ctx, cfg, c, opts)
			if err != nil {
				if errors.Is(err, solve.ErrNoSolver) {
					if !flagJSON {
						fmt.Printf("? %s — %s\n", c.CategoryDir+"/"+c.Slug, err)
					}
					continue
				}
				return fmt.Errorf("%s/%s: %w", c.CategoryDir, c.Slug, err)
			}
			results = append(results, res)
			if !res.Pass {
				failed++
			}
			if !flagJSON {
				printResult(res)
			}
		}

		if flagJSON {
			body, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(body))
		}

		if failed > 0 {
			return fmt.Errorf("%d solver(s) failed", failed)
		}
		return nil
	},
}

// instanceGetter is the slice of the instancer store that
// resolveInstanceTarget needs — narrowed to an interface so the
// resolution is unit-testable without a real store or docker daemon.
type instanceGetter interface {
	Get(id string) (instancer.Instance, bool)
}

// resolveInstanceTarget looks up an instance by id and pairs it with
// the discovered challenge it spawned from, returning the solver
// target (host-port mapping + per-team flag).
func resolveInstanceTarget(store instanceGetter, challs []*challenge.Challenge, id string) (*challenge.Challenge, *solve.InstanceTarget, error) {
	inst, ok := store.Get(id)
	if !ok {
		return nil, nil, fmt.Errorf("no instance with id %q", id)
	}
	category, slug, ok := splitCategorySlug(inst.ChallengeSlug)
	if !ok {
		return nil, nil, fmt.Errorf("instance %q has malformed challenge slug %q", inst.ID, inst.ChallengeSlug)
	}
	for _, c := range challs {
		if c.CategoryDir == category && c.Slug == slug {
			return c, &solve.InstanceTarget{
				ID:     inst.ID,
				TeamID: inst.TeamID,
				Ports:  inst.Ports,
				Flag:   inst.Flag,
			}, nil
		}
	}
	return nil, nil, fmt.Errorf("instance %q references unknown challenge %s", inst.ID, inst.ChallengeSlug)
}

func printResult(res *solve.Result) {
	mark := "✗"
	if res.Pass {
		mark = "✓"
	}
	fmt.Printf("\n%s %s — %s (%s)\n", mark, res.Challenge, res.Reason, res.Duration.Round(time.Millisecond))
	if res.Flag != "" {
		fmt.Printf("  flag: %s\n", res.Flag)
	}
}

func init() {
	solveCmd.Flags().BoolVar(&flagSolveAll, "all", false, "run every discovered solver in turn")
	solveCmd.Flags().DurationVar(&flagSolveTimeout, "timeout", 60*time.Second, "kill a solver after this duration")
	solveCmd.Flags().StringVar(&flagSolveCategory, "category", "", "with --all, restrict to challenges in this category")
	solveCmd.Flags().StringVar(&flagSolveInstance, "against-instance", "", "run the solver against a live per-team instance by id")
	rootCmd.AddCommand(solveCmd)
}
