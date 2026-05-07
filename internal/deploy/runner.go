package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// DockerRunner shells out to `docker compose`.
type DockerRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

// NewDockerRunner returns a runner that streams output to stdout/stderr.
func NewDockerRunner() *DockerRunner {
	return &DockerRunner{Stdout: os.Stdout, Stderr: os.Stderr}
}

// Up runs `docker compose up -d` for the unit.
func (d *DockerRunner) Up(ctx context.Context, u Unit) error {
	cmd := exec.CommandContext(ctx, "docker", UpArgs(u)...)
	cmd.Dir = u.Cwd
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr
	cmd.Env = mergeEnv(u.Env)
	return cmd.Run()
}

// Down runs `docker compose down` for the unit. volumes=true strips
// named volumes too.
func (d *DockerRunner) Down(ctx context.Context, u Unit, volumes bool) error {
	cmd := exec.CommandContext(ctx, "docker", DownArgs(u, volumes)...)
	cmd.Dir = u.Cwd
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr
	cmd.Env = mergeEnv(u.Env)
	return cmd.Run()
}

func mergeEnv(extra map[string]string) []string {
	env := os.Environ()
	for _, kv := range EnvSlice(extra) {
		env = append(env, kv)
	}
	return env
}

// DryRunner prints what would be run instead of running it.
type DryRunner struct {
	Stdout io.Writer
}

// NewDryRunner returns a runner that prints to stdout.
func NewDryRunner() *DryRunner { return &DryRunner{Stdout: os.Stdout} }

// Up prints the would-be `docker compose up -d` invocation.
func (d *DryRunner) Up(_ context.Context, u Unit) error {
	fmt.Fprintf(d.Stdout, "(cd %s; %s)\n", u.Cwd, CommandPreview(UpArgs(u), u.Env))
	return nil
}

// Down prints the would-be `docker compose down` invocation.
func (d *DryRunner) Down(_ context.Context, u Unit, volumes bool) error {
	fmt.Fprintf(d.Stdout, "(cd %s; %s)\n", u.Cwd, CommandPreview(DownArgs(u, volumes), u.Env))
	return nil
}
