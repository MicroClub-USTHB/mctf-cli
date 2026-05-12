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

type SSHRunner struct {
	/// SSHRunner implements deploy.Runner for single-host SSH deployments.
	// It copies the local challenge/ directory to:
	// /opt/mctf/<project-name>/challenge
	// then runs `docker compose` remotely inside that directory.
	// Up flow:
    //   - validate SSH config
    //   - check remote docker/docker compose
    //   - create remote project dir
    //   - copy challenge/ with scp
    //   - run remote `docker compose up -d``
    // Down flow:
    //   - validate SSH config
    //   - run remote `docker compose down` in the same directory
    // With DryRun enabled, ssh/scp commands are printed instead of executed.

	Host       string
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
	r.User = cfg.User
	if r.User == "" {
		r.User = "root"
	}
	r.KeyPath = expandUserPath(cfg.KeyPath)
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

	env := EnvSlice(u.Env)
	if len(env) > 0 {
		composeCmd = strings.Join(env, " ") + " " + composeCmd
	}
	return "cd " + dir + " && " + composeCmd
}

func (r *SSHRunner) target() string {
	if r.User == "" {
		return r.Host
	}
	return r.User + "@" + r.Host
}

func (r *SSHRunner) sshBaseArgs() []string {
	var args []string
	if r.KeyPath != "" {
		args = append(args, "-i", r.KeyPath)
	}
	args = append(args, r.target())
	return args
}

func (r *SSHRunner) scpBaseArgs() []string {
	var args []string
	if r.KeyPath != "" {
		args = append(args, "-i", r.KeyPath)
	}
	return args
}

func (r *SSHRunner) preflight(ctx context.Context) error {
	if err := r.runSSH(ctx, "command -v docker >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("docker not found on remote host")
	}
	if err := r.runSSH(ctx, "docker compose version >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("docker compose not available on remote host")
	}
	return nil
}

func (r *SSHRunner) runSSH(ctx context.Context, remoteCmd string) error {
	if r.runSSHFn != nil {
		return r.runSSHFn(ctx, remoteCmd)
	}
	args := append(r.sshBaseArgs(), "sh", "-lc", remoteCmd)

	if r.DryRun {
		fmt.Fprintf(r.Stdout, "ssh %s sh -lc %q\n", strings.Join(r.sshBaseArgs(), " "), remoteCmd)
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
		fmt.Fprintf(r.Stdout, "scp %s -r %s %s\n", strings.Join(r.scpBaseArgs(), " "), localPath, remotePath)
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
