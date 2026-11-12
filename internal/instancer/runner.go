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
	"time"
)

// Runner brings up, tears down, and inspects compose projects for one
// instance. The DockerRunner is the production impl; tests use a fake.
type Runner interface {
	Up(ctx context.Context, spec SpawnSpec) (containers []string, err error)
	Down(ctx context.Context, spec SpawnSpec, volumes bool) error
}

// NetworkCleaner removes an instance's dedicated Docker network.
// It is optional so existing Runner implementations remain compatible.
type NetworkCleaner interface {
	RemoveNetwork(ctx context.Context, id string) error
}

// DockerRunner shells out to `docker compose`. stdout/stderr stream to
// the configured writers (default: os.Stdout / os.Stderr).
type DockerRunner struct {
	Stdout io.Writer
	Stderr io.Writer

	runCommand func(
		ctx context.Context,
		args []string,
		dir string,
		env []string,
		stdout io.Writer,
		stderr io.Writer,
	) ([]byte, error)
}

func executeDockerCommand(
	ctx context.Context,
	args []string,
	dir string,
	env []string,
	stdout io.Writer,
	stderr io.Writer,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	cmd.Env = env

	var errBuf strings.Builder
	if stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, &errBuf)
	} else {
		cmd.Stderr = &errBuf
	}

	var output strings.Builder
	if stdout != nil {
		cmd.Stdout = io.MultiWriter(stdout, &output)
	} else {
		cmd.Stdout = &output
	}

	err := cmd.Run()
	if err != nil && errBuf.Len() > 0 {
		return []byte(output.String()), fmt.Errorf("%w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return []byte(output.String()), err
}

// NewDockerRunner returns a runner streaming to the process's
// stdout/stderr. The daemon uses io.Discard to keep its log clean.
func NewDockerRunner() *DockerRunner {
	return &DockerRunner{Stdout: os.Stdout, Stderr: os.Stderr, runCommand: executeDockerCommand}
}

// NewQuietDockerRunner suppresses all command output. The daemon
// generally wants this so a busy event doesn't fill its log with
// docker pull spam.
func NewQuietDockerRunner() *DockerRunner {
	return &DockerRunner{Stdout: io.Discard, Stderr: io.Discard, runCommand: executeDockerCommand}
}

// createNetwork creates a dedicated bridge network for an instance.
// If the network already exists, it is treated as a success (idempotent).
func (d *DockerRunner) createNetwork(ctx context.Context, id string) error {
	_, err := d.runCommand(
		ctx,
		[]string{"network", "create", "--driver", "bridge", NetworkName(id)},
		"",
		os.Environ(),
		d.Stdout,
		d.Stderr,
	)
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "already exists") {
			return nil
		}
		return fmt.Errorf("create instance network: %w", err)
	}
	return nil
}

// removeNetwork removes the dedicated network belonging to an instance.
// If the network is already gone, it is treated as a success (idempotent).
func (d *DockerRunner) removeNetwork(ctx context.Context, id string) error {
	_, err := d.runCommand(
		ctx,
		[]string{"network", "rm", NetworkName(id)},
		"",
		os.Environ(),
		d.Stdout,
		d.Stderr,
	)
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such network") {
			return nil
		}
		return fmt.Errorf("remove instance network: %w", err)
	}
	return nil
}

// RemoveNetwork removes the dedicated network for an instance.
func (d *DockerRunner) RemoveNetwork(ctx context.Context, id string) error {
	return d.removeNetwork(ctx, id)
}

// createNetworkOverride creates a temporary Compose file that connects
// the project's default network to the instance's dedicated network.
func (d *DockerRunner) createNetworkOverride(id string) (string, error) {
	networkName := NetworkName(id)

	content := fmt.Sprintf(`networks:
  default:
    external: true
    name: %s
`, networkName)

	file, err := os.CreateTemp("", "mctf-inst-*.yml")
	if err != nil {
		return "", fmt.Errorf("create network override: %w", err)
	}

	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("write network override: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("close network override: %w", err)
	}

	return file.Name(), nil
}

// Up runs `docker compose up -d` with the spec's env vars and returns
// the container IDs that compose now knows about. A failed up yields
// the docker exit code wrapped — the caller handles cleanup.
func (d *DockerRunner) Up(ctx context.Context, s SpawnSpec) ([]string, error) {
	// Create the dedicated network before starting the containers.
	if err := d.createNetwork(ctx, s.ID); err != nil {
		return nil, err
	}

	// Generate a temporary Compose override that uses this network.
	overrideFile, err := d.createNetworkOverride(s.ID)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if cleanupErr := d.removeNetwork(cleanupCtx, s.ID); cleanupErr != nil {
			return nil, fmt.Errorf(
				"%w (network cleanup failed: %v)",
				err,
				cleanupErr,
			)
		}
		return nil, err
	}
	defer func() { _ = os.Remove(overrideFile) }()

	// Start the Compose project on its dedicated network.
	_, err = d.runCommand(
		ctx,
		UpArgs(s, overrideFile),
		filepath.Dir(s.ComposeFile),
		append(os.Environ(), s.EnvSlice()...),
		d.Stdout,
		d.Stderr,
	)
	if err != nil {
		// Compose may have partially started containers. Stop them before
		// removing their network. Use a bounded timeout detached from ctx
		// in case ctx has already timed out or been canceled.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, downErr := d.runCommand(
			cleanupCtx,
			DestroyArgs(s, false, overrideFile),
			filepath.Dir(s.ComposeFile),
			os.Environ(),
			d.Stdout,
			d.Stderr,
		)
		removeErr := d.removeNetwork(cleanupCtx, s.ID)

		if downErr != nil || removeErr != nil {
			return nil, fmt.Errorf(
				"docker compose up: %w (cleanup errors: down=%v, network=%v)",
				err, downErr, removeErr,
			)
		}
		return nil, fmt.Errorf("docker compose up: %w", err)
	}

	return d.containerIDs(ctx, s)
}

// Down runs `docker compose down`. volumes=true also strips named volumes.
func (d *DockerRunner) Down(ctx context.Context, s SpawnSpec, volumes bool) error {
	overrideFile, err := d.createNetworkOverride(s.ID)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(overrideFile) }()

	_, err = d.runCommand(
		ctx,
		DestroyArgs(s, volumes, overrideFile),
		filepath.Dir(s.ComposeFile),
		os.Environ(),
		d.Stdout,
		d.Stderr,
	)
	if err != nil {
		return fmt.Errorf("docker compose down: %w", err)
	}

	if err := d.removeNetwork(ctx, s.ID); err != nil {
		return err
	}

	return nil
}

// containerIDs runs `docker compose ps -q` to enumerate the IDs after
// a successful Up. Failures are non-fatal — the caller will still
// have a usable Instance record.
func (d *DockerRunner) containerIDs(ctx context.Context, s SpawnSpec) ([]string, error) {
	output, err := d.runCommand(
		ctx,
		[]string{
			"compose",
			"-p", s.ID,
			"-f", s.ComposeFile,
			"ps", "-q",
		},
		filepath.Dir(s.ComposeFile),
		os.Environ(),
		d.Stdout,
		d.Stderr,
	)
	if err != nil {
		// Preserve the existing behavior: container ID lookup is best-effort.
		return nil, nil
	}

	return strings.Fields(string(output)), nil
}

// FakeRunner is the test double. It records every Up/Down call and
// hands back canned container IDs. ErrOn lets tests inject failures
// for specific instance IDs.
type FakeRunner struct {
	mu                 sync.Mutex
	UpCalls            []string
	DownCalls          []string
	ErrOnUp            map[string]error
	ErrOnDown          map[string]error
	IDsForUp           []string // returned by Up unless overridden by ErrOnUp
	UpHook             func(ctx context.Context, s SpawnSpec)
	RemoveNetworkCalls []string
	ErrOnRemoveNetwork map[string]error
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

func (f *FakeRunner) RemoveNetwork(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err, ok := f.ErrOnRemoveNetwork[id]; ok {
		return err
	}

	f.RemoveNetworkCalls = append(f.RemoveNetworkCalls, id)
	return nil
}
