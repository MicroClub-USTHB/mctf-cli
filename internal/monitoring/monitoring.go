// Package monitoring renders and runs the MCTF observability stack:
// Prometheus + Grafana + cAdvisor, with scrape config derived from the
// event's instanced challenges and a prebuilt container-overview dashboard.
//
// The rendered stack lives under <eventRoot>/monitoring/stack/ so it never
// collides with the operator-facing monitoring/prometheus.yml that
// `mctf generate` emits.
package monitoring

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// StackDir is the stack location relative to the event root.
var StackDir = filepath.Join("monitoring", "stack")

// cadvisorInternalPort is cAdvisor's fixed in-container port.
const cadvisorInternalPort = 8080

// Options are the tunable knobs for the stack, resolved from config
// defaults plus any overrides under the mctf.config.yml `monitoring` block.
type Options struct {
	PrometheusPort  int    // host port for the Prometheus UI (default 9090)
	GrafanaPort     int    // host port for the Grafana UI (default 3000)
	CadvisorPort    int    // host port for the cAdvisor UI (default 8081)
	GrafanaPassword string // Grafana admin password (default "admin")
	Retention       string // Prometheus TSDB retention (default "15d")
}

// DefaultOptions returns the stack defaults used when nothing is overridden.
func DefaultOptions() Options {
	return Options{
		PrometheusPort:  9090,
		GrafanaPort:     3000,
		CadvisorPort:    8081,
		GrafanaPassword: "admin",
		Retention:       "15d",
	}
}

// OptionsFromConfig layers any overrides from cfg.Monitoring over the
// defaults. The monitoring block is a free-form object in the schema, so
// recognized keys are read defensively and anything unknown is ignored.
//
// Recognized keys: prometheus_port, grafana_port, cadvisor_port (ints),
// grafana_password, retention (strings).
func OptionsFromConfig(cfg *config.Config) Options {
	o := DefaultOptions()
	if cfg == nil || cfg.Monitoring == nil {
		return o
	}
	m := cfg.Monitoring
	if v, ok := intValue(m["prometheus_port"]); ok {
		o.PrometheusPort = v
	}
	if v, ok := intValue(m["grafana_port"]); ok {
		o.GrafanaPort = v
	}
	if v, ok := intValue(m["cadvisor_port"]); ok {
		o.CadvisorPort = v
	}
	if s, ok := m["grafana_password"].(string); ok && s != "" {
		o.GrafanaPassword = s
	}
	if s, ok := m["retention"].(string); ok && s != "" {
		o.Retention = s
	}
	return o
}

// intValue coerces a decoded YAML/JSON scalar to an int. YAML-through-JSON
// decoding yields float64 for numbers, but tests and hand-built maps may
// carry int, so both are accepted.
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// Stack is the rendered set of files for the monitoring stack, keyed by
// absolute path, plus the compose project directory they live in.
type Stack struct {
	Dir   string
	Files map[string][]byte
}

// Plan renders the full stack for the event without touching disk.
func Plan(cfg *config.Config, eventRoot string, challs []*challenge.Challenge) (*Stack, error) {
	opts := OptionsFromConfig(cfg)
	dir := filepath.Join(eventRoot, StackDir)

	prom, err := PrometheusConfig(challs)
	if err != nil {
		return nil, fmt.Errorf("prometheus: %w", err)
	}

	files := map[string][]byte{
		filepath.Join(dir, "docker-compose.yml"):                                       ComposeConfig(opts),
		filepath.Join(dir, "prometheus.yml"):                                           prom,
		filepath.Join(dir, "grafana", "provisioning", "datasources", "datasource.yml"): GrafanaDatasource(opts),
		filepath.Join(dir, "grafana", "provisioning", "dashboards", "dashboards.yml"):  GrafanaDashboardProvider(),
		filepath.Join(dir, "grafana", "dashboards", "mctf-overview.json"):              OverviewDashboard(),
	}

	return &Stack{Dir: dir, Files: files}, nil
}

// SortedPaths returns the planned file paths in deterministic order,
// without touching disk (used by dry-run output).
func (s *Stack) SortedPaths() []string {
	paths := make([]string, 0, len(s.Files))
	for p := range s.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// Write writes every planned file, creating parent directories as needed,
// and returns the written paths in sorted order.
func (s *Stack) Write() ([]string, error) {
	written := make([]string, 0, len(s.Files))
	for path, body := range s.Files {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, body, 0644); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, path)
	}
	sort.Strings(written)
	return written, nil
}
