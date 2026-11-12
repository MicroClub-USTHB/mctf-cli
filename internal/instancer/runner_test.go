package instancer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFakeRunner_RecordsCalls(t *testing.T) {
	f := NewFakeRunner()
	spec := SpawnSpec{ID: "a", ComposeFile: "/x.yml"}
	ids, err := f.Up(context.Background(), spec)
	if err != nil || len(ids) != 1 {
		t.Errorf("Up: ids=%v err=%v", ids, err)
	}
	if err := f.Down(context.Background(), spec, true); err != nil {
		t.Fatal(err)
	}
	if len(f.UpCalls) != 1 || f.UpCalls[0] != "a" {
		t.Errorf("UpCalls = %v", f.UpCalls)
	}
	if len(f.DownCalls) != 1 || f.DownCalls[0] != "a" {
		t.Errorf("DownCalls = %v", f.DownCalls)
	}
}

func TestFakeRunner_InjectsErrors(t *testing.T) {
	f := NewFakeRunner()
	f.ErrOnUp = map[string]error{"bad": errors.New("up nope")}
	f.ErrOnDown = map[string]error{"bad": errors.New("down nope")}
	if _, err := f.Up(context.Background(), SpawnSpec{ID: "bad"}); err == nil {
		t.Error("expected Up error")
	}
	if err := f.Down(context.Background(), SpawnSpec{ID: "bad"}, false); err == nil {
		t.Error("expected Down error")
	}
	// And they shouldn't have been recorded.
	if len(f.UpCalls) != 0 || len(f.DownCalls) != 0 {
		t.Errorf("error calls were recorded: %v %v", f.UpCalls, f.DownCalls)
	}
}

func TestCreateNetworkOverride(t *testing.T) {
	runner := NewQuietDockerRunner()
	id := "mctf-inst-cub3-team1-a1b2c3d4"

	path, err := runner.createNetworkOverride(id)
	if err != nil {
		t.Fatalf("createNetworkOverride: %v", err)
	}

	t.Cleanup(func() {
		os.Remove(path)
	})

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read override file: %v", err)
	}

	expected := `networks:
  default:
    external: true
    name: mctf-inst-cub3-team1-a1b2c3d4
`

	if string(content) != expected {
		t.Errorf("override content:\n%s\nwant:\n%s", content, expected)
	}

	if !strings.HasSuffix(path, ".yml") {
		t.Errorf("override file should have .yml extension: %s", path)
	}
}

func TestDockerRunnerUp_CreatesDedicatedNetwork(t *testing.T) {
	var calls [][]string

	runner := &DockerRunner{
		Stdout: io.Discard,
		Stderr: io.Discard,
		runCommand: func(
			ctx context.Context,
			args []string,
			dir string,
			env []string,
			stdout io.Writer,
			stderr io.Writer,
		) ([]byte, error) {
			calls = append(calls, args)

			if len(args) >= 2 && args[len(args)-2] == "ps" && args[len(args)-1] == "-q" {
				return []byte("container-123\n"), nil
			}
			return nil, nil
		},
	}

	spec := SpawnSpec{
		ID:          "mctf-inst-test-123",
		ComposeFile: filepath.Join(t.TempDir(), "docker-compose.yml"),
	}

	containers, err := runner.Up(context.Background(), spec)
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	if !reflect.DeepEqual(containers, []string{"container-123"}) {
		t.Fatalf("Up() containers = %v, want [container-123]", containers)
	}

	if len(calls) != 3 {
		t.Fatalf("expected 3 Docker commands, got %d: %v", len(calls), calls)
	}

	if !reflect.DeepEqual(calls[0], []string{
		"network", "create", "--driver", "bridge", "mctf-inst-test-123",
	}) {
		t.Errorf("first command = %v, want network create", calls[0])
	}

	if calls[1][0] != "compose" || calls[1][len(calls[1])-2] != "up" {
		t.Errorf("second command = %v, want compose up", calls[1])
	}

	if !reflect.DeepEqual(calls[2][len(calls[2])-2:], []string{"ps", "-q"}) {
		t.Errorf("third command = %v, want compose ps -q", calls[2])
	}
}

func TestDockerRunnerDown_RemovesDedicatedNetwork(t *testing.T) {
	var calls [][]string

	runner := &DockerRunner{
		Stdout: io.Discard,
		Stderr: io.Discard,
		runCommand: func(
			ctx context.Context,
			args []string,
			dir string,
			env []string,
			stdout io.Writer,
			stderr io.Writer,
		) ([]byte, error) {
			calls = append(calls, args)
			return nil, nil
		},
	}

	spec := SpawnSpec{
		ID:          "mctf-inst-test-123",
		ComposeFile: filepath.Join(t.TempDir(), "docker-compose.yml"),
	}

	if err := runner.Down(context.Background(), spec, false); err != nil {
		t.Fatalf("Down() error = %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 Docker commands, got %d: %v", len(calls), calls)
	}

	if calls[0][0] != "compose" || calls[0][len(calls[0])-1] != "down" {
		t.Errorf("first command = %v, want compose down", calls[0])
	}

	if !reflect.DeepEqual(calls[1], []string{
		"network", "rm", "mctf-inst-test-123",
	}) {
		t.Errorf("second command = %v, want network rm", calls[1])
	}
}

func TestDockerRunnerUp_CleansUpWhenComposeFails(t *testing.T) {
	var calls [][]string

	runner := &DockerRunner{
		Stdout: io.Discard,
		Stderr: io.Discard,
		runCommand: func(
			ctx context.Context,
			args []string,
			dir string,
			env []string,
			stdout io.Writer,
			stderr io.Writer,
		) ([]byte, error) {
			calls = append(calls, args)

			// Simulate Compose failing to start.
			if args[0] == "compose" && args[len(args)-1] == "-d" {
				return nil, errors.New("simulated compose failure")
			}
			return nil, nil
		},
	}

	spec := SpawnSpec{
		ID:          "mctf-inst-test-123",
		ComposeFile: filepath.Join(t.TempDir(), "docker-compose.yml"),
	}

	_, err := runner.Up(context.Background(), spec)
	if err == nil {
		t.Fatal("expected Up() to return an error")
	}

	if len(calls) != 4 {
		t.Fatalf("expected 4 Docker commands, got %d: %v", len(calls), calls)
	}

	if calls[0][0] != "network" || calls[0][1] != "create" {
		t.Errorf("first command = %v, want network create", calls[0])
	}

	if calls[1][0] != "compose" || calls[1][len(calls[1])-2] != "up" {
		t.Errorf("second command = %v, want compose up", calls[1])
	}

	if calls[2][0] != "compose" || calls[2][len(calls[2])-1] != "down" {
		t.Errorf("third command = %v, want compose down cleanup", calls[2])
	}

	if !reflect.DeepEqual(calls[3], []string{
		"network", "rm", "mctf-inst-test-123",
	}) {
		t.Errorf("fourth command = %v, want network rm", calls[3])
	}
}

func TestDockerRunnerDown_IdempotentWhenNetworkNotFound(t *testing.T) {
	runner := &DockerRunner{
		Stdout: io.Discard,
		Stderr: io.Discard,
		runCommand: func(
			ctx context.Context,
			args []string,
			dir string,
			env []string,
			stdout io.Writer,
			stderr io.Writer,
		) ([]byte, error) {
			if len(args) >= 2 && args[0] == "network" && args[1] == "rm" {
				return nil, errors.New("exit status 1: Error response from daemon: network mctf-inst-test-123 not found")
			}
			return nil, nil
		},
	}

	spec := SpawnSpec{
		ID:          "mctf-inst-test-123",
		ComposeFile: filepath.Join(t.TempDir(), "docker-compose.yml"),
	}

	if err := runner.Down(context.Background(), spec, false); err != nil {
		t.Fatalf("Down() should ignore 'not found' network error, got: %v", err)
	}
}

func TestDockerRunnerUp_IdempotentWhenNetworkAlreadyExists(t *testing.T) {
	runner := &DockerRunner{
		Stdout: io.Discard,
		Stderr: io.Discard,
		runCommand: func(
			ctx context.Context,
			args []string,
			dir string,
			env []string,
			stdout io.Writer,
			stderr io.Writer,
		) ([]byte, error) {
			if len(args) >= 2 && args[0] == "network" && args[1] == "create" {
				return nil, errors.New("exit status 1: Error response from daemon: network with name mctf-inst-test-123 already exists")
			}
			if len(args) >= 2 && args[len(args)-2] == "ps" && args[len(args)-1] == "-q" {
				return []byte("container-123\n"), nil
			}
			return nil, nil
		},
	}

	spec := SpawnSpec{
		ID:          "mctf-inst-test-123",
		ComposeFile: filepath.Join(t.TempDir(), "docker-compose.yml"),
	}

	containers, err := runner.Up(context.Background(), spec)
	if err != nil {
		t.Fatalf("Up() should ignore 'already exists' network error, got: %v", err)
	}
	if !reflect.DeepEqual(containers, []string{"container-123"}) {
		t.Fatalf("Up() containers = %v, want [container-123]", containers)
	}
}
