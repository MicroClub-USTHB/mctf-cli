package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// DockerRunner shells out to the docker CLI. stdout/stderr stream
// directly to the configured writers (default: os.Stdout / os.Stderr).
type DockerRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

// NewDockerRunner returns a runner that streams output to stdout/stderr.
func NewDockerRunner() *DockerRunner {
	return &DockerRunner{Stdout: os.Stdout, Stderr: os.Stderr}
}

// Build runs `docker build` for the spec.
func (d *DockerRunner) Build(ctx context.Context, spec ImageSpec, opts Options) error {
	cmd := exec.CommandContext(ctx, "docker", BuildArgs(spec, opts)...)
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr
	return cmd.Run()
}

// Push runs `docker push` for each tag in the spec.
func (d *DockerRunner) Push(ctx context.Context, spec ImageSpec, opts Options) error {
	for _, tag := range spec.Tags {
		cmd := exec.CommandContext(ctx, "docker", PushArgs(tag)...)
		cmd.Stdout = d.Stdout
		cmd.Stderr = d.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("push %s: %w", tag, err)
		}
	}
	return nil
}

// DryRunner prints the would-be commands instead of executing them.
type DryRunner struct {
	Stdout io.Writer
}

// NewDryRunner returns a runner that prints commands to stdout.
func NewDryRunner() *DryRunner { return &DryRunner{Stdout: os.Stdout} }

// Build prints the docker build command.
func (d *DryRunner) Build(_ context.Context, spec ImageSpec, opts Options) error {
	fmt.Fprintf(d.Stdout, "docker %s\n", argsString(BuildArgs(spec, opts)))
	return nil
}

// Push prints the docker push command.
func (d *DryRunner) Push(_ context.Context, spec ImageSpec, _ Options) error {
	for _, tag := range spec.Tags {
		fmt.Fprintf(d.Stdout, "docker %s\n", argsString(PushArgs(tag)))
	}
	return nil
}

func argsString(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		// Quote args that contain spaces; everything from BuildArgs is
		// well-formed so this is purely cosmetic for terminal display.
		if hasSpace(a) {
			out += `"` + a + `"`
		} else {
			out += a
		}
	}
	return out
}

func hasSpace(s string) bool {
	for _, r := range s {
		if r == ' ' {
			return true
		}
	}
	return false
}
