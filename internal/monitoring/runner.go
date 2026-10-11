package monitoring

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner brings the stack up or tears it down from its compose directory.
type Runner interface {
	Up(ctx context.Context, dir string) error
	Down(ctx context.Context, dir string, volumes bool) error
}

// upArgs / downArgs centralize the docker compose invocation so the real
// and dry runners stay in lockstep.
func upArgs() []string { return []string{"compose", "up", "-d"} }

func downArgs(volumes bool) []string {
	args := []string{"compose", "down"}
	if volumes {
		args = append(args, "-v")
	}
	return args
}

// DockerRunner shells out to `docker compose` in the stack directory.
type DockerRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

// NewDockerRunner returns a runner that streams output to stdout/stderr.
func NewDockerRunner() *DockerRunner {
	return &DockerRunner{Stdout: os.Stdout, Stderr: os.Stderr}
}

// Up runs `docker compose up -d` in dir.
func (d *DockerRunner) Up(ctx context.Context, dir string) error {
	return d.run(ctx, dir, upArgs())
}

// Down runs `docker compose down` (with -v when volumes) in dir.
func (d *DockerRunner) Down(ctx context.Context, dir string, volumes bool) error {
	return d.run(ctx, dir, downArgs(volumes))
}

func (d *DockerRunner) run(ctx context.Context, dir string, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr
	return cmd.Run()
}

// DryRunner prints what would run instead of running it.
type DryRunner struct {
	Stdout io.Writer
}

// NewDryRunner returns a runner that prints to stdout.
func NewDryRunner() *DryRunner { return &DryRunner{Stdout: os.Stdout} }

// Up prints the would-be up invocation.
func (d *DryRunner) Up(_ context.Context, dir string) error {
	fmt.Fprintf(d.Stdout, "(cd %s; docker %s)\n", dir, strings.Join(upArgs(), " "))
	return nil
}

// Down prints the would-be down invocation.
func (d *DryRunner) Down(_ context.Context, dir string, volumes bool) error {
	fmt.Fprintf(d.Stdout, "(cd %s; docker %s)\n", dir, strings.Join(downArgs(volumes), " "))
	return nil
}
