// Package doctor runs preflight checks against an event repo and the
// host environment.
//
// Each check produces a Check record (ID, status, detail, suggestion).
// The set is intentionally non-blocking: a single Fail does not abort
// the run — callers decide policy. The CLI exits non-zero only when at
// least one check has Status == Fail.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/validate"
)

// Status is a check outcome.
type Status string

const (
	StatusOK   Status = "ok"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
	StatusSkip Status = "skip"
)

// Check is one preflight observation.
type Check struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Suggest string `json:"suggest,omitempty"`
}

// Options narrows the run.
type Options struct {
	// SkipEnv skips host-environment probes (docker, env vars). Useful
	// for tests and CI where the binaries aren't expected.
	SkipEnv bool
}

// Run executes all checks against dir and returns them in stable order.
func Run(dir string, opts Options) []Check {
	var checks []Check

	cfg, cfgCheck := checkConfig(dir)
	checks = append(checks, cfgCheck)
	if cfg == nil {
		// Without config, downstream checks have nothing to anchor on.
		return checks
	}

	challs, loadErrs, dCheck := checkDiscover(dir, cfg)
	checks = append(checks, dCheck)
	if len(challs) > 0 || len(loadErrs) > 0 {
		checks = append(checks, checkValidate(cfg, dir, challs, loadErrs))
	}

	if !opts.SkipEnv {
		checks = append(checks, checkDockerBinary())
		checks = append(checks, checkDockerDaemon())
		checks = append(checks, checkDockerCompose())
		checks = append(checks, checkGit())
		checks = append(checks, checkRegistryCreds())
		checks = append(checks, checkCTFdToken())
		if len(challs) > 0 {
			checks = append(checks, checkPortsFree(challs))
		}
	}

	return checks
}

func checkConfig(dir string) (*config.Config, Check) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, Check{
			ID: "D001", Name: "event config",
			Status: StatusFail, Detail: err.Error(),
			Suggest: "run `mtcf init` to scaffold a new event, or fix mtcf.config.yml",
		}
	}
	return cfg, Check{
		ID: "D001", Name: "event config",
		Status: StatusOK, Detail: "mtcf.config.yml validates against schema v1",
	}
}

func checkDiscover(dir string, cfg *config.Config) ([]*challenge.Challenge, map[string]error, Check) {
	challs, loadErrs, err := challenge.Discover(dir, cfg.ChallengesDir)
	if err != nil {
		return nil, nil, Check{
			ID: "D002", Name: "challenges directory",
			Status: StatusFail, Detail: err.Error(),
			Suggest: "create the directory or fix challenges_dir in mtcf.config.yml",
		}
	}
	if len(challs) == 0 {
		return challs, loadErrs, Check{
			ID: "D002", Name: "challenges directory",
			Status: StatusWarn, Detail: "no challenges discovered",
			Suggest: "scaffold one with `mtcf init challenge <category>/<slug>`",
		}
	}
	return challs, loadErrs, Check{
		ID: "D002", Name: "challenges directory",
		Status: StatusOK, Detail: fmt.Sprintf("%d challenge(s)", len(challs)),
	}
}

func checkValidate(cfg *config.Config, dir string, challs []*challenge.Challenge, loadErrs map[string]error) Check {
	report := validate.Run(cfg, dir, challs, loadErrs)
	e, w, _ := report.Counts()
	switch {
	case e > 0:
		return Check{
			ID: "D003", Name: "challenge validation",
			Status: StatusFail,
			Detail: fmt.Sprintf("%d error(s), %d warning(s)", e, w),
			Suggest: "run `mtcf validate` for details",
		}
	case w > 0:
		return Check{
			ID: "D003", Name: "challenge validation",
			Status: StatusWarn,
			Detail: fmt.Sprintf("%d warning(s) (no errors)", w),
		}
	default:
		return Check{
			ID: "D003", Name: "challenge validation",
			Status: StatusOK,
		}
	}
}

func checkDockerBinary() Check {
	if _, err := exec.LookPath("docker"); err != nil {
		return Check{
			ID: "D010", Name: "docker binary",
			Status: StatusWarn, Detail: "`docker` not found in $PATH",
			Suggest: "install Docker — required for `mtcf build` and `mtcf deploy`",
		}
	}
	return Check{ID: "D010", Name: "docker binary", Status: StatusOK}
}

func checkDockerDaemon() Check {
	if _, err := exec.LookPath("docker"); err != nil {
		return Check{ID: "D011", Name: "docker daemon", Status: StatusSkip, Detail: "docker not installed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Use Output() so stderr noise doesn't end up in our detail string;
	// `docker info` can exit 0 even when the daemon is unreachable
	// (stdout empty, error on stderr). Treat empty stdout as failure.
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Output()
	version := strings.TrimSpace(string(out))
	if err != nil || version == "" {
		return Check{
			ID: "D011", Name: "docker daemon",
			Status: StatusWarn, Detail: "docker daemon not reachable",
			Suggest: "start Docker Desktop, or `systemctl start docker`",
		}
	}
	return Check{
		ID: "D011", Name: "docker daemon",
		Status: StatusOK, Detail: "server " + version,
	}
}

func checkDockerCompose() Check {
	if _, err := exec.LookPath("docker"); err != nil {
		return Check{ID: "D012", Name: "docker compose v2", Status: StatusSkip, Detail: "docker not installed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "compose", "version", "--short").CombinedOutput()
	if err != nil {
		return Check{
			ID: "D012", Name: "docker compose v2",
			Status: StatusWarn, Detail: "`docker compose` plugin not available",
			Suggest: "install the docker-compose-plugin",
		}
	}
	return Check{
		ID: "D012", Name: "docker compose v2",
		Status: StatusOK, Detail: "v" + strings.TrimSpace(string(out)),
	}
}

func checkGit() Check {
	if _, err := exec.LookPath("git"); err != nil {
		return Check{
			ID: "D013", Name: "git binary",
			Status: StatusWarn, Detail: "`git` not in $PATH",
			Suggest: "install git — used to derive image tags from commit sha",
		}
	}
	return Check{ID: "D013", Name: "git binary", Status: StatusOK}
}

func checkRegistryCreds() Check {
	user := os.Getenv("MTCF_REGISTRY_USER")
	token := os.Getenv("MTCF_REGISTRY_TOKEN")
	if user != "" && token != "" {
		return Check{
			ID: "D020", Name: "registry credentials",
			Status: StatusOK, Detail: "MTCF_REGISTRY_USER and MTCF_REGISTRY_TOKEN set",
		}
	}
	return Check{
		ID: "D020", Name: "registry credentials",
		Status: StatusWarn, Detail: "MTCF_REGISTRY_USER / MTCF_REGISTRY_TOKEN not set",
		Suggest: "required for `mtcf build --push`",
	}
}

func checkCTFdToken() Check {
	if os.Getenv("MTCF_CTFD_TOKEN") != "" {
		return Check{ID: "D021", Name: "CTFd token", Status: StatusOK}
	}
	return Check{
		ID: "D021", Name: "CTFd token",
		Status: StatusWarn, Detail: "MTCF_CTFD_TOKEN not set",
		Suggest: "required for `mtcf deploy` (CTFd registration)",
	}
}

// checkPortsFree probes every service-archetype challenge's host port
// to verify nothing else is already bound. Doesn't try to identify the
// holder — operators can run lsof / ss themselves.
func checkPortsFree(challs []*challenge.Challenge) Check {
	type binding struct {
		port int
		slug string
	}
	var bindings []binding
	for _, c := range challs {
		if c.Archetype != challenge.ArchetypeService {
			continue
		}
		if c.Manifest.Deploy == nil || c.Manifest.Deploy.Port == nil {
			continue
		}
		bindings = append(bindings, binding{
			port: *c.Manifest.Deploy.Port,
			slug: c.CategoryDir + "/" + c.Slug,
		})
	}
	if len(bindings) == 0 {
		return Check{
			ID: "D030", Name: "host ports free",
			Status: StatusSkip, Detail: "no service-archetype challenges to probe",
		}
	}

	var busy []string
	for _, b := range bindings {
		if !portFree(b.port) {
			busy = append(busy, fmt.Sprintf("%d (%s)", b.port, b.slug))
		}
	}
	sort.Strings(busy)
	if len(busy) == 0 {
		return Check{
			ID: "D030", Name: "host ports free",
			Status: StatusOK,
			Detail: fmt.Sprintf("%d service port(s) checked, all free", len(bindings)),
		}
	}
	return Check{
		ID: "D030", Name: "host ports free",
		Status: StatusWarn,
		Detail: fmt.Sprintf("port(s) already in use: %s", strings.Join(busy, ", ")),
		Suggest: "stop the process holding the port (lsof -i :<port>) or change deploy.port",
	}
}

// portFree reports whether tcp/<port> on 127.0.0.1 can be bound.
//
// We bind to 127.0.0.1 (not 0.0.0.0) because some ports may be in use
// by macOS-only services on the loopback even when they're free
// externally — and the docker compose mapping binds host-side too.
func portFree(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// Counts breaks down the result set by status.
func Counts(checks []Check) (ok, fail, warn, skip int) {
	for _, c := range checks {
		switch c.Status {
		case StatusOK:
			ok++
		case StatusFail:
			fail++
		case StatusWarn:
			warn++
		case StatusSkip:
			skip++
		}
	}
	return
}

// Render produces a human-readable summary.
func Render(checks []Check) string {
	var sb strings.Builder
	for _, c := range checks {
		if c.Detail == "" {
			fmt.Fprintf(&sb, "  [%s] %-6s %s\n", statusBadge(c.Status), c.ID, c.Name)
		} else {
			fmt.Fprintf(&sb, "  [%s] %-6s %s — %s\n", statusBadge(c.Status), c.ID, c.Name, c.Detail)
		}
		if c.Suggest != "" {
			fmt.Fprintf(&sb, "         suggest: %s\n", c.Suggest)
		}
	}
	ok, fail, warn, skip := Counts(checks)
	fmt.Fprintf(&sb, "\n%d ok, %d fail, %d warn, %d skip\n", ok, fail, warn, skip)
	return sb.String()
}

// JSON returns the JSON encoding of the check set.
func JSON(checks []Check) ([]byte, error) {
	return json.MarshalIndent(checks, "", "  ")
}

func statusBadge(s Status) string {
	switch s {
	case StatusOK:
		return " OK "
	case StatusFail:
		return "FAIL"
	case StatusWarn:
		return "WARN"
	case StatusSkip:
		return "SKIP"
	default:
		return string(s)
	}
}
