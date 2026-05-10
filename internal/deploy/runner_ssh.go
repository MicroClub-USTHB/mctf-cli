package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
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
}

func NewSSHRunner(cfg *config.SSHDeploy) *SSHRunner {
	r := &SSHRunner{
		RemoteRoot: defaultSSHRemoteRoot,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}
	if cfg == nil {
		return r
	}
	r.Host = cfg.Host
	r.User = cfg.User
	r.KeyPath = cfg.KeyPath
	return r
}

func (r *SSHRunner) Up(_ context.Context, u Unit) error {
	return fmt.Errorf("%s: ssh deploy mode not implemented yet", u.Challenge)
}

func (r *SSHRunner) Down(_ context.Context, u Unit, _ bool) error {
	return fmt.Errorf("%s: ssh deploy mode not implemented yet", u.Challenge)
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
