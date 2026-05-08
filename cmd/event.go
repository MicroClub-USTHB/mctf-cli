package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

var eventCmd = &cobra.Command{
	Use:   "event",
	Short: "Print an event-wide summary (name, domain, challenge breakdown)",
	Long: "One-screen view of the event repo:\n\n" +
		"  • event name / slug / domain / profile\n" +
		"  • challenge counts by category, archetype, difficulty, wave\n" +
		"  • total assets size on disk\n\n" +
		"With --json, emits the same data as a single object suitable for\n" +
		"dashboards.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		summary := buildEventSummary(cfg, challs)
		if flagJSON {
			body, err := json.MarshalIndent(summary, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		}
		printEventSummary(summary)
		return nil
	},
}

type eventSummary struct {
	Name            string         `json:"name"`
	Slug            string         `json:"slug"`
	Domain          string         `json:"domain,omitempty"`
	Profile         string         `json:"profile"`
	FlagPrefix      string         `json:"flag_prefix"`
	Waves           int            `json:"waves"`
	CurrentWave     int            `json:"current_wave,omitempty"`
	TotalChallenges int            `json:"total_challenges"`
	ByCategory      map[string]int `json:"by_category"`
	ByArchetype     map[string]int `json:"by_archetype"`
	ByDifficulty    map[string]int `json:"by_difficulty"`
	ByWave          map[int]int    `json:"by_wave"`
	AssetsBytes     int64          `json:"assets_bytes"`
	Authors         []string       `json:"authors"`
}

func buildEventSummary(cfg *config.Config, challs []*challenge.Challenge) eventSummary {
	s := eventSummary{
		Name:         cfg.Event.Name,
		Slug:         cfg.Event.Slug,
		Domain:       cfg.Event.Domain,
		Profile:      string(cfg.Profile),
		FlagPrefix:   cfg.Event.FlagPrefix,
		Waves:        cfg.Event.WavesNumber,
		ByCategory:   map[string]int{},
		ByArchetype:  map[string]int{},
		ByDifficulty: map[string]int{},
		ByWave:       map[int]int{},
	}
	if cfg.Event.CurrentWave != nil {
		s.CurrentWave = *cfg.Event.CurrentWave
	}

	authorSet := map[string]struct{}{}
	for _, c := range challs {
		s.TotalChallenges++
		s.ByCategory[c.CategoryDir]++
		s.ByArchetype[string(c.Archetype)]++
		if d := c.Manifest.Challenge.Difficulty; d != "" {
			s.ByDifficulty[d]++
		}
		if w := c.Manifest.Challenge.Wave; w > 0 {
			s.ByWave[w]++
		}
		if a := c.Manifest.Challenge.Author; a != "" {
			authorSet[a] = struct{}{}
		}
		s.AssetsBytes += dirSize(c.AssetsDir)
	}
	s.Authors = make([]string, 0, len(authorSet))
	for a := range authorSet {
		s.Authors = append(s.Authors, a)
	}
	sort.Strings(s.Authors)
	return s
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		total += fi.Size()
		return nil
	})
	return total
}

func printEventSummary(s eventSummary) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "name\t%s\n", s.Name)
	fmt.Fprintf(tw, "slug\t%s\n", s.Slug)
	if s.Domain != "" {
		fmt.Fprintf(tw, "domain\t%s\n", s.Domain)
	}
	fmt.Fprintf(tw, "profile\t%s\n", s.Profile)
	fmt.Fprintf(tw, "flag prefix\t%s\n", s.FlagPrefix)
	if s.Waves > 0 {
		wave := fmt.Sprintf("%d total", s.Waves)
		if s.CurrentWave > 0 {
			wave += fmt.Sprintf(" (current: %d)", s.CurrentWave)
		}
		fmt.Fprintf(tw, "waves\t%s\n", wave)
	}
	fmt.Fprintf(tw, "challenges\t%d\n", s.TotalChallenges)
	if size := s.AssetsBytes; size > 0 {
		fmt.Fprintf(tw, "assets\t%s\n", humanBytes(size))
	}
	tw.Flush()

	if len(s.Authors) > 0 {
		fmt.Printf("\nauthors (%d):\n", len(s.Authors))
		for _, a := range s.Authors {
			fmt.Printf("  %s\n", a)
		}
	}
	if len(s.ByCategory) > 0 {
		fmt.Println("\nby category:")
		printCounts(s.ByCategory)
	}
	if len(s.ByArchetype) > 0 {
		fmt.Println("\nby archetype:")
		printCounts(s.ByArchetype)
	}
	if len(s.ByDifficulty) > 0 {
		fmt.Println("\nby difficulty:")
		printCounts(s.ByDifficulty)
	}
	if len(s.ByWave) > 0 {
		fmt.Println("\nby wave:")
		ws := make([]int, 0, len(s.ByWave))
		for w := range s.ByWave {
			ws = append(ws, w)
		}
		sort.Ints(ws)
		for _, w := range ws {
			fmt.Printf("  wave %d: %d\n", w, s.ByWave[w])
		}
	}
}

func printCounts(m map[string]int) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-12s %d\n", k, m[k])
	}
}

func humanBytes(n int64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GiB", float64(n)/float64(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(k))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func init() {
	rootCmd.AddCommand(eventCmd)
}
