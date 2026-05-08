package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/instancer"
)

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Manage per-team challenge instances (native instancer)",
	Long: "Spawn / destroy / list per-team instances of dynamic-instance\n" +
		"challenges. The store lives at .mtcf/instances.json.\n\n" +
		"For v0 the subcommands shell out directly to `docker compose`;\n" +
		"a daemon mode that exposes these as HTTP endpoints lands later.",
}

var (
	flagInstSpawnTeam  string
	flagInstSpawnFlag  string
	flagInstSpawnRange string
)

var instanceSpawnCmd = &cobra.Command{
	Use:   "spawn <category>/<slug>",
	Short: "Spawn a per-team instance of an instanced challenge",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		category, slug, ok := splitCategorySlug(args[0])
		if !ok {
			return fmt.Errorf("argument must be <category>/<slug>, got %q", args[0])
		}
		if flagInstSpawnTeam == "" {
			return fmt.Errorf("--team is required")
		}

		cfg, ch, err := loadInstanced(category, slug)
		if err != nil {
			return err
		}

		store, err := openStore()
		if err != nil {
			return err
		}

		// If this team already has a live instance, return it.
		if existing, ok := store.ByTeamAndChallenge(flagInstSpawnTeam, category+"/"+slug); ok {
			fmt.Printf("instance already exists for team %q on %s/%s\n", flagInstSpawnTeam, category, slug)
			return printInstance(existing)
		}

		opts := instancer.SpawnOptions{FlagOverride: flagInstSpawnFlag}
		if flagInstSpawnRange != "" {
			lo, hi, err := parsePortRange(flagInstSpawnRange)
			if err != nil {
				return err
			}
			opts.HostPortStart, opts.HostPortEnd = lo, hi
		}

		spec, err := instancer.PlanSpawn(cfg, ch, flagInstSpawnTeam, store.AllocatedHostPorts(), opts)
		if err != nil {
			return err
		}

		fmt.Printf("spawning %s/%s for team %q\n", category, slug, flagInstSpawnTeam)
		fmt.Printf("  id:    %s\n", spec.ID)
		for internal, host := range spec.Ports {
			fmt.Printf("  port:  container %d -> host %d\n", internal, host)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		runner := instancer.NewDockerRunner()
		containers, err := runner.Up(ctx, *spec)
		if err != nil {
			return err
		}

		inst := instancer.Instance{
			ID:            spec.ID,
			ChallengeSlug: spec.ChallengeSlug,
			TeamID:        spec.TeamID,
			ProjectName:   spec.ID,
			Containers:    containers,
			Ports:         spec.Ports,
			Flag:          spec.Env["FLAG"],
			SpawnedAt:     time.Now().UTC(),
			ExpiresAt:     spec.ExpiresAt,
		}
		if err := store.Put(inst); err != nil {
			return fmt.Errorf("persist instance: %w", err)
		}
		fmt.Printf("\nspawn complete (expires %s)\n", inst.ExpiresAt.Format(time.RFC3339))
		return nil
	},
}

var (
	flagInstDestroyForce   bool
	flagInstDestroyVolumes bool
)

var instanceDestroyCmd = &cobra.Command{
	Use:   "destroy <instance-id>",
	Short: "Tear down a per-team instance and remove it from the store",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openStore()
		if err != nil {
			return err
		}
		inst, ok := store.Get(args[0])
		if !ok {
			if !flagInstDestroyForce {
				return fmt.Errorf("no instance with id %q (use --force to remove from store anyway)", args[0])
			}
			return store.Delete(args[0])
		}

		// Reconstruct the compose file path from the store record. The
		// challenge dir is derivable from the slug; we re-discover so
		// we get the absolute path.
		composeFile, _ := composeFileForSlug(inst.ChallengeSlug)
		spec := instancer.SpawnSpec{ID: inst.ID, ComposeFile: composeFile}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		runner := instancer.NewDockerRunner()
		if err := runner.Down(ctx, spec, flagInstDestroyVolumes); err != nil {
			fmt.Fprintf(os.Stderr, "warning: docker compose down: %v\n", err)
			if !flagInstDestroyForce {
				return err
			}
		}
		if err := store.Delete(inst.ID); err != nil {
			return err
		}
		fmt.Printf("destroyed %s\n", inst.ID)
		return nil
	},
}

var instanceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List live instances from the store",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openStore()
		if err != nil {
			return err
		}
		instances := store.List()
		if flagJSON {
			body, err := json.MarshalIndent(instances, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		}
		if len(instances) == 0 {
			fmt.Println("no instances")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tCHALLENGE\tTEAM\tPORTS\tEXPIRES")
		now := time.Now().UTC()
		for _, i := range instances {
			ports := portsString(i.Ports)
			expires := i.ExpiresAt.Sub(now).Round(time.Second).String()
			if i.ExpiresAt.Before(now) {
				expires = "expired"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", i.ID, i.ChallengeSlug, i.TeamID, ports, expires)
		}
		tw.Flush()
		fmt.Printf("\n%d instance(s)\n", len(instances))
		return nil
	},
}

var flagInstExtendBy time.Duration

var instanceExtendCmd = &cobra.Command{
	Use:   "extend <instance-id>",
	Short: "Bump an instance's expiry by --by (default 15m)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openStore()
		if err != nil {
			return err
		}
		inst, ok := store.Get(args[0])
		if !ok {
			return fmt.Errorf("no instance with id %q", args[0])
		}
		now := time.Now().UTC()
		if inst.ExpiresAt.Before(now) {
			inst.ExpiresAt = now.Add(flagInstExtendBy)
		} else {
			inst.ExpiresAt = inst.ExpiresAt.Add(flagInstExtendBy)
		}
		inst.LastExtended = now
		if err := store.Put(inst); err != nil {
			return err
		}
		fmt.Printf("extended %s — now expires %s\n", inst.ID, inst.ExpiresAt.Format(time.RFC3339))
		return nil
	},
}

var instanceReapCmd = &cobra.Command{
	Use:   "reap",
	Short: "Destroy expired instances (manual cleanup; daemon does this automatically once it lands)",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openStore()
		if err != nil {
			return err
		}
		expired := store.Expired(time.Now().UTC())
		if len(expired) == 0 {
			fmt.Println("no expired instances")
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		runner := instancer.NewDockerRunner()
		var failed int
		for _, inst := range expired {
			composeFile, _ := composeFileForSlug(inst.ChallengeSlug)
			spec := instancer.SpawnSpec{ID: inst.ID, ComposeFile: composeFile}
			if err := runner.Down(ctx, spec, false); err != nil {
				fmt.Fprintf(os.Stderr, "warning: %s: %v\n", inst.ID, err)
				failed++
			}
			if err := store.Delete(inst.ID); err != nil {
				return err
			}
			fmt.Printf("reaped %s\n", inst.ID)
		}
		fmt.Printf("\n%d reaped, %d errors\n", len(expired)-failed, failed)
		if failed > 0 {
			return fmt.Errorf("%d instance(s) failed to tear down cleanly", failed)
		}
		return nil
	},
}

// --- helpers ---

func openStore() (*instancer.Store, error) {
	abs, err := filepath.Abs(flagDir)
	if err != nil {
		return nil, err
	}
	return instancer.NewStore(filepath.Join(abs, ".mtcf", "instances.json"))
}

func loadInstanced(category, slug string) (*config.Config, *challenge.Challenge, error) {
	cfg, err := config.Load(flagDir)
	if err != nil {
		return nil, nil, err
	}
	challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range challs {
		if c.CategoryDir == category && c.Slug == slug {
			if c.Archetype != challenge.ArchetypeInstanced {
				return nil, nil, fmt.Errorf("%s/%s is not an instanced challenge (archetype=%s)", category, slug, c.Archetype)
			}
			return cfg, c, nil
		}
	}
	return nil, nil, fmt.Errorf("no challenge at %s/%s", category, slug)
}

func composeFileForSlug(slug string) (string, error) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("malformed slug %q", slug)
	}
	cfg, err := config.Load(flagDir)
	if err != nil {
		return "", err
	}
	abs, _ := filepath.Abs(flagDir)
	root := cfg.ChallengesDir
	if !filepath.IsAbs(root) {
		root = filepath.Join(abs, root)
	}
	return filepath.Join(root, parts[0], parts[1], "challenge", "docker-compose.yml"), nil
}

func parsePortRange(s string) (int, int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("port range must be lo-hi (got %q)", s)
	}
	var lo, hi int
	if _, err := fmt.Sscanf(parts[0], "%d", &lo); err != nil {
		return 0, 0, err
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &hi); err != nil {
		return 0, 0, err
	}
	if lo >= hi {
		return 0, 0, fmt.Errorf("port range invalid: %d-%d", lo, hi)
	}
	return lo, hi, nil
}

func portsString(p map[int]int) string {
	if len(p) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(p))
	for in, host := range p {
		parts = append(parts, fmt.Sprintf("%d->%d", in, host))
	}
	return strings.Join(parts, ",")
}

func printInstance(i instancer.Instance) error {
	body, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(body))
	return nil
}

func init() {
	instanceSpawnCmd.Flags().StringVar(&flagInstSpawnTeam, "team", "", "team identifier (required)")
	instanceSpawnCmd.Flags().StringVar(&flagInstSpawnFlag, "flag", "", "override the manifest flag with a per-team value")
	instanceSpawnCmd.Flags().StringVar(&flagInstSpawnRange, "host-port-range", "", "host port range, e.g. 30000-32999 (default)")
	instanceCmd.AddCommand(instanceSpawnCmd)

	instanceDestroyCmd.Flags().BoolVar(&flagInstDestroyForce, "force", false, "delete from store even if docker compose down fails")
	instanceDestroyCmd.Flags().BoolVar(&flagInstDestroyVolumes, "volumes", false, "also drop named volumes")
	instanceCmd.AddCommand(instanceDestroyCmd)

	instanceCmd.AddCommand(instanceListCmd)

	instanceExtendCmd.Flags().DurationVar(&flagInstExtendBy, "by", 15*time.Minute, "duration to add to ExpiresAt")
	instanceCmd.AddCommand(instanceExtendCmd)

	instanceCmd.AddCommand(instanceReapCmd)

	rootCmd.AddCommand(instanceCmd)
}
