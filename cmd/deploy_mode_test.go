package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/kubernetes"
)

func cfgWithMode(mode string) *config.Config {
	return &config.Config{Deploy: &config.Deploy{Mode: mode}}
}

func TestDeployMode(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		flag    string
		want    string
		wantErr string
	}{
		{"nil config defaults to docker", nil, "", "docker", ""},
		{"nil deploy block defaults to docker", &config.Config{}, "", "docker", ""},
		{"config mode used when no flag", cfgWithMode("ssh"), "", "ssh", ""},
		{"flag kubernetes overrides docker config", cfgWithMode("docker"), "kubernetes", "kubernetes", ""},
		{"flag kubernetes with nil config", nil, "kubernetes", "kubernetes", ""},
		{"flag restating config is fine", cfgWithMode("ssh"), "ssh", "ssh", ""},
		{"invalid flag", cfgWithMode("docker"), "foo", "", "invalid --mode"},
		{"flag docker conflicts with ssh config", cfgWithMode("ssh"), "docker", "", "conflicts"},
		{"flag ssh conflicts with docker config", cfgWithMode("docker"), "ssh", "", "conflicts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deployMode(tt.cfg, tt.flag)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("deployMode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAcquireDeployLock(t *testing.T) {
	dir := t.TempDir()

	release, err := acquireDeployLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	lockFile := filepath.Join(dir, ".mctf", "deploy.lock")
	if _, err := os.Stat(lockFile); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}

	if _, err := acquireDeployLock(dir); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("second acquire err = %v, want 'already in progress'", err)
	}

	release()
	if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed, stat err = %v", err)
	}

	release2, err := acquireDeployLock(dir)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	release2()
}

func TestApplyKube_DoesNotMutateConfig(t *testing.T) {
	cfg := &config.Config{
		Event:  config.Event{Slug: "evt"},
		Deploy: &config.Deploy{Mode: "docker"},
	}
	// No challenges: Plan succeeds with an empty plan.
	if err := applyKube(context.Background(), cfg, nil, "up", true, "", ""); err != nil {
		t.Fatalf("applyKube: %v", err)
	}
	if cfg.Deploy.Mode != "docker" {
		t.Errorf("config mutated: Deploy.Mode = %q, want docker", cfg.Deploy.Mode)
	}

	nilDeploy := &config.Config{Event: config.Event{Slug: "evt"}}
	if err := applyKube(context.Background(), nilDeploy, nil, "up", true, "", ""); err != nil {
		t.Fatalf("applyKube nil deploy: %v", err)
	}
	if nilDeploy.Deploy != nil {
		t.Errorf("config mutated: Deploy = %+v, want nil", nilDeploy.Deploy)
	}
}

func TestKubeDeployHookPayload_MapsAllUnitFields(t *testing.T) {
	cfg := &config.Config{
		Event: config.Event{Slug: "test-event"},
	}

	plan := []kubernetes.KubeUnit{
		{
			Challenge: "web/my-challenge",
			Category:  "web",
			Slug:      "my-challenge",
			Name:      "mctf-test-web-my-challenge",
			Image:     "registry.example.com/my-challenge:v1",
		},
	}

	payload := kubeDeployHookPayload(
		cfg,
		"after",
		false,
		plan,
		"success",
		nil,
	)

	if payload.Event != "test-event" {
		t.Errorf("Event = %q, want %q", payload.Event, "test-event")
	}
	if payload.Phase != "after" {
		t.Errorf("Phase = %q, want %q", payload.Phase, "after")
	}
	if payload.Mode != "kubernetes" {
		t.Errorf("Mode = %q, want %q", payload.Mode, "kubernetes")
	}
	if len(payload.Units) != 1 {
		t.Fatalf("got %d units, want 1", len(payload.Units))
	}

	unit := payload.Units[0]

	if unit.Challenge != "web/my-challenge" {
		t.Errorf("Challenge = %q, want %q", unit.Challenge, "web/my-challenge")
	}
	if unit.Category != "web" {
		t.Errorf("Category = %q, want %q", unit.Category, "web")
	}
	if unit.Slug != "my-challenge" {
		t.Errorf("Slug = %q, want %q", unit.Slug, "my-challenge")
	}
	if unit.ProjectName != "mctf-test-web-my-challenge" {
		t.Errorf("ProjectName = %q, want %q", unit.ProjectName, "mctf-test-web-my-challenge")
	}
	if unit.ImageTag != "registry.example.com/my-challenge:v1" {
		t.Errorf("ImageTag = %q, want %q", unit.ImageTag, "registry.example.com/my-challenge:v1")
	}
}
