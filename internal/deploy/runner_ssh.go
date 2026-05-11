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
	Host       string
	User       string
	KeyPath    string
	RemoteRoot string
	Stdout     io.Writer
	Stderr     io.Writer
	DryRun     bool
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
	r.KeyPath = cfg.KeyPath
	return r
}

func (r *SSHRunner) Up(ctx context.Context, u Unit) error {
	remoteProjectDir := r.remoteProjectDir(u)

	if err := r.validate(); err != nil {
		return fmt.Errorf("%s: %w", u.Challenge, err)
	}

	if err := r.runSSH(ctx, "mkdir -p "+remoteProjectDir); err != nil {
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

func (r *SSHRunner) runSSH(ctx context.Context, remoteCmd string) error {
	args := append(r.sshBaseArgs(), "sh", "-lc", remoteCmd)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	return cmd.Run()
}

func (r *SSHRunner) runSCP(ctx context.Context, localPath, remotePath string) error {
	args := append(r.scpBaseArgs(), "-r", localPath, remotePath)

	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	return cmd.Run()
}

func (r *SSHRunner) validate() error {
	if r.Host == "" {
		return fmt.Errorf("ssh host is required!!")
	}
	return nil
}
