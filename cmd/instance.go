package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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
		"challenges. The store lives at .mctf/instances.json.\n\n" +
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
	Short: "Tear down a per-team instance (remote instancer when instancer.url is set, else local store)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Remote instancer is the source of truth when configured: the server
		// owns teardown (containers, volumes, routing), so --force/--volumes
		// are local-only and do not apply.
		if url, ok := remoteInstancerURL(); ok {
			return destroyRemoteInstance(cmd.Context(), url, args[0])
		}

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
	Short: "List live instances (remote instancer when instancer.url is set, else local store)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// When the event config points at a remote instancer, that is the
		// source of truth — list from its admin API instead of the local store.
		if url, ok := remoteInstancerURL(); ok {
			return listRemoteInstances(cmd.Context(), url)
		}

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

var instanceFlagCmd = &cobra.Command{
	Use:   "flag <instance-id>",
	Short: "Print the flag for a live instance",
	Long: "Reads the per-team flag captured when the instance was spawned\n" +
		"(`--flag` override or the manifest's static flag). Falls back to\n" +
		"the manifest if the store record predates the flag field.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openStore()
		if err != nil {
			return err
		}
		inst, ok := store.Get(args[0])
		if !ok {
			return fmt.Errorf("no instance with id %q", args[0])
		}
		if inst.Flag != "" {
			fmt.Println(inst.Flag)
			return nil
		}
		// Pre-flag-field record (or never set). Recover from the manifest.
		category, slug, ok := splitCategorySlug(inst.ChallengeSlug)
		if !ok {
			return fmt.Errorf("instance %q has malformed challenge slug %q", inst.ID, inst.ChallengeSlug)
		}
		_, ch, err := loadInstanced(category, slug)
		if err != nil {
			return err
		}
		if len(ch.Manifest.Challenge.Flags) == 0 {
			return fmt.Errorf("%s has no flag in its manifest", inst.ChallengeSlug)
		}
		fmt.Println(ch.Manifest.Challenge.Flags[0].Flag)
		return nil
	},
}

// containerState is one row of `docker ps`-style state for a container
// referenced by the store. Status is left empty when the container is
// no longer on the daemon (e.g. destroyed out-of-band).
type containerState struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
	Image  string `json:"image,omitempty"`
	Found  bool   `json:"found"`
}

// inspectContainersFn shells out to `docker ps -a` to fetch live state.
// Overridable in tests so we don't need a real docker daemon.
var inspectContainersFn = inspectContainersViaDocker

func inspectContainersViaDocker(ctx context.Context, ids []string) ([]containerState, error) {
	out := make([]containerState, 0, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := []string{"ps", "-a", "--no-trunc",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Image}}",
	}
	for _, id := range ids {
		args = append(args, "--filter", "id="+id)
	}
	raw, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	found := map[string]containerState{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}
		found[parts[0]] = containerState{
			ID: parts[0], Name: parts[1], Status: parts[2], Image: parts[3], Found: true,
		}
	}
	// Preserve store order; mark unknown containers as not-found.
	for _, id := range ids {
		if cs, ok := found[id]; ok {
			out = append(out, cs)
			continue
		}
		out = append(out, containerState{ID: id, Status: "(gone)"})
	}
	return out, nil
}

var instancePsCmd = &cobra.Command{
	Use:   "ps <instance-id>",
	Short: "Show live docker state for an instance",
	Long: "Looks up the instance in the store and queries `docker ps` for\n" +
		"each container in it. Reports up-time / health status, image, and\n" +
		"the host-port mapping captured at spawn.\n\n" +
		"The flag is intentionally not printed — use `mctf instance flag <id>`.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openStore()
		if err != nil {
			return err
		}
		inst, ok := store.Get(args[0])
		if !ok {
			return fmt.Errorf("no instance with id %q", args[0])
		}
		states, err := inspectContainersFn(cmd.Context(), inst.Containers)
		if err != nil {
			return err
		}
		if flagJSON {
			payload := struct {
				Instance   instancer.Instance `json:"instance"`
				Containers []containerState   `json:"containers"`
			}{Instance: scrubFlag(inst), Containers: states}
			body, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		}
		return printInstancePs(os.Stdout, inst, states)
	},
}

// scrubFlag returns a copy of the instance with the Flag field cleared.
// `ps` is meant for operational use (status / health); the flag has its
// own dedicated subcommand.
func scrubFlag(i instancer.Instance) instancer.Instance {
	i.Flag = ""
	return i
}

func printInstancePs(w io.Writer, inst instancer.Instance, states []containerState) error {
	now := time.Now().UTC()
	spawnedRel := now.Sub(inst.SpawnedAt).Round(time.Second).String()
	var expiresRel string
	switch {
	case inst.ExpiresAt.IsZero():
		expiresRel = "(none)"
	case inst.ExpiresAt.Before(now):
		expiresRel = "expired " + now.Sub(inst.ExpiresAt).Round(time.Second).String() + " ago"
	default:
		expiresRel = "in " + inst.ExpiresAt.Sub(now).Round(time.Second).String()
	}

	fmt.Fprintf(w, "id:        %s\n", inst.ID)
	fmt.Fprintf(w, "challenge: %s\n", inst.ChallengeSlug)
	fmt.Fprintf(w, "team:      %s\n", inst.TeamID)
	fmt.Fprintf(w, "spawned:   %s (%s ago)\n", inst.SpawnedAt.Format(time.RFC3339), spawnedRel)
	fmt.Fprintf(w, "expires:   %s (%s)\n", inst.ExpiresAt.Format(time.RFC3339), expiresRel)
	if len(inst.Ports) > 0 {
		fmt.Fprintf(w, "ports:     %s\n", portsString(inst.Ports))
	}

	if len(states) == 0 {
		fmt.Fprintln(w, "\nno containers recorded on this instance")
		return nil
	}
	fmt.Fprintln(w, "\ncontainers:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  CONTAINER ID\tNAME\tSTATUS\tIMAGE")
	for _, s := range states {
		id := s.ID
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", id, dashIfEmpty(s.Name), dashIfEmpty(s.Status), dashIfEmpty(s.Image))
	}
	return tw.Flush()
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
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
	return instancer.NewStore(filepath.Join(abs, ".mctf", "instances.json"))
}

// remoteInstancerURL returns the configured instancer API URL when the event
// config sets instancer.url. A config-load failure falls back to the local
// store (returns "", false), preserving the offline/local workflow.
func remoteInstancerURL() (string, bool) {
	cfg, err := config.Load(flagDir)
	if err != nil {
		return "", false
	}
	if cfg.Instancer != nil {
		if u := strings.TrimSpace(cfg.Instancer.URL); u != "" {
			return u, true
		}
	}
	return "", false
}

// destroyRemoteInstance force-stops one instance via the remote instancer's
// admin API. The server owns teardown; the local --force/--volumes flags do
// not apply to this path.
func destroyRemoteInstance(ctx context.Context, baseURL, id string) error {
	client := instancer.NewAPIClient(baseURL, os.Getenv("MCTF_INSTANCER_ADMIN_KEY"))
	msg, err := client.Destroy(ctx, id)
	if err != nil {
		return err
	}
	if msg == "" {
		msg = "destroyed"
	}
	fmt.Printf("%s: %s\n", id, msg)
	return nil
}

// listRemoteInstances lists instances from the remote instancer's admin API.
// The admin key is read from MCTF_INSTANCER_ADMIN_KEY (a secret, never config).
func listRemoteInstances(ctx context.Context, baseURL string) error {
	client := instancer.NewAPIClient(baseURL, os.Getenv("MCTF_INSTANCER_ADMIN_KEY"))
	instances, err := client.List(ctx)
	if err != nil {
		return err
	}
	if flagJSON {
		body, err := json.MarshalIndent(instances, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(body))
		return nil
	}
	fmt.Printf("instancer: %s\n\n", baseURL)
	if len(instances) == 0 {
		fmt.Println("no instances")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tCHALLENGE\tTEAM\tSTATUS\tPORTS\tEXPIRES")
	for _, i := range instances {
		team := i.TeamName
		if team == "" {
			team = i.TeamID
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			i.InstanceID, i.ChallengeID, team, i.Status, remotePortsString(i.Ports), i.ExpiresAt)
	}
	tw.Flush()
	fmt.Printf("\n%d instance(s)\n", len(instances))
	return nil
}

// remotePortsString renders an internal:external port map (string JSON keys)
// as "80->10234, 443->10235", sorted by internal port.
func remotePortsString(ports map[string]int) string {
	if len(ports) == 0 {
		return "-"
	}
	internals := make([]int, 0, len(ports))
	ext := make(map[int]int, len(ports))
	for k, v := range ports {
		n, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		internals = append(internals, n)
		ext[n] = v
	}
	sort.Ints(internals)
	parts := make([]string, 0, len(internals))
	for _, in := range internals {
		parts = append(parts, fmt.Sprintf("%d->%d", in, ext[in]))
	}
	return strings.Join(parts, ", ")
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
	return nil, nil, fmt.Errorf("no challenge at %s/%s%s", category, slug, didYouMean(challs, category+"/"+slug))
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
	instanceCmd.AddCommand(instanceFlagCmd)
	instanceCmd.AddCommand(instancePsCmd)

	rootCmd.AddCommand(instanceCmd)
}
