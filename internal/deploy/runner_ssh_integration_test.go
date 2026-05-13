//go:build integration

// End-to-end SSH-runner integration test.
//
// Boots an alpine sshd container with the host docker socket mounted in,
// then exercises a real Up → ps → Down round-trip through SSHRunner.
//
// Run with:
//   go test -tags integration ./internal/deploy/...
//
// Requires: docker daemon reachable, plus ssh, scp, ssh-keygen on PATH.
// Skips (not fails) when any of those are missing, so the same target
// works on machines without docker.

package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIntegration_SSHRunner_UpAndDown(t *testing.T) {
	requireBin(t, "docker")
	requireDockerDaemon(t)
	requireBin(t, "ssh")
	requireBin(t, "scp")
	requireBin(t, "ssh-keygen")

	tmp := t.TempDir()

	// Force ssh to use a sandboxed known_hosts so the test doesn't
	// pollute the user's ~/.ssh — accept-new will write into here.
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	// 1. Ephemeral keypair.
	keyPath := filepath.Join(tmp, "id_test")
	mustRun(t, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath, "-C", "mctf-it")
	pubKey := keyPath + ".pub"

	// 2. Build sshd+docker-cli image (small, mostly cached after first run).
	buildCtx := filepath.Join(tmp, "build")
	if err := os.MkdirAll(buildCtx, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerfile := `FROM alpine:3.20
RUN apk add --no-cache openssh-server docker-cli docker-compose && \
    ssh-keygen -A && mkdir -p /root/.ssh && \
    printf 'PermitRootLogin yes\nPubkeyAuthentication yes\nPasswordAuthentication no\n' >> /etc/ssh/sshd_config
EXPOSE 22
CMD ["/usr/sbin/sshd","-D","-e"]
`
	if err := os.WriteFile(filepath.Join(buildCtx, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatal(err)
	}
	imgTag := fmt.Sprintf("mctf-it-sshd:%d", time.Now().UnixNano())
	mustRun(t, "docker", "build", "-t", imgTag, buildCtx)
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "-f", imgTag).Run() })

	// 3. Run sshd container on a random host port with host docker socket.
	cname := fmt.Sprintf("mctf-it-sshd-%d", time.Now().UnixNano())
	mustRun(t, "docker", "run", "-d", "--name", cname,
		"-p", "0:22",
		"-v", pubKey+":/root/.ssh/authorized_keys:ro",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		imgTag,
	)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", cname).Run() })

	port := containerHostPort(t, cname, "22/tcp")
	waitSSH(t, "localhost", port, keyPath)

	// 4. Unit under test: the testdata compose. Use a unique project name
	//    so concurrent integration runs don't collide.
	// The fixture lives under testdata/it_challenge/challenge/ — the
	// trailing `challenge/` directory name is load-bearing: the runner
	// scp's that directory verbatim and `cd`s into `challenge/` remotely.
	repoRel := mustRepoRel(t)
	challengeDir := filepath.Join(repoRel, "internal", "deploy", "testdata", "it_challenge", "challenge")
	projectName := fmt.Sprintf("mctf-it-test-%d", time.Now().UnixNano())
	u := Unit{
		Challenge:   "test/it",
		Category:    "test",
		Slug:        "it",
		ProjectName: projectName,
		Cwd:         challengeDir,
		ComposeFile: filepath.Join(challengeDir, "docker-compose.yml"),
	}

	runner := &SSHRunner{
		Host:       "localhost",
		Port:       port,
		User:       "root",
		KeyPath:    keyPath,
		RemoteRoot: "/opt/mctf-it",
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}

	// Cleanup safety net: always tear down the deployed compose project,
	// even if the test panics between Up and Down.
	t.Cleanup(func() {
		_ = runner.Down(context.Background(), u, true)
	})

	// 5. Up.
	if err := runner.Up(context.Background(), u); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// 6. Container should exist on the host docker daemon (via the mounted
	//    socket the sshd container shares with the host).
	if !containerPresent(t, projectName) {
		t.Fatalf("after Up: expected a container with project prefix %q on host docker", projectName)
	}

	// 7. Down.
	if err := runner.Down(context.Background(), u, true); err != nil {
		t.Fatalf("Down: %v", err)
	}

	// 8. Container should be gone (including stopped state — Down removes).
	if containerPresentAny(t, projectName) {
		t.Errorf("after Down: container with project prefix %q still present", projectName)
	}
}

// --- helpers ---

func requireBin(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH", name)
	}
}

func requireDockerDaemon(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "version").Run(); err != nil {
		t.Skipf("docker daemon not reachable: %v", err)
	}
}

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func containerHostPort(t *testing.T, cname, internal string) int {
	t.Helper()
	out, err := exec.Command("docker", "port", cname, internal).Output()
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	// docker port output is "0.0.0.0:55432\n[::]:55432\n" — take first line.
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	parts := strings.Split(line, ":")
	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		t.Fatalf("parse port from %q: %v", line, err)
	}
	return port
}

func waitSSH(t *testing.T, host string, port int, keyPath string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		out, err := exec.Command("ssh",
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "ConnectTimeout=2",
			"-i", keyPath,
			"-p", strconv.Itoa(port),
			"root@"+host,
			"echo ready",
		).CombinedOutput()
		if err == nil && strings.Contains(string(out), "ready") {
			return
		}
		lastErr = fmt.Errorf("%v: %s", err, out)
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("sshd on %s:%d did not become ready in time; last error: %v", host, port, lastErr)
}

func containerPresent(t *testing.T, projectPrefix string) bool {
	t.Helper()
	out, err := exec.Command("docker", "ps",
		"--filter", "name="+projectPrefix,
		"--format", "{{.Names}}",
	).Output()
	if err != nil {
		t.Fatalf("docker ps: %v", err)
	}
	return strings.Contains(string(out), projectPrefix)
}

func containerPresentAny(t *testing.T, projectPrefix string) bool {
	t.Helper()
	out, err := exec.Command("docker", "ps", "-a",
		"--filter", "name="+projectPrefix,
		"--format", "{{.Names}}",
	).Output()
	if err != nil {
		t.Fatalf("docker ps -a: %v", err)
	}
	return strings.Contains(string(out), projectPrefix)
}

// mustRepoRel walks up from the test's working directory (typically the
// package dir) to find the repo root, identified by go.mod.
func mustRepoRel(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate go.mod above %s", dir)
	return ""
}
