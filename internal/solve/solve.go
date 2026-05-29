// Package solve runs a challenge's solution script and verifies the
// recovered flag matches the manifest.
//
// Convention: a challenge with a solver places one of solve.sh,
// solve.py, or an executable named solve under solution/. The runner
// stamps HOST/PORT/URL/FLAG_PREFIX env vars derived from the deploy
// block, runs the script with a timeout, and looks for any of the
// manifest's flag values in the captured output.
package solve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// Result is one challenge's solver outcome.
type Result struct {
	Challenge string        `json:"challenge"`
	Solver    string        `json:"solver"`
	ExitCode  int           `json:"exit_code"`
	Duration  time.Duration `json:"duration_ns"`
	Stdout    string        `json:"stdout,omitempty"`
	Stderr    string        `json:"stderr,omitempty"`
	Flag      string        `json:"flag,omitempty"`
	Pass      bool          `json:"pass"`
	Reason    string        `json:"reason"`
}

// Options narrows or alters the run.
type Options struct {
	// Timeout kills the solver after this duration. Defaults to 60s.
	Timeout time.Duration
	// Stdout/Stderr (optional) receive a streaming copy of the solver's
	// output. The Result still captures full stdout/stderr regardless.
	Stdout io.Writer
	Stderr io.Writer
	// Instance, when set, points the solver at a live per-team instance
	// instead of the deploy block: HOST/PORT/URL are derived from the
	// instance's host-port mapping, and its per-team flag is accepted in
	// addition to the manifest flags.
	Instance *InstanceTarget
}

// InstanceTarget describes a live per-team instance the solver should
// run against. The cmd layer builds this from the instancer store so
// the solve package stays decoupled from instancer.
type InstanceTarget struct {
	// ID is the instance handle, exported to the solver as INSTANCE_ID.
	ID string
	// TeamID is exported to the solver as TEAM_ID (omitted when empty).
	TeamID string
	// Host is where the instance is reachable; defaults to 127.0.0.1.
	Host string
	// Ports maps internal container ports to allocated host ports.
	Ports map[int]int
	// Flag is the per-team flag value, accepted in addition to the
	// manifest flags (omitted when empty).
	Flag string
}

// ErrNoSolver is returned when no solve.{sh,py,binary} exists.
var ErrNoSolver = errors.New("no solver script (looked for solve.sh, solve.py, solve)")

// FindSolver returns the absolute path to the first solver script
// present in the challenge's solution/ directory, or ErrNoSolver.
func FindSolver(c *challenge.Challenge) (string, error) {
	candidates := []string{"solve.sh", "solve.py", "solve"}
	for _, name := range candidates {
		p := filepath.Join(c.SolutionDir, name)
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		return p, nil
	}
	return "", ErrNoSolver
}

// Run executes the solver for one challenge and returns the result.
//
// For deployable challenges, env vars are derived from the deploy
// block; for static challenges they are omitted (the solver typically
// works on assets/ alone). Instanced challenges receive the instancer
// URL but no per-team HOST/PORT, since smoke-testing instanced
// challenges requires spawning an instance first (out of scope for v0).
func Run(ctx context.Context, cfg *config.Config, c *challenge.Challenge, opts Options) (*Result, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	solverPath, err := FindSolver(c)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Challenge: c.CategoryDir + "/" + c.Slug,
		Solver:    solverPath,
	}

	cmdCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Absolutize so a relative -C invocation still resolves correctly.
	absSolver, err := filepath.Abs(solverPath)
	if err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(c.SolutionDir)
	if err != nil {
		return nil, err
	}

	cmd := commandFor(cmdCtx, absSolver)
	cmd.Dir = absDir
	if opts.Instance != nil {
		cmd.Env = append(os.Environ(), envForInstance(cfg, c, opts.Instance)...)
	} else {
		cmd.Env = append(os.Environ(), envForChallenge(cfg, c)...)
	}
	// Don't wait forever for I/O to drain after the context cancels;
	// orphaned grandchild processes (e.g. a `sleep` spawned by sh)
	// can keep the stdout pipe open even after sh itself is killed.
	cmd.WaitDelay = 500 * time.Millisecond

	var stdout, stderr bytes.Buffer
	if opts.Stdout != nil {
		cmd.Stdout = io.MultiWriter(&stdout, opts.Stdout)
	} else {
		cmd.Stdout = &stdout
	}
	if opts.Stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, opts.Stderr)
	} else {
		cmd.Stderr = &stderr
	}

	start := time.Now()
	runErr := cmd.Run()
	res.Duration = time.Since(start)
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		if cmdCtx.Err() == context.DeadlineExceeded {
			res.Reason = fmt.Sprintf("solver timed out after %s", opts.Timeout)
			return res, nil
		}
	}

	candidates := c.Manifest.Challenge.Flags
	if opts.Instance != nil && opts.Instance.Flag != "" {
		// Accept the per-team flag too — a dynamic-flag instance can
		// recover a value the manifest doesn't carry.
		candidates = append([]challenge.Flag{{Flag: opts.Instance.Flag}}, candidates...)
	}
	res.Flag = findFlag(res.Stdout+"\n"+res.Stderr, candidates)
	res.Pass = res.Flag != "" && res.ExitCode == 0
	res.Reason = explain(res)
	return res, nil
}

func commandFor(ctx context.Context, solverPath string) *exec.Cmd {
	switch {
	case strings.HasSuffix(solverPath, ".sh"):
		return exec.CommandContext(ctx, "sh", solverPath)
	case strings.HasSuffix(solverPath, ".py"):
		return exec.CommandContext(ctx, "python3", solverPath)
	default:
		// Assumed executable
		return exec.CommandContext(ctx, solverPath)
	}
}

// flagPrefix resolves the event flag prefix, defaulting to "mctf".
func flagPrefix(cfg *config.Config) string {
	if cfg != nil && cfg.Event.FlagPrefix != "" {
		return cfg.Event.FlagPrefix
	}
	return "mctf"
}

// envForChallenge derives HOST / PORT / URL / FLAG_PREFIX so solvers
// can read them with a uniform contract.
func envForChallenge(cfg *config.Config, c *challenge.Challenge) []string {
	env := []string{"FLAG_PREFIX=" + flagPrefix(cfg)}

	if c.Manifest.Deploy == nil {
		return env
	}
	d := c.Manifest.Deploy
	domain := ""
	if cfg != nil {
		domain = cfg.Event.Domain
	}

	switch c.Archetype {
	case challenge.ArchetypeService:
		if d.Subdomain != "" && domain != "" {
			env = append(env, "HOST="+d.Subdomain+"."+domain)
		}
		if d.Port != nil && *d.Port > 0 {
			env = append(env, fmt.Sprintf("PORT=%d", *d.Port))
		}
		if !d.TCP && d.Subdomain != "" && domain != "" {
			scheme := "https"
			if d.NoSSL {
				scheme = "http"
			}
			if d.Port != nil {
				env = append(env, fmt.Sprintf("URL=%s://%s.%s:%d", scheme, d.Subdomain, domain, *d.Port))
			} else {
				env = append(env, fmt.Sprintf("URL=%s://%s.%s", scheme, d.Subdomain, domain))
			}
		}
	case challenge.ArchetypeInstanced:
		if cfg != nil && cfg.Instancer != nil && cfg.Instancer.URL != "" {
			env = append(env, "INSTANCER_URL="+cfg.Instancer.URL)
		} else if domain != "" {
			env = append(env, "INSTANCER_URL=https://instancer."+domain)
		}
	}
	return env
}

// envForInstance derives the solver env for a run against a live
// per-team instance: HOST/PORT/URL from the instance's host-port
// mapping, plus INSTANCE_ID / TEAM_ID / FLAG / FLAG_PREFIX.
func envForInstance(cfg *config.Config, c *challenge.Challenge, inst *InstanceTarget) []string {
	env := []string{"FLAG_PREFIX=" + flagPrefix(cfg)}

	host := inst.Host
	if host == "" {
		host = "127.0.0.1"
	}
	env = append(env, "HOST="+host, "INSTANCE_ID="+inst.ID)
	if inst.TeamID != "" {
		env = append(env, "TEAM_ID="+inst.TeamID)
	}
	if inst.Flag != "" {
		env = append(env, "FLAG="+inst.Flag)
	}

	if hostPort := instanceHostPort(c, inst.Ports); hostPort > 0 {
		env = append(env, fmt.Sprintf("PORT=%d", hostPort))
		tcp := c.Manifest.Deploy != nil && c.Manifest.Deploy.TCP
		if !tcp {
			env = append(env, fmt.Sprintf("URL=http://%s:%d", host, hostPort))
		}
	}
	return env
}

// instanceHostPort resolves the host port the solver should hit: the
// mapping for the challenge's declared internal port, or the sole
// mapping when only one exists.
func instanceHostPort(c *challenge.Challenge, ports map[int]int) int {
	if len(ports) == 0 {
		return 0
	}
	if c.Manifest.Deploy != nil && c.Manifest.Deploy.InternalPort != nil {
		if hp, ok := ports[*c.Manifest.Deploy.InternalPort]; ok {
			return hp
		}
	}
	if len(ports) == 1 {
		for _, hp := range ports {
			return hp
		}
	}
	return 0
}

func findFlag(haystack string, candidates []challenge.Flag) string {
	for _, f := range candidates {
		if f.Flag == "" {
			continue
		}
		if strings.Contains(haystack, f.Flag) {
			return f.Flag
		}
		// Case-insensitive fallback when the manifest says so.
		if !f.CaseSensitive && strings.Contains(strings.ToLower(haystack), strings.ToLower(f.Flag)) {
			return f.Flag
		}
	}
	return ""
}

func explain(r *Result) string {
	switch {
	case r.Pass:
		return "flag recovered"
	case r.ExitCode != 0 && r.Flag == "":
		return fmt.Sprintf("solver exited %d, no flag in output", r.ExitCode)
	case r.ExitCode != 0:
		return fmt.Sprintf("solver exited %d (flag was found but the run failed)", r.ExitCode)
	case r.Flag == "":
		return "solver ran cleanly but produced no flag"
	default:
		return ""
	}
}
