package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

const defaultSSHRemoteRoot = "/opt/mctf"

// SSHRunner implements deploy.Runner for single-host SSH deployments.
//
// It copies the local challenge/ directory to
// <RemoteRoot>/<project-name>/challenge and runs `docker compose` remotely
// inside that directory.
//
// Up flow:   validate → preflight (docker, compose) → mkdir → scp → compose up.
// Down flow: validate → compose down. (No preflight: a partially-torn-down
// remote should still accept compose-down.)
//
// With DryRun=true, ssh/scp commands are printed instead of executed.
type SSHRunner struct {
	Host       string
	Port       int
	User       string
	KeyPath    string
	RemoteRoot string
	Stdout     io.Writer
	Stderr     io.Writer
	DryRun     bool

	runSSHFn func(ctx context.Context, remoteCmd string) error
}

func NewSSHRunner(cfg *config.SSHDeploy, dryRun bool) *SSHRunner {
	r := &SSHRunner{
		RemoteRoot: defaultSSHRemoteRoot,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		DryRun:     dryRun,
	}
	if cfg == nil {
		return r
	}
	r.Host = cfg.Host
	r.Port = cfg.Port
	r.User = cfg.User
	if r.User == "" {
		r.User = "root"
	}
	r.KeyPath = expandUserPath(cfg.KeyPath)
	if cfg.RemoteRoot != "" {
		r.RemoteRoot = cfg.RemoteRoot
	}
	return r
}

func (r *SSHRunner) Up(ctx context.Context, u Unit) error {
	remoteProjectDir := r.remoteProjectDir(u)

	if err := r.validate(); err != nil {
		return fmt.Errorf("%s: %w", u.Challenge, err)
	}
	if err := r.preflight(ctx); err != nil {
		return fmt.Errorf("%s: %w", u.Challenge, err)
	}
	if err := r.runSSH(ctx, "mkdir -p "+shellQuote(remoteProjectDir)); err != nil {
		return fmt.Errorf("%s: create remote project dir: %w", u.Challenge, err)
	}
	if err := r.runSCP(ctx, u.Cwd, r.target()+":"+remoteProjectDir); err != nil {
		return fmt.Errorf("%s: copy challenge tree: %w", u.Challenge, err)
	}
	if err := r.runSSH(ctx, r.remoteComposeCommand(u, "up", false)); err != nil {
		return fmt.Errorf("%s: remote compose up: %w", u.Challenge, err)
	}
	return nil
}

func (r *SSHRunner) Down(ctx context.Context, u Unit, volumes bool) error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("%s: %w", u.Challenge, err)
	}
	if err := r.runSSH(ctx, r.remoteComposeCommand(u, "down", volumes)); err != nil {
		return fmt.Errorf("%s: remote compose down: %w", u.Challenge, err)
	}
	return nil
}

func (r *SSHRunner) remoteProjectDir(u Unit) string {
	return filepath.ToSlash(filepath.Join(r.RemoteRoot, u.ProjectName))
}

func (r *SSHRunner) remoteChallengeDir(u Unit) string {
	return filepath.ToSlash(filepath.Join(r.remoteProjectDir(u), "challenge"))
}

func (r *SSHRunner) remoteComposeCommand(u Unit, action string, volumes bool) string {
	dir := r.remoteChallengeDir(u)

	args := []string{
		"docker", "compose",
		"-p", u.ProjectName,
		"-f", "docker-compose.yml",
		action,
	}
	if action == "up" {
		args = append(args, "-d")
	}
	if action == "down" && volumes {
		args = append(args, "-v")
	}
	composeCmd := strings.Join(args, " ")

	if env := EnvSlice(u.Env); len(env) > 0 {
		composeCmd = strings.Join(env, " ") + " " + composeCmd
	}
	return "cd " + shellQuote(dir) + " && " + composeCmd
}

func (r *SSHRunner) target() string {
	if r.User == "" {
		return r.Host
	}
	return r.User + "@" + r.Host
}

// sshOptions returns the common -o flags. Non-interactive by default so a
// deploy from CI doesn't hang on a password prompt or first-time host-key
// confirmation. accept-new pins the key on first use; subsequent key changes
// still fail loudly.
func sshOptions() []string {
	return []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
	}
}

func (r *SSHRunner) sshBaseArgs() []string {
	args := sshOptions()
	if r.Port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", r.Port))
	}
	if r.KeyPath != "" {
		args = append(args, "-i", r.KeyPath)
	}
	args = append(args, r.target())
	return args
}

func (r *SSHRunner) scpBaseArgs() []string {
	// scp uses -P (capital) for port, unlike ssh's lowercase -p.
	args := sshOptions()
	if r.Port > 0 {
		args = append(args, "-P", fmt.Sprintf("%d", r.Port))
	}
	if r.KeyPath != "" {
		args = append(args, "-i", r.KeyPath)
	}
	return args
}

func (r *SSHRunner) preflight(ctx context.Context) error {
	if err := r.runSSH(ctx, "command -v docker >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("remote docker check failed (host unreachable, key wrong, or docker missing): %w", err)
	}
	if err := r.runSSH(ctx, "docker compose version >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("remote docker compose check failed (need v2 plugin): %w", err)
	}
	return nil
}

// runSSH executes one remote shell command. The whole command must be
// passed as a single ssh argument: ssh joins its trailing args with spaces
// and ships that as one string to the remote sshd, which re-parses it.
// Splitting it ourselves would corrupt embedded spaces in the command.
func (r *SSHRunner) runSSH(ctx context.Context, remoteCmd string) error {
	if r.runSSHFn != nil {
		return r.runSSHFn(ctx, remoteCmd)
	}
	args := append(r.sshBaseArgs(), remoteCmd)

	if r.DryRun {
		fmt.Fprintf(r.Stdout, "ssh %s %s\n",
			strings.Join(r.sshBaseArgs(), " "), shellQuote(remoteCmd))
		return nil
	}

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	return cmd.Run()
}

func (r *SSHRunner) runSCP(ctx context.Context, localPath, remotePath string) error {
	args := append(r.scpBaseArgs(), "-r", localPath, remotePath)

	if r.DryRun {
		fmt.Fprintf(r.Stdout, "scp %s -r %s %s\n",
			strings.Join(r.scpBaseArgs(), " "), shellQuote(localPath), shellQuote(remotePath))
		return nil
	}

	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	return cmd.Run()
}

func (r *SSHRunner) validate() error {
	if r.Host == "" {
		return fmt.Errorf("ssh host is required")
	}
	if r.KeyPath != "" {
		if _, err := os.Stat(r.KeyPath); err != nil {
			return fmt.Errorf("ssh key not found: %s", r.KeyPath)
		}
	}
	return nil
}

func expandUserPath(path string) string {
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		trimmed := strings.TrimPrefix(path, "~")
		trimmed = strings.TrimPrefix(trimmed, "/")
		trimmed = strings.TrimPrefix(trimmed, "\\")
		return filepath.Join(home, filepath.FromSlash(trimmed))
	}
	return path
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
