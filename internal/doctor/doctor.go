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
	"regexp"
	"sort"
	"strings"
	"time"

	"path/filepath"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/instancer"
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
		checks = append(checks, checkDockerStorageDriver())
		checks = append(checks, checkDockerCompose())
		checks = append(checks, checkGit())
		checks = append(checks, checkRegistryCreds())
		checks = append(checks, checkCTFdToken())
		if len(challs) > 0 {
			checks = append(checks, checkPortsFree(challs))
			checks = append(checks, checkDNSResolves(cfg, challs))
		}
		checks = append(checks, checkDiskSpace(dir))
	}

	checks = append(checks, checkExpiredInstances(dir))
	checks = append(checks, checkCommittedSecrets(dir))

	return checks
}

// secretAssignRE matches an assignment of an MCTF_*_{TOKEN,KEY,SECRET,WEBHOOK}
// to a value: `MCTF_FOO_TOKEN = value` or `MCTF_FOO_TOKEN: value`.
var secretAssignRE = regexp.MustCompile(`MCTF_[A-Z0-9_]*(?:TOKEN|KEY|SECRET|WEBHOOK)[[:blank:]]*[:=][[:blank:]]*(.+)`)

// secretPlaceholders mark a value as a non-secret sample, so an example or
// template config doesn't trip the check.
var secretPlaceholders = []string{
	"changeme", "change-me", "your", "example", "placeholder", "redacted",
	"dummy", "fixme", "todo", "replace", "xxxx", "<", ">", "...", "here",
}

// checkCommittedSecrets scans tracked files for an MCTF_* token/key/secret
// committed with a real-looking value. It warns (never fails) and never
// prints the value — only the location. Scope is the working tree (not a deep
// history scan — that is a job for a dedicated tool like gitleaks), and
// excludes examples, docs, and *_test.go fixtures where fake token values are
// expected.
func checkCommittedSecrets(dir string) Check {
	id, name := "D043", "committed secrets"

	if _, err := exec.LookPath("git"); err != nil {
		return Check{ID: id, Name: name, Status: StatusSkip, Detail: "git not in $PATH"}
	}
	if err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return Check{ID: id, Name: name, Status: StatusSkip, Detail: "not a git repository"}
	}

	cmd := exec.Command("git", "-C", dir, "grep", "-I", "-n", "-E",
		`MCTF_[A-Z0-9_]*(TOKEN|KEY|SECRET|WEBHOOK)[[:blank:]]*[:=]`,
		"--",
		".", ":(exclude)*.example", ":(exclude)*.md", ":(exclude)docs/", ":(exclude)*_test.go",
	)
	out, err := cmd.Output()
	if err != nil {
		// `git grep` exits 1 when there are no matches — the clean case.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return Check{ID: id, Name: name, Status: StatusOK, Detail: "no committed MCTF_* secret values found"}
		}
		return Check{ID: id, Name: name, Status: StatusSkip, Detail: "git grep failed: " + err.Error()}
	}

	var hits []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		// format: path:lineno:content
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		m := secretAssignRE.FindStringSubmatch(parts[2])
		if m == nil {
			continue
		}
		if looksLikeRealSecret(m[1]) {
			hits = append(hits, parts[0]+":"+parts[1])
		}
	}

	if len(hits) == 0 {
		return Check{ID: id, Name: name, Status: StatusOK, Detail: "no committed MCTF_* secret values found"}
	}
	sort.Strings(hits)
	return Check{
		ID: id, Name: name, Status: StatusWarn,
		Detail:  fmt.Sprintf("%d location(s) look like a committed secret: %s", len(hits), strings.Join(hits, ", ")),
		Suggest: "rotate the exposed secret and load it from the environment (MCTF_*) or a secret store — never commit token values",
	}
}

// looksLikeRealSecret heuristically decides whether an assigned value is a
// real secret rather than a placeholder or an env reference.
func looksLikeRealSecret(raw string) bool {
	v := strings.TrimSpace(raw)
	v = strings.Trim(v, `"'`)
	if i := strings.IndexByte(v, '#'); i >= 0 { // strip trailing comment
		v = strings.TrimSpace(v[:i])
		v = strings.Trim(v, `"'`)
	}
	if len(v) < 8 {
		return false
	}
	if strings.HasPrefix(v, "$") { // ${VAR} / $VAR reference, not a literal
		return false
	}
	low := strings.ToLower(v)
	for _, p := range secretPlaceholders {
		if strings.Contains(low, p) {
			return false
		}
	}
	return true
}

// checkExpiredInstances surfaces stale entries in .mctf/instances.json
// that the operator should reap with `mctf instance reap`.
func checkExpiredInstances(dir string) Check {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Check{ID: "D031", Name: "expired instances", Status: StatusSkip, Detail: err.Error()}
	}
	storePath := filepath.Join(abs, ".mctf", "instances.json")
	if _, err := os.Stat(storePath); err != nil {
		// Store doesn't exist yet — nothing to report.
		return Check{ID: "D031", Name: "expired instances", Status: StatusSkip, Detail: "no instance store"}
	}
	store, err := instancer.NewStore(storePath)
	if err != nil {
		return Check{ID: "D031", Name: "expired instances", Status: StatusFail, Detail: err.Error()}
	}
	expired := store.Expired(time.Now().UTC())
	if len(expired) == 0 {
		return Check{ID: "D031", Name: "expired instances", Status: StatusOK,
			Detail: fmt.Sprintf("%d live, 0 expired", len(store.List()))}
	}
	return Check{
		ID: "D031", Name: "expired instances",
		Status:  StatusWarn,
		Detail:  fmt.Sprintf("%d expired instance(s) need reaping", len(expired)),
		Suggest: "run `mctf instance reap`",
	}
}

func checkConfig(dir string) (*config.Config, Check) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, Check{
			ID: "D001", Name: "event config",
			Status: StatusFail, Detail: err.Error(),
			Suggest: "run `mctf init` to scaffold a new event, or fix mctf.config.yml",
		}
	}
	return cfg, Check{
		ID: "D001", Name: "event config",
		Status: StatusOK, Detail: "mctf.config.yml validates against schema v1",
	}
}

func checkDiscover(dir string, cfg *config.Config) ([]*challenge.Challenge, map[string]error, Check) {
	challs, loadErrs, err := challenge.Discover(dir, cfg.ChallengesDir)
	if err != nil {
		return nil, nil, Check{
			ID: "D002", Name: "challenges directory",
			Status: StatusFail, Detail: err.Error(),
			Suggest: "create the directory or fix challenges_dir in mctf.config.yml",
		}
	}
	if len(challs) == 0 {
		return challs, loadErrs, Check{
			ID: "D002", Name: "challenges directory",
			Status: StatusWarn, Detail: "no challenges discovered",
			Suggest: "scaffold one with `mctf init challenge <category>/<slug>`",
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
			Status:  StatusFail,
			Detail:  fmt.Sprintf("%d error(s), %d warning(s)", e, w),
			Suggest: "run `mctf validate` for details",
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
			Suggest: "install Docker — required for `mctf build` and `mctf deploy`",
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

// checkDockerStorageDriverFn shells out to `docker info`.
// Overridable in tests.
var checkDockerStorageDriverFn = checkDockerStorageDriverViaDocker

func checkDockerStorageDriverViaDocker() Check {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.Driver}}").Output()
	driver := strings.TrimSpace(string(out))
	if err != nil || driver == "" {
		return Check{ID: "D042", Name: "docker storage driver", Status: StatusSkip, Detail: "docker daemon not reachable"}
	}
	if driver == "vfs" || driver == "devicemapper" {
		return Check{
			ID: "D042", Name: "docker storage driver",
			Status: StatusWarn, Detail: "using slow/legacy storage driver: " + driver,
			Suggest: "reconfigure docker to use overlay2, zfs, or btrfs",
		}
	}
	return Check{ID: "D042", Name: "docker storage driver", Status: StatusOK, Detail: driver}
}

func checkDockerStorageDriver() Check {
	return checkDockerStorageDriverFn()
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
	user := os.Getenv("MCTF_REGISTRY_USER")
	token := os.Getenv("MCTF_REGISTRY_TOKEN")
	if user != "" && token != "" {
		return Check{
			ID: "D020", Name: "registry credentials",
			Status: StatusOK, Detail: "MCTF_REGISTRY_USER and MCTF_REGISTRY_TOKEN set",
		}
	}
	return Check{
		ID: "D020", Name: "registry credentials",
		Status: StatusWarn, Detail: "MCTF_REGISTRY_USER / MCTF_REGISTRY_TOKEN not set",
		Suggest: "required for `mctf build --push`",
	}
}

func checkCTFdToken() Check {
	if os.Getenv("MCTF_CTFD_TOKEN") != "" {
		return Check{ID: "D021", Name: "CTFd token", Status: StatusOK}
	}
	return Check{
		ID: "D021", Name: "CTFd token",
		Status: StatusWarn, Detail: "MCTF_CTFD_TOKEN not set",
		Suggest: "required for `mctf deploy` (CTFd registration)",
	}
}

// lookupIPFn resolves hostnames. Overridable in tests.
var lookupIPFn = net.LookupIP

func checkDNSResolves(cfg *config.Config, challs []*challenge.Challenge) Check {
	if cfg == nil || cfg.Event.Domain == "" {
		return Check{
			ID: "D040", Name: "dns resolution",
			Status: StatusSkip, Detail: "event.domain not configured",
		}
	}
	var failed []string
	checked := 0
	for _, c := range challs {
		if !c.Manifest.Challenge.NeedDeploy || c.Manifest.Deploy == nil || c.Manifest.Deploy.Subdomain == "" {
			continue
		}
		fqdn := fmt.Sprintf("%s.%s", c.Manifest.Deploy.Subdomain, cfg.Event.Domain)
		checked++
		ips, err := lookupIPFn(fqdn)
		if err != nil || len(ips) == 0 {
			failed = append(failed, fqdn)
		}
	}
	if checked == 0 {
		return Check{
			ID: "D040", Name: "dns resolution",
			Status: StatusSkip, Detail: "no challenges with subdomains",
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return Check{
			ID: "D040", Name: "dns resolution",
			Status: StatusWarn, Detail: fmt.Sprintf("failed to resolve: %s", strings.Join(failed, ", ")),
			Suggest: "check your DNS zone configuration",
		}
	}
	return Check{
		ID: "D040", Name: "dns resolution",
		Status: StatusOK, Detail: fmt.Sprintf("all %d subdomains resolve", checked),
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
		Status:  StatusWarn,
		Detail:  fmt.Sprintf("port(s) already in use: %s", strings.Join(busy, ", ")),
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
