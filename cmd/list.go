package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

var (
	flagListCategory   string
	flagListArchetype  string
	flagListWave       int
	flagListDifficulty string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List discovered challenges with archetype, difficulty, wave",
	Long: "Walks the event repo, prints one row per challenge.\n\n" +
		"Filters compose: combining --category and --archetype narrows to\n" +
		"challenges that match both. With --json, emits a machine-readable\n" +
		"array (same fields as challenges/index.json's per-challenge shape).\n\n" +
		"Examples:\n" +
		"  mctf list\n" +
		"  mctf list --archetype instanced\n" +
		"  mctf list --category web --wave 2\n" +
		"  mctf list --json",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		filtered := filterChallenges(challs)

		if flagJSON {
			return printListJSON(filtered)
		}
		printListTable(filtered)
		return nil
	},
}

type listRow struct {
	Category   string   `json:"category"`
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`
	Archetype  string   `json:"archetype"`
	Difficulty string   `json:"difficulty"`
	Wave       int      `json:"wave"`
	Author     string   `json:"author"`
	Tags       []string `json:"tags,omitempty"`
}

func filterChallenges(challs []*challenge.Challenge) []*challenge.Challenge {
	var out []*challenge.Challenge
	for _, c := range challs {
		if c.Manifest.Challenge.Name == "" {
			continue
		}
		if flagListCategory != "" && c.CategoryDir != flagListCategory {
			continue
		}
		if flagListArchetype != "" && string(c.Archetype) != flagListArchetype {
			continue
		}
		if flagListWave != 0 && c.Manifest.Challenge.Wave != flagListWave {
			continue
		}
		if flagListDifficulty != "" && c.Manifest.Challenge.Difficulty != flagListDifficulty {
			continue
		}
		out = append(out, c)
	}
	return out
}

func printListTable(challs []*challenge.Challenge) {
	if len(challs) == 0 {
		fmt.Println("no challenges matched the filters")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CATEGORY\tSLUG\tNAME\tARCHETYPE\tDIFFICULTY\tWAVE\tAUTHOR")
	var nStatic, nService, nInstanced int
	for _, c := range challs {
		switch c.Archetype {
		case challenge.ArchetypeStatic:
			nStatic++
		case challenge.ArchetypeService:
			nService++
		case challenge.ArchetypeInstanced:
			nInstanced++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			c.CategoryDir, c.Slug,
			truncate(c.Manifest.Challenge.Name, 30),
			c.Archetype,
			c.Manifest.Challenge.Difficulty,
			c.Manifest.Challenge.Wave,
			c.Manifest.Challenge.Author,
		)
	}
	tw.Flush()
	fmt.Fprintf(os.Stdout, "\n%d challenges (%d static, %d service, %d instanced)\n",
		len(challs), nStatic, nService, nInstanced)
}

func printListJSON(challs []*challenge.Challenge) error {
	rows := make([]listRow, 0, len(challs))
	for _, c := range challs {
		rows = append(rows, listRow{
			Category:   c.CategoryDir,
			Slug:       c.Slug,
			Name:       c.Manifest.Challenge.Name,
			Archetype:  string(c.Archetype),
			Difficulty: c.Manifest.Challenge.Difficulty,
			Wave:       c.Manifest.Challenge.Wave,
			Author:     c.Manifest.Challenge.Author,
			Tags:       append([]string(nil), c.Manifest.Challenge.Tags...),
		})
	}
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(body))
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func init() {
	listCmd.Flags().StringVar(&flagListCategory, "category", "", "filter by category slug")
	listCmd.Flags().StringVar(&flagListArchetype, "archetype", "", "filter by archetype: static | service | instanced")
	listCmd.Flags().IntVar(&flagListWave, "wave", 0, "filter by wave (0 = no filter)")
	listCmd.Flags().StringVar(&flagListDifficulty, "difficulty", "", "filter by difficulty: easy | medium | hard | extreme")
	rootCmd.AddCommand(listCmd)
}
