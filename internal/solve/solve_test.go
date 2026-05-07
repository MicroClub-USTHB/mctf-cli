package solve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func mkChall(t *testing.T, root, cat, slug string) *challenge.Challenge {
	t.Helper()
	chRoot := filepath.Join(root, cat, slug)
	chDir := filepath.Join(chRoot, "challenge")
	solDir := filepath.Join(chRoot, "solution")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(solDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &challenge.Challenge{
		Slug: slug, CategoryDir: cat, RootDir: chRoot, ChallengeDir: chDir, SolutionDir: solDir,
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: slug, Category: cat, Difficulty: "easy", Wave: 1,
				Flags: []challenge.Flag{{Flag: "mctf{victory}", CaseSensitive: true}},
			},
		},
		Archetype: challenge.ArchetypeStatic,
	}
}

func writeSolver(t *testing.T, c *challenge.Challenge, name, body string) {
	t.Helper()
	p := filepath.Join(c.SolutionDir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindSolver_PrefersSh(t *testing.T) {
	root := t.TempDir()
	c := mkChall(t, root, "misc", "x")
	writeSolver(t, c, "solve.sh", "#!/bin/sh\necho hi\n")
	writeSolver(t, c, "solve.py", "print('hi')\n")
	got, err := FindSolver(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "solve.sh") {
		t.Errorf("want solve.sh, got %s", got)
	}
}

func TestFindSolver_ErrNoSolver(t *testing.T) {
	root := t.TempDir()
	c := mkChall(t, root, "misc", "x")
	_, err := FindSolver(c)
	if !errors.Is(err, ErrNoSolver) {
		t.Errorf("want ErrNoSolver, got %v", err)
	}
}

func TestRun_PassWhenFlagInOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fixtures don't run on windows")
	}
	root := t.TempDir()
	c := mkChall(t, root, "misc", "win")
	writeSolver(t, c, "solve.sh", "#!/bin/sh\necho mctf{victory}\n")

	res, err := Run(context.Background(), nil, c, Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pass {
		t.Errorf("want pass, got %+v", res)
	}
	if res.Flag != "mctf{victory}" {
		t.Errorf("flag = %q", res.Flag)
	}
}

func TestRun_FailWhenNoFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fixtures don't run on windows")
	}
	root := t.TempDir()
	c := mkChall(t, root, "misc", "lose")
	writeSolver(t, c, "solve.sh", "#!/bin/sh\necho 'almost'\n")

	res, err := Run(context.Background(), nil, c, Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pass {
		t.Errorf("should not pass without flag: %+v", res)
	}
	if !strings.Contains(res.Reason, "no flag") {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestRun_FailWhenSolverExitsNonZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fixtures don't run on windows")
	}
	root := t.TempDir()
	c := mkChall(t, root, "misc", "broken")
	writeSolver(t, c, "solve.sh", "#!/bin/sh\necho mctf{victory}\nexit 7\n")

	res, err := Run(context.Background(), nil, c, Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pass {
		t.Errorf("non-zero exit should not pass even when flag present")
	}
	if res.ExitCode != 7 {
		t.Errorf("exit code = %d", res.ExitCode)
	}
}

func TestRun_TimeoutKillsSolver(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fixtures don't run on windows")
	}
	root := t.TempDir()
	c := mkChall(t, root, "misc", "slow")
	writeSolver(t, c, "solve.sh", "#!/bin/sh\nsleep 10\n")

	res, err := Run(context.Background(), nil, c, Options{Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pass {
		t.Errorf("timed-out solver should not pass")
	}
	if !strings.Contains(res.Reason, "timed out") {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestRun_StampsEnvForServiceArchetype(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fixtures don't run on windows")
	}
	root := t.TempDir()
	c := mkChall(t, root, "misc", "env")
	c.Manifest.Challenge.NeedDeploy = true
	port := 14000
	internal := 8000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain:    "envtest",
		Port:         &port,
		InternalPort: &internal,
	}
	c.Archetype = challenge.ArchetypeService
	writeSolver(t, c, "solve.sh", `#!/bin/sh
echo HOST=$HOST
echo PORT=$PORT
echo URL=$URL
echo FLAG_PREFIX=$FLAG_PREFIX
echo mctf{victory}
`)
	cfg := &config.Config{
		Event: config.Event{Domain: "ctf.example.com", FlagPrefix: "mctf"},
	}
	res, err := Run(context.Background(), cfg, c, Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Stdout
	for _, want := range []string{
		"HOST=envtest.ctf.example.com",
		"PORT=14000",
		"URL=https://envtest.ctf.example.com:14000",
		"FLAG_PREFIX=mctf",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("env missing %q in output:\n%s", want, out)
		}
	}
	if !res.Pass {
		t.Errorf("want pass, got %+v", res)
	}
}

func TestRun_ErrNoSolver(t *testing.T) {
	root := t.TempDir()
	c := mkChall(t, root, "misc", "nope")
	_, err := Run(context.Background(), nil, c, Options{Timeout: 5 * time.Second})
	if !errors.Is(err, ErrNoSolver) {
		t.Errorf("want ErrNoSolver, got %v", err)
	}
}
