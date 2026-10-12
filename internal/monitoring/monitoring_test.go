package monitoring

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func instancedChall(slug string, port int) *challenge.Challenge {
	p := port
	return &challenge.Challenge{
		Slug:      slug,
		Archetype: challenge.ArchetypeInstanced,
		Manifest:  challenge.Manifest{Deploy: &challenge.DeployBlock{InternalPort: &p}},
	}
}

func staticChall(slug string) *challenge.Challenge {
	return &challenge.Challenge{Slug: slug, Archetype: challenge.ArchetypeStatic}
}

func TestOptionsFromConfig_Defaults(t *testing.T) {
	got := OptionsFromConfig(nil)
	want := DefaultOptions()
	if got != want {
		t.Errorf("nil cfg: got %+v, want defaults %+v", got, want)
	}
	// Empty monitoring block also yields defaults.
	if got := OptionsFromConfig(&config.Config{}); got != want {
		t.Errorf("empty cfg: got %+v, want defaults %+v", got, want)
	}
}

func TestOptionsFromConfig_Overrides(t *testing.T) {
	cfg := &config.Config{Monitoring: map[string]any{
		"prometheus_port":  float64(19090), // float64: YAML-through-JSON path
		"grafana_port":     3001,           // int: hand-built map path
		"cadvisor_port":    int64(18080),   // int64 accepted too
		"grafana_password": "s3cr3t",
		"retention":        "30d",
		"unknown_key":      "ignored",
	}}
	got := OptionsFromConfig(cfg)
	if got.PrometheusPort != 19090 || got.GrafanaPort != 3001 || got.CadvisorPort != 18080 {
		t.Errorf("port overrides not applied: %+v", got)
	}
	if got.GrafanaPassword != "s3cr3t" || got.Retention != "30d" {
		t.Errorf("string overrides not applied: %+v", got)
	}
}

func TestOptionsFromConfig_IgnoresWrongTypes(t *testing.T) {
	cfg := &config.Config{Monitoring: map[string]any{
		"prometheus_port":  "nope", // wrong type: keep default
		"grafana_password": 123,    // wrong type: keep default
	}}
	got := OptionsFromConfig(cfg)
	def := DefaultOptions()
	if got.PrometheusPort != def.PrometheusPort {
		t.Errorf("bad prometheus_port should keep default, got %d", got.PrometheusPort)
	}
	if got.GrafanaPassword != def.GrafanaPassword {
		t.Errorf("bad grafana_password should keep default, got %q", got.GrafanaPassword)
	}
}

func TestPrometheusConfig_InfraAndChallengeJobs(t *testing.T) {
	challs := []*challenge.Challenge{
		instancedChall("z", 8003),
		staticChall("static"), // skipped
		instancedChall("a", 8001),
		{Slug: "noport", Archetype: challenge.ArchetypeInstanced}, // nil deploy, skipped
	}
	got := string(mustPrometheus(t, challs))

	// Infra jobs always present, on in-container ports.
	if !strings.Contains(got, `job_name: "prometheus"`) || !strings.Contains(got, "localhost:9090") {
		t.Error("missing prometheus self-scrape job on in-container port 9090")
	}
	if !strings.Contains(got, `job_name: "cadvisor"`) || !strings.Contains(got, "cadvisor:8080") {
		t.Error("missing cadvisor job/target")
	}
	// Global block present.
	if !strings.Contains(got, "global:") || !strings.Contains(got, "scrape_interval: 15s") {
		t.Error("missing global block")
	}
	// Challenge targets use the host gateway, not localhost.
	if !strings.Contains(got, "host.docker.internal:8001") || !strings.Contains(got, "host.docker.internal:8003") {
		t.Errorf("challenge targets should use host gateway:\n%s", got)
	}
	if strings.Contains(got, "localhost:8001") {
		t.Error("challenge target must not use localhost (unreachable from container)")
	}
	// Static and port-less challenges are skipped.
	if strings.Contains(got, `job_name: "static"`) || strings.Contains(got, `job_name: "noport"`) {
		t.Error("non-scrapable challenges should be skipped")
	}
}

func TestPrometheusConfig_SortedAndSelfPort(t *testing.T) {
	got := string(mustPrometheus(t, []*challenge.Challenge{
		instancedChall("z", 9), instancedChall("a", 8),
	}))
	// Self-scrape always targets the in-container port 9090, independent of
	// any host-port override (targets resolve inside the container).
	if !strings.Contains(got, "localhost:9090") {
		t.Error("prometheus self-scrape must target in-container port 9090")
	}
	// "a" job must appear before "z" job.
	if ai, zi := strings.Index(got, `job_name: "a"`), strings.Index(got, `job_name: "z"`); ai < 0 || zi < 0 || ai > zi {
		t.Errorf("challenge jobs not sorted (a=%d z=%d)", ai, zi)
	}
}

func TestComposeConfig_PinnedImagesAndWiring(t *testing.T) {
	got := string(ComposeConfig(Options{PrometheusPort: 9090, GrafanaPort: 3000, CadvisorPort: 8081, GrafanaPassword: "pw", Retention: "7d"}))
	for _, want := range []string{
		prometheusImage, grafanaImage, cadvisorImage,
		"host.docker.internal:host-gateway",
		`"9090:9090"`, `"3000:3000"`, `"8081:8080"`,
		"GF_SECURITY_ADMIN_PASSWORD=pw",
		"retention.time=7d",
		"prometheus-data:",
		"grafana-data:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("compose missing %q", want)
		}
	}
}

func TestPlanAndWrite(t *testing.T) {
	root := t.TempDir()
	stack, err := Plan(&config.Config{}, root, []*challenge.Challenge{instancedChall("a", 8001)})
	if err != nil {
		t.Fatal(err)
	}
	// Dir is under <root>/monitoring/stack.
	if stack.Dir != filepath.Join(root, "monitoring", "stack") {
		t.Errorf("unexpected stack dir %q", stack.Dir)
	}
	wantFiles := []string{
		filepath.Join(stack.Dir, "docker-compose.yml"),
		filepath.Join(stack.Dir, "prometheus.yml"),
		filepath.Join(stack.Dir, "grafana", "provisioning", "datasources", "datasource.yml"),
		filepath.Join(stack.Dir, "grafana", "provisioning", "dashboards", "dashboards.yml"),
		filepath.Join(stack.Dir, "grafana", "dashboards", "mctf-overview.json"),
	}
	for _, f := range wantFiles {
		if _, ok := stack.Files[f]; !ok {
			t.Errorf("plan missing file %q", f)
		}
	}

	written, err := stack.Write()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(wantFiles) {
		t.Errorf("wrote %d files, want %d", len(written), len(wantFiles))
	}
	for _, f := range wantFiles {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("file not written to disk: %q (%v)", f, err)
		}
	}
}

func TestGrafanaProvisioning(t *testing.T) {
	ds := string(GrafanaDatasource(DefaultOptions()))
	if !strings.Contains(ds, "http://prometheus:9090") {
		t.Error("datasource should target the in-stack prometheus service URL")
	}
	prov := string(GrafanaDashboardProvider())
	if !strings.Contains(prov, "/var/lib/grafana/dashboards") {
		t.Error("dashboard provider should point at the mounted dashboards dir")
	}
}

// The overview dashboard must reference the datasource by the exact uid the
// datasource provisioning file assigns — otherwise a provisioned (non-imported)
// dashboard renders empty panels against a dangling datasource reference.
func TestGrafanaDatasourceUIDMatchesDashboard(t *testing.T) {
	ds := string(GrafanaDatasource(DefaultOptions()))
	if !strings.Contains(ds, "uid: "+datasourceUID) {
		t.Fatalf("datasource missing fixed uid %q", datasourceUID)
	}
	dash := string(OverviewDashboard())
	if !strings.Contains(dash, `"uid": "`+datasourceUID+`"`) {
		t.Errorf("dashboard does not reference datasource uid %q", datasourceUID)
	}
	// And must not carry an unresolved import-time input variable.
	if strings.Contains(dash, "${DS_") {
		t.Error("dashboard still references an import-time datasource input var")
	}
}

// The rendered Prometheus config and compose file must be valid YAML, and
// the compose must declare the three stack services. Substring checks alone
// would not catch an indentation bug that breaks the YAML.
func TestRenderedYAMLIsValid(t *testing.T) {
	prom, err := PrometheusConfig([]*challenge.Challenge{instancedChall("a", 8001)})
	if err != nil {
		t.Fatal(err)
	}
	var promDoc struct {
		Global        map[string]any `json:"global"`
		ScrapeConfigs []struct {
			JobName string `json:"job_name"`
		} `json:"scrape_configs"`
	}
	if err := yaml.Unmarshal(prom, &promDoc); err != nil {
		t.Fatalf("prometheus.yml is not valid YAML: %v\n%s", err, prom)
	}
	// prometheus + cadvisor + 1 challenge = 3 jobs.
	if len(promDoc.ScrapeConfigs) != 3 {
		t.Errorf("expected 3 scrape jobs, got %d", len(promDoc.ScrapeConfigs))
	}
	if promDoc.Global["scrape_interval"] != "15s" {
		t.Errorf("global scrape_interval not parsed, got %v", promDoc.Global["scrape_interval"])
	}

	var composeDoc struct {
		Name     string                    `json:"name"`
		Services map[string]map[string]any `json:"services"`
		Volumes  map[string]any            `json:"volumes"`
	}
	if err := yaml.Unmarshal(ComposeConfig(DefaultOptions()), &composeDoc); err != nil {
		t.Fatalf("docker-compose.yml is not valid YAML: %v", err)
	}
	for _, svc := range []string{"prometheus", "cadvisor", "grafana"} {
		if _, ok := composeDoc.Services[svc]; !ok {
			t.Errorf("compose missing service %q", svc)
		}
	}
	if _, ok := composeDoc.Volumes["prometheus-data"]; !ok {
		t.Error("compose missing prometheus-data volume")
	}

	// Grafana provisioning files must be valid YAML too.
	for name, body := range map[string][]byte{
		"datasource": GrafanaDatasource(DefaultOptions()),
		"provider":   GrafanaDashboardProvider(),
	} {
		var doc map[string]any
		if err := yaml.Unmarshal(body, &doc); err != nil {
			t.Errorf("grafana %s yaml invalid: %v", name, err)
		}
	}
}

func TestOverviewDashboard_ValidJSON(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(OverviewDashboard(), &doc); err != nil {
		t.Fatalf("dashboard is not valid JSON: %v", err)
	}
	if doc["uid"] != "mctf-overview" {
		t.Errorf("unexpected dashboard uid %v", doc["uid"])
	}
	if _, ok := doc["panels"].([]any); !ok {
		t.Error("dashboard should have a panels array")
	}
}

// The panel queries must filter on the cgroup id (present on both native and
// Docker-Desktop cAdvisor) rather than the name label (empty on Docker
// Desktop), and surface a derived container legend. This locks the fix for
// the empty-panels regression.
func TestOverviewDashboard_QueriesAreCadvisorRobust(t *testing.T) {
	dash := string(OverviewDashboard())
	if strings.Contains(dash, `container_cpu_usage_seconds_total{name!=`) ||
		strings.Contains(dash, `container_memory_usage_bytes{name!=`) {
		t.Error("queries must not filter on the name label (empty on Docker Desktop cAdvisor)")
	}
	if !strings.Contains(dash, `id=~`) {
		t.Error("queries should filter on the cgroup id label")
	}
	if strings.Count(dash, `"legendFormat": "{{container}}"`) != 2 {
		t.Error("both panels should use the derived {{container}} legend")
	}
}

func mustPrometheus(t *testing.T, challs []*challenge.Challenge) []byte {
	t.Helper()
	b, err := PrometheusConfig(challs)
	if err != nil {
		t.Fatalf("PrometheusConfig: %v", err)
	}
	return b
}
