package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	JumpHost   string
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
	r.JumpHost = cfg.JumpHost
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

	composeBase := filepath.Base(u.ComposeFile)
	args := []string{
		"docker", "compose",
		"-p", u.ProjectName,
		"-f", composeBase,
		action,
	}
	if action == "up" {
		// --wait blocks until every service is healthy (or running, if
		// no healthcheck is declared). Without it, `up -d` returns the
		// moment containers start, so a deploy can "succeed" while the
		// service is still booting and players hit 502s.
		args = append(args, "-d", "--wait")
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

// commonSSHOptions returns the -o / -J flags shared by ssh and scp:
//
//   - BatchMode=yes and StrictHostKeyChecking=accept-new keep CI deploys
//     non-interactive while still pinning the host key on first use.
//   - When KeyPath is set we also pin IdentitiesOnly=yes so ssh stops at
//     the configured key instead of cycling through every key in the
//     user's ssh-agent (which can trip too-many-auth-attempts on some
//     hosts).
//   - JumpHost forwards through a bastion via -J.
func (r *SSHRunner) commonSSHOptions() []string {
	opts := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if r.KeyPath != "" {
		opts = append(opts, "-o", "IdentitiesOnly=yes")
	}
	if r.JumpHost != "" {
		opts = append(opts, "-J", r.JumpHost)
	}
	return opts
}

func (r *SSHRunner) sshBaseArgs() []string {
	args := r.commonSSHOptions()
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
	args := r.commonSSHOptions()
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
		fi, err := os.Stat(r.KeyPath)
		if err != nil {
			return fmt.Errorf("ssh key not found: %s", r.KeyPath)
		}
		// ssh refuses to use a key with too-permissive bits and prints a
		// hard-to-spot error. Fail fast with a clearer message. Skip on
		// Windows where unix mode bits are simulated and meaningless.
		if runtime.GOOS != "windows" {
			if mode := fi.Mode().Perm(); mode&0o077 != 0 {
				return fmt.Errorf("ssh key %s has overly-permissive permissions %#o (group/other readable); run: chmod 600 %s",
					r.KeyPath, mode, r.KeyPath)
			}
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
