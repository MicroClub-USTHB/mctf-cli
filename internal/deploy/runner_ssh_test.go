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
		ProjectName: "mctf-sample-event-misc-welcome",
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
	want := "/opt/mctf/mctf-sample-event-misc-welcome"

	if got != want {
		t.Fatalf("remoteProjectDir = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteChallengeDir(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteChallengeDir(u)
	want := "/opt/mctf/mctf-sample-event-misc-welcome/challenge"

	if got != want {
		t.Fatalf("remoteChallengeDir = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteComposeCommandUp(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteComposeCommand(u, "up", false)
	want := "cd '/opt/mctf/mctf-sample-event-misc-welcome/challenge' && PORT_8000=14000 docker compose -p mctf-sample-event-misc-welcome -f docker-compose.yml up -d"

	if got != want {
		t.Fatalf("remoteComposeCommand(up) = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteComposeCommandDown(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteComposeCommand(u, "down", false)
	want := "cd '/opt/mctf/mctf-sample-event-misc-welcome/challenge' && PORT_8000=14000 docker compose -p mctf-sample-event-misc-welcome -f docker-compose.yml down"

	if got != want {
		t.Fatalf("remoteComposeCommand(down) = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteComposeCommandDownVolumes(t *testing.T) {
	r := newTestSSHRunner()
	u := newTestUnit()

	got := r.remoteComposeCommand(u, "down", true)
	want := "cd '/opt/mctf/mctf-sample-event-misc-welcome/challenge' && PORT_8000=14000 docker compose -p mctf-sample-event-misc-welcome -f docker-compose.yml down -v"

	if got != want {
		t.Fatalf("remoteComposeCommand(down -v) = %q, want %q", got, want)
	}
}

func TestSSHRunner_RemoteRootWithSpaces(t *testing.T) {
	// Operators sometimes pick paths with spaces (e.g. NAS mounts). The
	// compose command must still be valid shell — the `cd` path needs
	// quoting, otherwise the && splits the path mid-word.
	r := newTestSSHRunner()
	r.RemoteRoot = "/srv/CTF Stuff"
	u := newTestUnit()
	got := r.remoteComposeCommand(u, "up", false)
	if !strings.HasPrefix(got, "cd '/srv/CTF Stuff/") {
		t.Fatalf("cd path must be shell-quoted, got: %q", got)
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

func TestSSHRunner_PortAndRemoteRootFromConfig(t *testing.T) {
	r := NewSSHRunner(&config.SSHDeploy{
		Host: "example.com", Port: 2222, RemoteRoot: "/srv/mctf",
	}, false)
	if r.Port != 2222 {
		t.Errorf("Port = %d, want 2222", r.Port)
	}
	if r.RemoteRoot != "/srv/mctf" {
		t.Errorf("RemoteRoot = %q, want /srv/mctf", r.RemoteRoot)
	}
}

func TestSSHRunner_DefaultRemoteRoot(t *testing.T) {
	r := NewSSHRunner(&config.SSHDeploy{Host: "example.com"}, false)
	if r.RemoteRoot != defaultSSHRemoteRoot {
		t.Errorf("RemoteRoot = %q, want %q", r.RemoteRoot, defaultSSHRemoteRoot)
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
		return errors.New("connection refused")
	}
	err := r.preflight(context.Background())
	if err == nil {
		t.Fatalf("expected preflight to fail when docker is missing")
	}
	// The underlying ssh error must be preserved so the user sees the real
	// cause (this is the regression test for the misleading-error bug).
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("preflight should wrap the underlying ssh error, got %v", err)
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
		return errors.New("no compose plugin")
	}
	err := r.preflight(context.Background())
	if err == nil {
		t.Fatalf("expected preflight to fail when docker compose is missing")
	}
	if !strings.Contains(err.Error(), "no compose plugin") {
		t.Fatalf("compose preflight should wrap underlying error, got %v", err)
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

func TestSSHRunner_BaseArgsCarryBatchAndHostKey(t *testing.T) {
	// Non-interactive options must always be present so CI deploys don't
	// hang on password or first-time host-key prompts.
	r := newTestSSHRunner()
	got := strings.Join(r.sshBaseArgs(), " ")
	if !strings.Contains(got, "-o BatchMode=yes") {
		t.Errorf("sshBaseArgs missing BatchMode: %q", got)
	}
	if !strings.Contains(got, "-o StrictHostKeyChecking=accept-new") {
		t.Errorf("sshBaseArgs missing StrictHostKeyChecking: %q", got)
	}
	got = strings.Join(r.scpBaseArgs(), " ")
	if !strings.Contains(got, "-o BatchMode=yes") {
		t.Errorf("scpBaseArgs missing BatchMode: %q", got)
	}
}

func TestSSHRunner_PortFlagSSHvsSCP(t *testing.T) {
	// ssh uses lowercase -p, scp uses uppercase -P. Swapping them is a
	// classic mistake.
	r := newTestSSHRunner()
	r.Port = 2222
	ssh := strings.Join(r.sshBaseArgs(), " ")
	scp := strings.Join(r.scpBaseArgs(), " ")
	if !strings.Contains(ssh, "-p 2222") {
		t.Errorf("ssh missing -p 2222: %q", ssh)
	}
	if !strings.Contains(scp, "-P 2222") {
		t.Errorf("scp missing -P 2222: %q", scp)
	}
}

func TestSSHRunner_NoShLcWrapper(t *testing.T) {
	// Past regression: wrapping the remote command in `sh -lc` made ssh
	// pass it as 3 separate args, which the remote shell then split on
	// whitespace, breaking every multi-token command. The arg list must
	// end with one single string (the remote command).
	r := newTestSSHRunner()
	args := r.sshBaseArgs()
	for _, a := range args {
		if a == "sh" || a == "-lc" {
			t.Fatalf("sshBaseArgs must not include 'sh -lc' wrapper: %v", args)
		}
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
	// Dry-run output must be a runnable shell line — no Go-style %q
	// escaping. The remote command should be shell-single-quoted.
	if !strings.Contains(got, "ssh ") {
		t.Fatalf("missing ssh prefix: %q", got)
	}
	if !strings.Contains(got, "deploy@example.com") {
		t.Fatalf("missing target: %q", got)
	}
	if !strings.Contains(got, "'mkdir -p /opt/mctf/test'") {
		t.Fatalf("dry-run should shell-quote remote command (no sh -lc wrapper, no Go escapes), got: %q", got)
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
	if !strings.Contains(got, "scp ") || !strings.Contains(got, "-r ") {
		t.Fatalf("missing scp -r prefix: %q", got)
	}
	if !strings.Contains(got, "'/local/challenge'") {
		t.Fatalf("dry-run should shell-quote paths, got: %q", got)
	}
}
