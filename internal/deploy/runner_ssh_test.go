package deploy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func newTestSSHRunner() *SSHRunner {
	return &SSHRunner{
		Host:       "example.com",
		User:       "deploy",
		KeyPath:    "/home/me/.ssh/id_rsa",
		RemoteRoot: "/opt/mctf",
	}
}

func newTestUnit() Unit {
	return Unit{
		Challenge:   "misc/welcome",
		Category:    "misc",
		Slug:        "welcome",
		ProjectName: "mtcf-sample-event-misc-welcome",
		Cwd:         "/local/challenge",
		ComposeFile: "/local/challenge/docker-compose.yml",
		Env: map[string]string{
			"PORT_8000": "14000",
		},
	}
}

func TestSSHRunner_RemoteProjectDir(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteProjectDir(u)
	want := "/opt/mctf/mtcf-sample-event-misc-welcome"

	if got != want {
		t.Fatalf("remoteProjectDir = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteChallengeDir(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteChallengeDir(u)
	want := "/opt/mctf/mtcf-sample-event-misc-welcome/challenge"

	if got != want {
		t.Fatalf("remoteChallengeDir = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteComposeCommandUp(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteComposeCommand(u, "up", false)
	want := "cd /opt/mctf/mtcf-sample-event-misc-welcome/challenge && PORT_8000=14000 docker compose -p mtcf-sample-event-misc-welcome -f docker-compose.yml up -d"

	if got != want {
		t.Fatalf("remoteComposeCommand(up) = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteComposeCommandDown(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteComposeCommand(u, "down", false)
	want := "cd /opt/mctf/mtcf-sample-event-misc-welcome/challenge && PORT_8000=14000 docker compose -p mtcf-sample-event-misc-welcome -f docker-compose.yml down"

	if got != want {
		t.Fatalf("remoteComposeCommand(down) = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteComposeCommandDownVolumes(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteComposeCommand(u, "down", true)
	want := "cd /opt/mctf/mtcf-sample-event-misc-welcome/challenge && PORT_8000=14000 docker compose -p mtcf-sample-event-misc-welcome -f docker-compose.yml down -v"

	if got != want {
		t.Fatalf("remoteComposeCommand(down -v) = %q, want %q", got, want)
	}
}

func TestSSHRunner_TargetWithUser(t *testing.T) {
	r := newTestSSHRunner()

	got := r.target()
	want := "deploy@example.com"

	if got != want {
		t.Fatalf("target() = %q, want %q", got, want)
	}
}

func TestSSHRunner_TargetWithoutUser(t *testing.T) {
	r := &SSHRunner{
		Host: "example.com",
	}

	got := r.target()
	want := "example.com"

	if got != want {
		t.Fatalf("target() without user = %q, want %q", got, want)
	}
}

func TestSSHRunner_ValidateRequiresHost(t *testing.T) {
	r := &SSHRunner{}

	err := r.validate()
	if err == nil {
		t.Fatal("expected validate to fail when host is empty")
	}
}

func TestSSHRunner_ValidateAcceptsHostOnly(t *testing.T) {
	r := &SSHRunner{
		Host: "example.com",
	}

	err := r.validate()
	if err != nil {
		t.Fatalf("expected validate to accept host-only runner, got %v", err)
	}
}

func TestSSHRunner_DefaultUserIsRoot(t *testing.T) {
	r := NewSSHRunner(&config.SSHDeploy{Host: "example.com"}, true)
	if r.User != "root" {
		t.Fatalf("expected default user to be root, got %q", r.User)
	}
}

func TestSSHRunner_ValidateKeyPath(t *testing.T) {
	file, err := os.CreateTemp("", "mctf-ssh-key-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(file.Name())

	r := &SSHRunner{
		Host:    "example.com",
		KeyPath: file.Name(),
	}
	if err := r.validate(); err != nil {
		t.Fatalf("expected validate to accept existing key, got %v", err)
	}

	r.KeyPath = filepath.Join(os.TempDir(), "does-not-exist")
	if err := r.validate(); err == nil {
		t.Fatalf("expected validate to fail for missing key")
	}
}

func TestExpandUserPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home dir: %v", err)
	}
	got := expandUserPath("~/id_rsa")
	want := filepath.Join(home, "id_rsa")
	if got != want {
		t.Fatalf("expandUserPath = %q, want %q", got, want)
	}
}

func TestSSHRunner_PreflightDockerMissing(t *testing.T) {
	r := &SSHRunner{Host: "example.com"}
	r.runSSHFn = func(_ context.Context, _ string) error {
		return errors.New("no docker")
	}
	if err := r.preflight(context.Background()); err == nil {
		t.Fatalf("expected preflight to fail when docker is missing")
	}
}

func TestSSHRunner_PreflightComposeMissing(t *testing.T) {
	r := &SSHRunner{Host: "example.com"}
	call := 0
	r.runSSHFn = func(_ context.Context, _ string) error {
		call++
		if call == 1 {
			return nil
		}
		return errors.New("no compose")
	}
	if err := r.preflight(context.Background()); err == nil {
		t.Fatalf("expected preflight to fail when docker compose is missing")
	}
}

func TestSSHRunner_PreflightOK(t *testing.T) {
	r := &SSHRunner{Host: "example.com"}
	r.runSSHFn = func(_ context.Context, _ string) error {
		return nil
	}
	if err := r.preflight(context.Background()); err != nil {
		t.Fatalf("expected preflight to pass, got %v", err)
	}
}

func TestSSHRunner_RunSSHDryRun(t *testing.T) {
	var out bytes.Buffer

	r := &SSHRunner{
		Host:       "example.com",
		User:       "deploy",
		KeyPath:    "/home/me/.ssh/id_rsa",
		RemoteRoot: "/opt/mctf",
		Stdout:     &out,
		Stderr:     &out,
		DryRun:     true,
	}

	err := r.runSSH(context.Background(), "mkdir -p /opt/mctf/test")
	if err != nil {
		t.Fatalf("runSSH dry-run returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "ssh -i /home/me/.ssh/id_rsa deploy@example.com sh -lc") {
		t.Fatalf("unexpected runSSH dry-run output: %q", got)
	}
	if !strings.Contains(got, "mkdir -p /opt/mctf/test") {
		t.Fatalf("runSSH dry-run output missing remote command: %q", got)
	}
}

func TestSSHRunner_RunSCPDryRun(t *testing.T) {
	var out bytes.Buffer

	r := &SSHRunner{
		Host:       "example.com",
		User:       "deploy",
		KeyPath:    "/home/me/.ssh/id_rsa",
		RemoteRoot: "/opt/mctf",
		Stdout:     &out,
		Stderr:     &out,
		DryRun:     true,
	}

	err := r.runSCP(context.Background(), "/local/challenge", "deploy@example.com:/opt/mctf/proj")
	if err != nil {
		t.Fatalf("runSCP dry-run returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "scp -i /home/me/.ssh/id_rsa -r /local/challenge deploy@example.com:/opt/mctf/proj") {
		t.Fatalf("unexpected runSCP dry-run output: %q", got)
	}
}
