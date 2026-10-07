package instancer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Runner brings up, tears down, and inspects compose projects for one
// instance. The DockerRunner is the production impl; tests use a fake.
type Runner interface {
	Up(ctx context.Context, spec SpawnSpec) (containers []string, err error)
	Down(ctx context.Context, spec SpawnSpec, volumes bool) error
}

// DockerRunner shells out to `docker compose`. stdout/stderr stream to
// the configured writers (default: os.Stdout / os.Stderr).
type DockerRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

// NewDockerRunner returns a runner streaming to the process's
// stdout/stderr. The daemon uses io.Discard to keep its log clean.
func NewDockerRunner() *DockerRunner {
	return &DockerRunner{Stdout: os.Stdout, Stderr: os.Stderr}
}

// NewQuietDockerRunner suppresses all command output. The daemon
// generally wants this so a busy event doesn't fill its log with
// docker pull spam.
func NewQuietDockerRunner() *DockerRunner {
	return &DockerRunner{Stdout: io.Discard, Stderr: io.Discard}
}

// Up runs `docker compose up -d` with the spec's env vars and returns
// the container IDs that compose now knows about. A failed up yields
// the docker exit code wrapped — the caller handles cleanup.
func (d *DockerRunner) Up(ctx context.Context, s SpawnSpec) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", UpArgs(s)...)
	cmd.Dir = filepath.Dir(s.ComposeFile)
	cmd.Env = append(os.Environ(), s.EnvSlice()...)
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker compose up: %w", err)
	}
	return d.containerIDs(ctx, s)
}

// Down runs `docker compose down`. volumes=true also strips named volumes.
func (d *DockerRunner) Down(ctx context.Context, s SpawnSpec, volumes bool) error {
	cmd := exec.CommandContext(ctx, "docker", DestroyArgs(s, volumes)...)
	cmd.Dir = filepath.Dir(s.ComposeFile)
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose down: %w", err)
	}
	return nil
}

// containerIDs runs `docker compose ps -q` to enumerate the IDs after
// a successful Up. Failures are non-fatal — the caller will still
// have a usable Instance record.
func (d *DockerRunner) containerIDs(ctx context.Context, s SpawnSpec) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", s.ID, "-f", s.ComposeFile, "ps", "-q")
	cmd.Dir = filepath.Dir(s.ComposeFile)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil // best-effort
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

// FakeRunner is the test double. It records every Up/Down call and
// hands back canned container IDs. ErrOn lets tests inject failures
// for specific instance IDs.
type FakeRunner struct {
	mu        sync.Mutex
	UpCalls   []string
	DownCalls []string
	ErrOnUp   map[string]error
	ErrOnDown map[string]error
	IDsForUp  []string // returned by Up unless overridden by ErrOnUp
	UpHook    func(ctx context.Context, s SpawnSpec)
}

// NewFakeRunner returns a runner that succeeds for every call,
// returning a single fake container ID per Up.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{IDsForUp: []string{"fake-container-1"}}
}

func (f *FakeRunner) Up(ctx context.Context, s SpawnSpec) ([]string, error) {
	if f.UpHook != nil {
		f.UpHook(ctx, s)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.ErrOnUp[s.ID]; ok {
		return nil, err
	}
	f.UpCalls = append(f.UpCalls, s.ID)
	return append([]string(nil), f.IDsForUp...), nil
}

func (f *FakeRunner) Down(_ context.Context, s SpawnSpec, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.ErrOnDown[s.ID]; ok {
		return err
	}
	f.DownCalls = append(f.DownCalls, s.ID)
	return nil
}
