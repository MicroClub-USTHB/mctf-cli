package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/monitoring"
)

var (
	flagMonitoringDryRun  bool
	flagMonitoringVolumes bool
)

var monitoringCmd = &cobra.Command{
	Use:   "monitoring",
	Short: "Bring up the Prometheus + Grafana + cAdvisor observability stack",
	Long: `Renders and runs the MCTF monitoring stack under monitoring/stack/.

The stack is Prometheus (scraping cAdvisor and every instanced challenge),
cAdvisor (per-container metrics), and Grafana (auto-provisioned with the
Prometheus datasource and a container-overview dashboard).

Stack files are regenerated on every ` + "`up`" + ` from the current event config and
challenge set, so the scrape targets always match what is deployed. Tune
ports, Grafana password, and retention under the ` + "`monitoring`" + ` block in
mctf.config.yml.`,
}

var monitoringUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Render the stack files and start the stack (docker compose up -d)",
	Long: "Regenerates monitoring/stack/ from the current config and challenges,\n" +
		"then runs `docker compose up -d` in it.\n\n" +
		"Examples:\n" +
		"  mctf monitoring up\n" +
		"  mctf monitoring up --dry-run",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, challs, err := discoverAll()
		if err != nil {
			return err
		}

		stack, err := monitoring.Plan(cfg, flagDir, challs)
		if err != nil {
			return err
		}
		abs, _ := filepath.Abs(flagDir)
		ctx := context.Background()
		runner := monitoringRunner(flagMonitoringDryRun)

		// Dry run: show what would be rendered and run, touch nothing.
		if flagMonitoringDryRun {
			fmt.Printf("would render monitoring stack (%d files):\n", len(stack.Files))
			for _, p := range stack.SortedPaths() {
				fmt.Printf("  render  %s\n", relPath(abs, p))
			}
			return runner.Up(ctx, stack.Dir)
		}

		written, err := stack.Write()
		if err != nil {
			return err
		}
		fmt.Printf("rendered monitoring stack (%d files):\n", len(written))
		for _, p := range written {
			fmt.Printf("  wrote   %s\n", relPath(abs, p))
		}

		if err := runner.Up(ctx, stack.Dir); err != nil {
			notifyDiscord(ctx, "monitoring up failed", "error", err.Error())
			return err
		}

		opts := monitoring.OptionsFromConfig(cfg)
		fmt.Printf("\nmonitoring up\n")
		fmt.Printf("  grafana:    http://localhost:%d (admin / %s)\n", opts.GrafanaPort, opts.GrafanaPassword)
		fmt.Printf("  prometheus: http://localhost:%d\n", opts.PrometheusPort)
		fmt.Printf("  cadvisor:   http://localhost:%d\n", opts.CadvisorPort)
		notifyDiscord(ctx, "monitoring up", "success",
			fmt.Sprintf("prometheus+grafana+cadvisor up (%d challenge scrape target(s))", instancedCount(challs)))
		return nil
	},
}

var monitoringDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop the stack (docker compose down)",
	Long: "Runs `docker compose down` in monitoring/stack/.\n\n" +
		"With --volumes, also drops the Prometheus and Grafana data volumes.\n\n" +
		"Examples:\n" +
		"  mctf monitoring down\n" +
		"  mctf monitoring down --volumes",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := filepath.Join(flagDir, monitoring.StackDir)
		if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); os.IsNotExist(err) {
			fmt.Println("nothing to tear down — no monitoring stack found (run `mctf monitoring up` first)")
			return nil
		}
		runner := monitoringRunner(flagMonitoringDryRun)
		ctx := context.Background()
		if err := runner.Down(ctx, dir, flagMonitoringVolumes); err != nil {
			notifyDiscord(ctx, "monitoring down failed", "error", err.Error())
			return err
		}
		if !flagMonitoringDryRun {
			fmt.Println("monitoring down")
			notifyDiscord(ctx, "monitoring down", "info", "stack stopped")
		}
		return nil
	},
}

// instancedCount counts instanced challenges that produce a scrape target.
func instancedCount(challs []*challenge.Challenge) int {
	n := 0
	for _, c := range challs {
		if c.Archetype == challenge.ArchetypeInstanced &&
			c.Manifest.Deploy != nil && c.Manifest.Deploy.InternalPort != nil {
			n++
		}
	}
	return n
}

func monitoringRunner(dry bool) monitoring.Runner {
	if dry {
		return monitoring.NewDryRunner()
	}
	return monitoring.NewDockerRunner()
}

func init() {
	monitoringUpCmd.Flags().BoolVar(&flagMonitoringDryRun, "dry-run", false, "print the docker compose command instead of running it")
	monitoringDownCmd.Flags().BoolVar(&flagMonitoringDryRun, "dry-run", false, "print the docker compose command instead of running it")
	monitoringDownCmd.Flags().BoolVar(&flagMonitoringVolumes, "volumes", false, "also drop prometheus/grafana data volumes")
	monitoringCmd.AddCommand(monitoringUpCmd)
	monitoringCmd.AddCommand(monitoringDownCmd)
	rootCmd.AddCommand(monitoringCmd)
}
