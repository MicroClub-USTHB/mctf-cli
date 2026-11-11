package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/infra"
)

var (
	flagInfraDryRun      bool
	flagInfraAutoApprove bool
)

var infraCmd = &cobra.Command{
	Use:   "infra",
	Short: "Provision the event server with Terraform (plan, apply, destroy)",
	Long: `Wraps Terraform to provision the host an event runs on: a single VPS on
Hetzner Cloud or DigitalOcean, with a firewall opening ssh, http/https and
every active category's challenge port range, and docker preinstalled so
it is ready for deploy.mode: ssh.

The configuration is rendered from the ` + "`infra`" + ` block of mctf.config.yml into
.mctf/infra/, which also holds Terraform's local state. Keep that
directory: it is the only record of what was provisioned.

Terraform must be on PATH (or set ` + infra.BinEnv + `, e.g. to tofu). The
provider API token is read from HCLOUD_TOKEN (hetzner) or
DIGITALOCEAN_TOKEN (digitalocean). See docs/spec/infra.md.`,
}

var infraPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Render the Terraform configuration and show the plan",
	Long: "Renders .mctf/infra/ from the current config, then runs\n" +
		"`terraform init` and `terraform plan` in it.\n\n" +
		"With --dry-run the configuration is still rendered (so it can be\n" +
		"reviewed), but Terraform is not run and no token is needed.\n\n" +
		"Examples:\n" +
		"  mctf infra plan\n" +
		"  mctf infra plan --dry-run",
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, runner, err := prepareInfra()
		if err != nil {
			return err
		}
		return runner.Plan(context.Background(), ws.Dir)
	},
}

var infraApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Render the Terraform configuration and provision it",
	Long: "Renders .mctf/infra/ from the current config, then runs\n" +
		"`terraform init` and `terraform apply` in it. Terraform asks for\n" +
		"confirmation unless --auto-approve is set.\n\n" +
		"Examples:\n" +
		"  mctf infra apply\n" +
		"  mctf infra apply --auto-approve",
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, runner, err := prepareInfra()
		if err != nil {
			return err
		}
		ctx := context.Background()
		if err := runner.Apply(ctx, ws.Dir, flagInfraAutoApprove); err != nil {
			notifyDiscord(ctx, "infra apply failed", "error", err.Error())
			return err
		}
		if flagInfraDryRun {
			return nil
		}
		abs, _ := filepath.Abs(flagDir)
		fmt.Printf("\ninfra applied\n")
		fmt.Printf("  server address: %s -chdir=%s output ipv4\n", infra.Bin(), relPath(abs, ws.Dir))
		fmt.Printf("  next: point deploy.ssh.host and the event DNS records at it\n")
		notifyDiscord(ctx, "infra apply", "success", "event server provisioned")
		return nil
	},
}

var infraDestroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy everything provisioned by infra apply",
	Long: "Runs `terraform init` and `terraform destroy` against the\n" +
		"configuration and state already in .mctf/infra/. The configuration\n" +
		"is not re-rendered, so destroy tears down exactly what was applied\n" +
		"even if mctf.config.yml changed since. Terraform asks for\n" +
		"confirmation unless --auto-approve is set.\n\n" +
		"Examples:\n" +
		"  mctf infra destroy\n" +
		"  mctf infra destroy --auto-approve",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := filepath.Join(flagDir, infra.WorkDir)
		if !infra.HasState(dir) {
			fmt.Println("nothing to destroy — no Terraform state in .mctf/infra (run `mctf infra apply` first)")
			return nil
		}
		// Best-effort token check: destroy must still work when the
		// config no longer loads or no longer has an infra block.
		if cfg, err := config.Load(flagDir); err == nil && cfg.Infra != nil && !flagInfraDryRun {
			if err := requireInfraToken(cfg); err != nil {
				return err
			}
		}
		runner, err := infraRunner(flagInfraDryRun)
		if err != nil {
			return err
		}
		ctx := context.Background()
		if err := runner.Destroy(ctx, dir, flagInfraAutoApprove); err != nil {
			notifyDiscord(ctx, "infra destroy failed", "error", err.Error())
			return err
		}
		if !flagInfraDryRun {
			fmt.Println("infra destroyed")
			notifyDiscord(ctx, "infra destroy", "info", "event server destroyed")
		}
		return nil
	},
}

// prepareInfra loads the config, renders .mctf/infra/ to disk (also on
// --dry-run, so the configuration can be reviewed), and picks a runner.
func prepareInfra() (*infra.Workspace, infra.Runner, error) {
	cfg, err := config.Load(flagDir)
	if err != nil {
		return nil, nil, err
	}
	ws, err := infra.Plan(cfg, flagDir)
	if err != nil {
		return nil, nil, err
	}
	if !flagInfraDryRun {
		if err := requireInfraToken(cfg); err != nil {
			return nil, nil, err
		}
	}
	runner, err := infraRunner(flagInfraDryRun)
	if err != nil {
		return nil, nil, err
	}

	written, err := ws.Write()
	if err != nil {
		return nil, nil, err
	}
	abs, _ := filepath.Abs(flagDir)
	fmt.Printf("rendered terraform configuration (%d files):\n", len(written))
	for _, p := range written {
		fmt.Printf("  wrote   %s\n", relPath(abs, p))
	}
	return ws, runner, nil
}

// requireInfraToken fails early, before Terraform starts, when the
// provider's API token is not in the environment.
func requireInfraToken(cfg *config.Config) error {
	p, ok := infra.Providers[cfg.Infra.Provider]
	if !ok {
		return nil // OptionsFromConfig reports the unsupported provider
	}
	if os.Getenv(p.TokenEnv) == "" {
		return fmt.Errorf("%s is not set — export the %s API token, or use --dry-run to only render the configuration", p.TokenEnv, p.Name)
	}
	return nil
}

func infraRunner(dry bool) (infra.Runner, error) {
	if dry {
		return infra.NewDryRunner(), nil
	}
	return infra.NewTerraformRunner()
}

func init() {
	for _, c := range []*cobra.Command{infraPlanCmd, infraApplyCmd, infraDestroyCmd} {
		c.Flags().BoolVar(&flagInfraDryRun, "dry-run", false, "print the terraform commands instead of running them (plan/apply still render the configuration)")
		infraCmd.AddCommand(c)
	}
	infraApplyCmd.Flags().BoolVar(&flagInfraAutoApprove, "auto-approve", false, "skip Terraform's interactive confirmation")
	infraDestroyCmd.Flags().BoolVar(&flagInfraAutoApprove, "auto-approve", false, "skip Terraform's interactive confirmation")
	rootCmd.AddCommand(infraCmd)
}
