// Package status reports the runtime state of an event's deployable
// challenges by interrogating `docker compose ps --format json`.
//
// Static challenges report state="static" (no runtime). Instanced
// challenges report state="instanced" (per-team, instancer-managed).
// Service-archetype challenges report the actual compose-project
// state: running / exited / restarting / missing.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/deploy"
)

// Row is one challenge's runtime status.
type Row struct {
	Challenge string `json:"challenge"`
	Archetype string `json:"archetype"`
	Project   string `json:"project,omitempty"`
	State     string `json:"state"`
	Container string `json:"container,omitempty"`
	Image     string `json:"image,omitempty"`
	Uptime    string `json:"uptime,omitempty"`
	URL       string `json:"url,omitempty"`
}

// Inspector resolves the runtime state of a single Unit.
//
// The default implementation calls `docker compose ps`. Tests can
// substitute a fake; we expose Inspector so the package boundary holds.
type Inspector interface {
	Inspect(ctx context.Context, project, composeFile string) ([]composeContainer, error)
}

// composeContainer mirrors the relevant subset of `docker compose ps
// --format json` output. CTFd compose v2 emits one JSON object per
// container, separated by newlines (not a JSON array).
type composeContainer struct {
	Name    string `json:"Name"`
	State   string `json:"State"` // running, exited, ...
	Image   string `json:"Image"`
	Status  string `json:"Status"` // "Up 5 minutes" / "Exited (0) 2 hours ago"
	Service string `json:"Service"`
}

// DockerInspector shells out to docker compose. Pass nil HTTP client
// (we don't use one); kept on the struct for future config knobs.
type DockerInspector struct{}

// Inspect runs `docker compose -p <project> -f <composeFile> ps --format json`
// and decodes one container per line.
func (DockerInspector) Inspect(ctx context.Context, project, composeFile string) ([]composeContainer, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not in $PATH")
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", project, "-f", composeFile, "ps", "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		// `docker compose ps` for a missing project returns success +
		// empty output, so any error here is a real one (daemon down,
		// compose missing, etc.). Surface it.
		var exitErr *exec.ExitError
		if eaErr := err; eaErr != nil {
			if e, ok := eaErr.(*exec.ExitError); ok {
				exitErr = e
				return nil, fmt.Errorf("docker compose ps: %s", strings.TrimSpace(string(exitErr.Stderr)))
			}
		}
		return nil, err
	}
	return parseComposeJSON(out)
}

// parseComposeJSON handles both compose v2 line-delimited output and
// (for safety) the older array-shaped format.
func parseComposeJSON(out []byte) ([]composeContainer, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var arr []composeContainer
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
			return nil, fmt.Errorf("decode compose ps: %w", err)
		}
		return arr, nil
	}
	var out2 []composeContainer
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c composeContainer
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("decode compose ps line: %w", err)
		}
		out2 = append(out2, c)
	}
	return out2, nil
}

// Run resolves the status of every challenge.
//
// inspector may be nil; defaults to DockerInspector.
func Run(ctx context.Context, cfg *config.Config, challs []*challenge.Challenge, inspector Inspector) ([]Row, error) {
	if inspector == nil {
		inspector = DockerInspector{}
	}
	var rows []Row
	for _, c := range challs {
		row := Row{
			Challenge: c.CategoryDir + "/" + c.Slug,
			Archetype: string(c.Archetype),
		}
		switch c.Archetype {
		case challenge.ArchetypeStatic:
			row.State = "static"
		case challenge.ArchetypeInstanced:
			row.State = "instanced"
			row.URL = instancerURL(cfg)
		case challenge.ArchetypeService:
			row.Project = deploy.ProjectName(cfg, c)
			row.URL = serviceURL(cfg, c)
			containers, err := inspector.Inspect(ctx, row.Project, c.ChallengeDir+"/docker-compose.yml")
			if err != nil {
				row.State = "error: " + err.Error()
			} else if len(containers) == 0 {
				row.State = "not deployed"
			} else {
				cn := containers[0]
				row.State = cn.State
				row.Container = cn.Name
				row.Image = cn.Image
				row.Uptime = parseUptime(cn.Status)
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Challenge < rows[j].Challenge })
	return rows, nil
}

// parseUptime extracts a friendly uptime from a compose status line
// like "Up 5 minutes" or "Up About a minute (healthy)".
func parseUptime(status string) string {
	if !strings.HasPrefix(status, "Up ") {
		return ""
	}
	body := strings.TrimPrefix(status, "Up ")
	if i := strings.Index(body, " ("); i >= 0 {
		return body[:i]
	}
	return body
}

func instancerURL(cfg *config.Config) string {
	if cfg.Instancer != nil && cfg.Instancer.URL != "" {
		return cfg.Instancer.URL
	}
	if cfg.Event.Domain != "" {
		return "https://instancer." + cfg.Event.Domain
	}
	return ""
}

func serviceURL(cfg *config.Config, c *challenge.Challenge) string {
	if c.Manifest.Deploy == nil || cfg.Event.Domain == "" {
		return ""
	}
	d := c.Manifest.Deploy
	if d.Subdomain == "" {
		return ""
	}
	scheme := "https"
	if d.NoSSL {
		scheme = "http"
	}
	if d.TCP {
		if d.Port != nil {
			return fmt.Sprintf("nc %s.%s %d", d.Subdomain, cfg.Event.Domain, *d.Port)
		}
		return ""
	}
	if d.Port != nil {
		return fmt.Sprintf("%s://%s.%s:%d", scheme, d.Subdomain, cfg.Event.Domain, *d.Port)
	}
	return fmt.Sprintf("%s://%s.%s", scheme, d.Subdomain, cfg.Event.Domain)
}

// Render produces the human-readable table.
func Render(rows []Row) string {
	var sb strings.Builder
	const fmtLine = "  %-22s  %-10s  %-32s  %-10s  %s\n"
	fmt.Fprintf(&sb, fmtLine, "CHALLENGE", "ARCHETYPE", "PROJECT/STATE", "UPTIME", "URL")
	for _, r := range rows {
		state := r.State
		if r.Project != "" {
			state = r.Project + " (" + r.State + ")"
		}
		fmt.Fprintf(&sb, fmtLine, r.Challenge, r.Archetype, truncate(state, 32), r.Uptime, r.URL)
	}
	var nUp, nDown int
	for _, r := range rows {
		if r.Archetype != "service" {
			continue
		}
		if r.State == "running" {
			nUp++
		} else {
			nDown++
		}
	}
	fmt.Fprintf(&sb, "\n%d service(s): %d running, %d not running\n", nUp+nDown, nUp, nDown)
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// JSON marshals the rows.
func JSON(rows []Row) ([]byte, error) {
	return json.MarshalIndent(rows, "", "  ")
}
