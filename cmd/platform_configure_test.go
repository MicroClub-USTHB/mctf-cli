package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformConfigure_StartAfterEndValidation(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte(`
version: 1
event:
  name: "Bad Dates"
  slug: "bad-dates"
  start_at: "2026-10-18T10:00:00Z"
  end_at: "2026-10-15T10:00:00Z"
platform:
  type: ctfd
  url: "https://ctfd.example.com"
`)
	if err := os.WriteFile(filepath.Join(dir, "mctf.config.yml"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MCTF_CTFD_TOKEN", "mock-token")
	flagDir = dir
	flagConfigureDryRun = true
	defer func() {
		flagDir = ""
		flagConfigureDryRun = false
	}()

	err := platformConfigureCmd.RunE(platformConfigureCmd, nil)
	if err == nil {
		t.Fatal("expected error when start_at is after end_at")
	}
	if !strings.Contains(err.Error(), "must be before") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPlatformConfigure_DryRunOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte(`
version: 1
event:
  name: "Valid CTF"
  slug: "valid-ctf"
  description: "Test description"
  start_at: "2026-10-15T10:00:00Z"
  end_at: "2026-10-17T18:00:00Z"
platform:
  type: ctfd
  url: "https://ctfd.example.com"
`)
	if err := os.WriteFile(filepath.Join(dir, "mctf.config.yml"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MCTF_CTFD_TOKEN", "mock-token")
	flagDir = dir
	flagConfigureDryRun = true
	defer func() {
		flagDir = ""
		flagConfigureDryRun = false
	}()

	err := platformConfigureCmd.RunE(platformConfigureCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error in dry-run: %v", err)
	}
}
