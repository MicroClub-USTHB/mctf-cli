package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/infra"
)

func TestInfraCommandsRegistered(t *testing.T) {
	for _, sub := range []string{"plan", "apply", "destroy"} {
		cmd, _, err := rootCmd.Find([]string{"infra", sub})
		if err != nil {
			t.Fatalf("infra %s not registered: %v", sub, err)
		}
		if cmd.Name() != sub {
			t.Errorf("resolved command = %q, want %q", cmd.Name(), sub)
		}
	}
}

func TestRequireInfraToken(t *testing.T) {
	t.Run("hetzner missing token", func(t *testing.T) {
		t.Setenv("HCLOUD_TOKEN", "")
		cfg := &config.Config{Infra: &config.Infra{Provider: "hetzner"}}
		err := requireInfraToken(cfg)
		if err == nil || !strings.Contains(err.Error(), "HCLOUD_TOKEN is not set") {
			t.Errorf("err = %v, want HCLOUD_TOKEN is not set", err)
		}
	})

	t.Run("hetzner token present", func(t *testing.T) {
		t.Setenv("HCLOUD_TOKEN", "test-token")
		cfg := &config.Config{Infra: &config.Infra{Provider: "hetzner"}}
		if err := requireInfraToken(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("digitalocean missing token", func(t *testing.T) {
		t.Setenv("DIGITALOCEAN_TOKEN", "")
		cfg := &config.Config{Infra: &config.Infra{Provider: "digitalocean"}}
		err := requireInfraToken(cfg)
		if err == nil || !strings.Contains(err.Error(), "DIGITALOCEAN_TOKEN is not set") {
			t.Errorf("err = %v, want DIGITALOCEAN_TOKEN is not set", err)
		}
	})

	t.Run("digitalocean token present", func(t *testing.T) {
		t.Setenv("DIGITALOCEAN_TOKEN", "test-token")
		cfg := &config.Config{Infra: &config.Infra{Provider: "digitalocean"}}
		if err := requireInfraToken(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("unsupported provider no-ops", func(t *testing.T) {
		cfg := &config.Config{Infra: &config.Infra{Provider: "unsupported"}}
		if err := requireInfraToken(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestInfraRunner_DryRun(t *testing.T) {
	runner, err := infraRunner(true)
	if err != nil {
		t.Fatalf("infraRunner(true) err = %v", err)
	}
	if _, ok := runner.(*infra.DryRunner); !ok {
		t.Errorf("expected *infra.DryRunner, got %T", runner)
	}
}

func TestInfraDestroy_NoState(t *testing.T) {
	dir := t.TempDir()
	flagDir = dir
	defer func() { flagDir = "." }()

	// infra destroy without state should return nil and report nothing to destroy
	if err := infraDestroyCmd.RunE(infraDestroyCmd, nil); err != nil {
		t.Errorf("infra destroy with no state returned err = %v", err)
	}
}

func TestPrepareInfra_DryRunRendersConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mctf.config.yml")
	content := `version: 1
event:
  name: "Sample Event"
  slug: "sample-event"
infra:
  provider: hetzner
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	flagDir = dir
	flagInfraDryRun = true
	defer func() {
		flagDir = "."
		flagInfraDryRun = false
	}()

	t.Setenv("HCLOUD_TOKEN", "") // No token needed for dry-run

	ws, runner, err := prepareInfra()
	if err != nil {
		t.Fatalf("prepareInfra() err = %v", err)
	}
	if _, ok := runner.(*infra.DryRunner); !ok {
		t.Errorf("expected DryRunner, got %T", runner)
	}

	mainTF := filepath.Join(ws.Dir, "main.tf")
	if _, err := os.Stat(mainTF); err != nil {
		t.Errorf("expected main.tf to be rendered on dry-run: %v", err)
	}
}
