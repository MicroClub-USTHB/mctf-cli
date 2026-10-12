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

func TestRun_StampsEnvForInstance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fixtures don't run on windows")
	}
	root := t.TempDir()
	c := mkChall(t, root, "web", "stateful")
	c.Manifest.Challenge.NeedDeploy = true
	internal := 8000
	c.Manifest.Deploy = &challenge.DeployBlock{InternalPort: &internal}
	c.Archetype = challenge.ArchetypeInstanced
	writeSolver(t, c, "solve.sh", `#!/bin/sh
echo HOST=$HOST
echo PORT=$PORT
echo URL=$URL
echo INSTANCE_ID=$INSTANCE_ID
echo TEAM_ID=$TEAM_ID
echo $FLAG
`)
	inst := &InstanceTarget{
		ID:     "mctf-inst-stateful-team1-ab12cd34",
		TeamID: "team1",
		Ports:  map[int]int{internal: 31044},
		Flag:   "mctf{per_team_dynamic}",
	}
	res, err := Run(context.Background(), nil, c, Options{Timeout: 5 * time.Second, Instance: inst})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Stdout
	for _, want := range []string{
		"HOST=127.0.0.1",
		"PORT=31044",
		"URL=http://127.0.0.1:31044",
		"INSTANCE_ID=mctf-inst-stateful-team1-ab12cd34",
		"TEAM_ID=team1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("env missing %q in output:\n%s", want, out)
		}
	}
	// The per-team flag is recovered even though it isn't in the manifest.
	if res.Flag != "mctf{per_team_dynamic}" {
		t.Errorf("flag = %q, want per-team flag", res.Flag)
	}
	if !res.Pass {
		t.Errorf("want pass, got %+v", res)
	}
}

func TestRun_InstanceTCPOmitsURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fixtures don't run on windows")
	}
	root := t.TempDir()
	c := mkChall(t, root, "pwn", "tcpchal")
	c.Manifest.Challenge.NeedDeploy = true
	internal := 9000
	c.Manifest.Deploy = &challenge.DeployBlock{InternalPort: &internal, TCP: true}
	c.Archetype = challenge.ArchetypeInstanced
	writeSolver(t, c, "solve.sh", "#!/bin/sh\necho \"URL=[$URL]\"\necho PORT=$PORT\necho mctf{victory}\n")
	inst := &InstanceTarget{ID: "x", Ports: map[int]int{internal: 30500}}
	res, err := Run(context.Background(), nil, c, Options{Timeout: 5 * time.Second, Instance: inst})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stdout, "URL=[]") {
		t.Errorf("tcp challenge should not set URL, got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "PORT=30500") {
		t.Errorf("tcp challenge should still set PORT, got:\n%s", res.Stdout)
	}
}

func TestInstanceHostPort(t *testing.T) {
	internal := 8000
	withPort := &challenge.Challenge{
		Manifest: challenge.Manifest{Deploy: &challenge.DeployBlock{InternalPort: &internal}},
	}
	cases := []struct {
		name  string
		c     *challenge.Challenge
		ports map[int]int
		want  int
	}{
		{"declared internal port", withPort, map[int]int{8000: 31044}, 31044},
		{"sole mapping fallback", &challenge.Challenge{}, map[int]int{1234: 40000}, 40000},
		{"no ports", withPort, nil, 0},
		{"ambiguous, no declared port", &challenge.Challenge{}, map[int]int{1: 2, 3: 4}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := instanceHostPort(tc.c, tc.ports); got != tc.want {
				t.Errorf("instanceHostPort = %d, want %d", got, tc.want)
			}
		})
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
