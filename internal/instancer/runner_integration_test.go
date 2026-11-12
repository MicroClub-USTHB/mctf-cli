package instancer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDockerRunnerNetworkIsolationIntegration(t *testing.T) {
	if os.Getenv("MCTF_DOCKER_INTEGRATION") != "1" {
		t.Skip("set MCTF_DOCKER_INTEGRATION=1 to run Docker integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner := NewQuietDockerRunner()
	dir := t.TempDir()

	composeA := filepath.Join(dir, "a.yml")
	composeB := filepath.Join(dir, "b.yml")

	err := os.WriteFile(composeA, []byte(`
services:
  app:
    image: alpine:3.21
    command: ["sh", "-c", "sleep 600"]
`), 0600)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(composeB, []byte(`
services:
  app:
    image: python:3.13-alpine
    command:
      - sh
      - -c
      - mkdir -p /tmp/www && echo ok > /tmp/www/index.html && python -m http.server 8080 --directory /tmp/www
`), 0600)
	if err != nil {
		t.Fatal(err)
	}

	specA := SpawnSpec{
		ID:          "mctf-inst-integration-a",
		ComposeFile: composeA,
	}

	specB := SpawnSpec{
		ID:          "mctf-inst-integration-b",
		ComposeFile: composeB,
	}

	var startedA, startedB bool

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			time.Minute,
		)
		defer cleanupCancel()

		if startedA {
			if err := runner.Down(cleanupCtx, specA, true); err != nil {
				t.Logf("cleanup A: %v", err)
			}
		}

		if startedB {
			if err := runner.Down(cleanupCtx, specB, true); err != nil {
				t.Logf("cleanup B: %v", err)
			}
		}
	})

	idsA, err := runner.Up(ctx, specA)
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	startedA = true

	idsB, err := runner.Up(ctx, specB)
	if err != nil {
		t.Fatalf("start instance B: %v", err)
	}
	startedB = true

	if len(idsA) == 0 || len(idsB) == 0 {
		t.Fatalf("expected container IDs; A=%v B=%v", idsA, idsB)
	}

	networkA := NetworkName(specA.ID)
	networkB := NetworkName(specB.ID)

	if networkA == networkB {
		t.Fatal("instances unexpectedly share a network name")
	}

	for _, network := range []string{networkA, networkB} {
		_, err := runner.runCommand(
			ctx,
			[]string{"network", "inspect", network},
			"",
			os.Environ(),
			io.Discard,
			io.Discard,
		)
		if err != nil {
			t.Fatalf("network %s was not created: %v", network, err)
		}
	}

	
	output, err := runner.runCommand(
		ctx,
		[]string{
			"inspect",
			"--format={{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
			idsB[0],
		},
		"",
		os.Environ(),
		io.Discard,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("inspect container B: %v", err)
	}

	ipB := string(output)
	if ipB == "" {
		t.Fatal("container B has no network IP")
	}

	
	var serverReady bool

	for i := 0; i < 20; i++ {
		_, err = runner.runCommand(
			ctx,
			[]string{
				"exec", idsB[0], "python", "-c",
				"import urllib.request; print(urllib.request.urlopen('http://127.0.0.1:8080/').read())",
			},
			"",
			os.Environ(),
			io.Discard,
			io.Discard,
		)

		if err == nil {
			serverReady = true
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	if !serverReady {
		t.Fatal("container B's HTTP server did not become ready within 10 seconds")
	}

	
	_, err = runner.runCommand(
		ctx,
		[]string{
			"exec", idsA[0], "wget", "-T", "3", "-qO-",
			fmt.Sprintf("http://%s:8080/", ipB),
		},
		"",
		os.Environ(),
		io.Discard,
		io.Discard,
	)
	if err == nil {
		t.Fatal("network isolation failed: instance A reached instance B")
	}

	
	if err := runner.Down(ctx, specA, true); err != nil {
		t.Fatalf("stop instance A: %v", err)
	}
	startedA = false

	if _, err := runner.runCommand(
		ctx,
		[]string{"network", "inspect", networkA},
		"",
		os.Environ(),
		io.Discard,
		io.Discard,
	); err == nil {
		t.Fatalf("network %s still exists after Down", networkA)
	}

	
	if err := runner.Down(ctx, specB, true); err != nil {
		t.Fatalf("stop instance B: %v", err)
	}
	startedB = false

	if _, err := runner.runCommand(
		ctx,
		[]string{"network", "inspect", networkB},
		"",
		os.Environ(),
		io.Discard,
		io.Discard,
	); err == nil {
		t.Fatalf("network %s still exists after Down", networkB)
	}
}
