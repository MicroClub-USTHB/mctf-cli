package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/validate"
)

var categoriesCmd = &cobra.Command{
	Use:   "categories",
	Short: "List the active category catalog (core + extended + custom)",
	Long: "Resolves the per-event category catalog using mctf.config.yml's\n" +
		"categories.extended and categories.custom additions on top of the\n" +
		"core 10 categories.\n\n" +
		"With --json, emits a machine-readable view.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		cats := validate.ResolveCategories(cfg)

		slugs := make([]string, 0, len(cats))
		for k := range cats {
			slugs = append(slugs, k)
		}
		sort.Strings(slugs)

		if flagJSON {
			rows := make([]map[string]any, 0, len(slugs))
			for _, s := range slugs {
				ci := cats[s]
				row := map[string]any{
					"slug":       s,
					"name":       ci.Name,
					"deployable": ci.Deployable,
				}
				if !ci.Range.Empty() {
					row["port_range"] = []int{ci.Range.Min, ci.Range.Max}
				}
				rows = append(rows, row)
			}
			body, err := json.MarshalIndent(rows, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "SLUG\tNAME\tDEPLOYABLE\tPORT RANGE")
		coreSlugs := map[string]bool{}
		for k := range validate.CoreCategories {
			coreSlugs[k] = true
		}
		var nCore, nExt, nCustom int
		extSlugs := map[string]bool{}
		for _, e := range validate.ExtendedCategories {
			extSlugs[e.Slug] = true
		}
		for _, s := range slugs {
			ci := cats[s]
			deployable := "no"
			if ci.Deployable {
				deployable = "yes"
			}
			rng := "-"
			if !ci.Range.Empty() {
				rng = fmt.Sprintf("%d-%d", ci.Range.Min, ci.Range.Max)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s, ci.Name, deployable, rng)
			switch {
			case coreSlugs[s]:
				nCore++
			case extSlugs[s]:
				nExt++
			default:
				nCustom++
			}
		}
		tw.Flush()
		fmt.Fprintf(os.Stdout, "\n%d active categories (%d core, %d extended, %d custom)\n",
			len(slugs), nCore, nExt, nCustom)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(categoriesCmd)
}
