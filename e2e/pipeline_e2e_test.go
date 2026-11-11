//go:build e2e

// End-to-end pipeline test: validate → generate --check → build → deploy
// → solve → down, driven through the real mctf binary against a copy of
// examples/sample-event and a real docker daemon.
//
// Run with:
//
//	make test-e2e
//
// Requires: docker daemon + compose plugin, git, python3. Skips (not
// fails) when any of those are missing, so the target is safe to run on
// machines without docker.
//
// The sample event is copied into a temp dir and committed to a throwaway
// git repo first: deploy stamps images with HEAD's sha and writes
// mctf-deploy-* tags, neither of which should touch the real checkout.

package e2e

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	// The challenge exercised end to end: a service-archetype
	// (deployed at event time, not per-team) HTTP challenge with a solver.
	category = "misc"
	slug     = "welcome"
	hostPort = 14000 // deploy.port in misc/welcome/challenge/challenge.yml

	// Derived from mctf.config.yml: mctf-<event.slug>-<cat>-<slug> and
	// <registry.url>/<event.slug>-<cat>-<slug>.
	project   = "mctf-sample-event-" + category + "-" + slug
	imageRepo = "ghcr.io/example/sample-event-" + category + "-" + slug
)

func TestE2E_Pipeline(t *testing.T) {
	requireBin(t, "git")
	requireBin(t, "python3")
	requireBin(t, "docker")
	requireDockerDaemon(t)
	requireCompose(t)
	requirePortFree(t, hostPort)

	tmp := t.TempDir()
	bin := buildMCTF(t, tmp)
	event := copySampleEvent(t, tmp)
	sha := gitInitCommit(t, event)

	// Registered before anything touches docker so a failure at any
	// stage still leaves the host clean. Cleanups run LIFO: containers
	// and networks go first, then the images they were using.
	t.Cleanup(func() {
		for _, tag := range []string{imageRepo + ":" + sha, imageRepo + ":latest"} {
			_ = exec.Command("docker", "rmi", "-f", tag).Run()
		}
	})
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", project, "down", "-v", "--remove-orphans").Run()
	})

	mctf := func(t *testing.T, env []string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"-C", event}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mctf %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	steps := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"validate", func(t *testing.T) {
			mctf(t, nil, "validate")
		}},
		{"generate --check", func(t *testing.T) {
			mctf(t, nil, "generate", "--check")
		}},
		{"build", func(t *testing.T) {
			mctf(t, nil, "build", "--category", category, "--slug", slug)
			for _, tag := range []string{imageRepo + ":" + sha, imageRepo + ":latest"} {
				if err := exec.Command("docker", "image", "inspect", tag).Run(); err != nil {
					t.Fatalf("expected image %s after build: %v", tag, err)
				}
			}
		}},
		{"deploy", func(t *testing.T) {
			mctf(t, nil, "deploy", "--category", category, "--slug", slug)
			if ids := composeResources(t, "ps", "-a", "-q"); len(ids) == 0 {
				t.Fatalf("no containers for compose project %s after deploy", project)
			}
		}},
		{"solve", func(t *testing.T) {
			// The manifest URL points at the event domain, which doesn't
			// resolve here; aim the solver at the locally bound port.
			out := mctf(t, []string{fmt.Sprintf("SOLVE_URL=http://127.0.0.1:%d", hostPort)},
				"solve", category+"/"+slug, "--timeout", "60s")
			if !strings.Contains(out, "mctf{w3lc0m3_t0_th3_g4me}") {
				t.Fatalf("solver output does not contain the flag:\n%s", out)
			}
		}},
		{"down", func(t *testing.T) {
			mctf(t, nil, "down", "--category", category, "--slug", slug)
			if ids := composeResources(t, "ps", "-a", "-q"); len(ids) > 0 {
				t.Fatalf("containers left behind after down: %v", ids)
			}
			if ids := composeResources(t, "network", "ls", "-q"); len(ids) > 0 {
				t.Fatalf("networks left behind after down: %v", ids)
			}
		}},
	}

	// Stages depend on each other: stop at the first failure.
	for _, s := range steps {
		if !t.Run(s.name, s.run) {
			t.FailNow()
		}
	}
}

// composeResources lists docker objects (containers or networks) that
// carry the compose project label of the deployed challenge.
func composeResources(t *testing.T, args ...string) []string {
	t.Helper()
	full := append(args, "--filter", "label=com.docker.compose.project="+project)
	out, err := exec.Command("docker", full...).Output()
	if err != nil {
		t.Fatalf("docker %s: %v", strings.Join(full, " "), err)
	}
	return strings.Fields(string(out))
}

// buildMCTF compiles the CLI from the repo root into dir.
func buildMCTF(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "mctf")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// copySampleEvent copies examples/sample-event into dir and drops any
// local runtime state (.mctf/) a developer may have accumulated.
func copySampleEvent(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "examples", "sample-event")
	dst := filepath.Join(dir, "sample-event")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy sample event: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dst, ".mctf")); err != nil {
		t.Fatal(err)
	}
	return dst
}

// gitInitCommit turns dir into a single-commit repo and returns the
// short HEAD sha (the tag build and deploy both derive from it).
func gitInitCommit(t *testing.T, dir string) string {
	t.Helper()
	git := func(args ...string) string {
		t.Helper()
		base := []string{
			"-C", dir,
			"-c", "user.name=mctf-e2e",
			"-c", "user.email=mctf-e2e@example.invalid",
			"-c", "commit.gpgsign=false",
			"-c", "tag.gpgsign=false",
		}
		cmd := exec.Command("git", append(base, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "e2e fixture")
	return git("rev-parse", "--short", "HEAD")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root not found at %s: %v", root, err)
	}
	return root
}

func requireBin(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH: %v", name, err)
	}
}

func requireDockerDaemon(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("docker daemon not reachable: %v", err)
	}
}

func requireCompose(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skipf("docker compose plugin not available: %v", err)
	}
}

// requirePortFree fails (rather than skips) on a busy port: docker is
// available, so a silent skip would hide a real environment problem.
func requirePortFree(t *testing.T, port int) {
	t.Helper()
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port %d is in use; the welcome challenge binds it on deploy: %v", port, err)
	}
	_ = l.Close()
}
