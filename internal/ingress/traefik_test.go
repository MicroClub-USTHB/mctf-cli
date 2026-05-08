package ingress

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func mkChallenge(t *testing.T, root, cat, slug string, arch challenge.Archetype, internal int, sub string, tcp, noSSL bool, compose string) *challenge.Challenge {
	t.Helper()
	chRoot := filepath.Join(root, cat, slug)
	chDir := filepath.Join(chRoot, "challenge")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if compose != "" {
		_ = os.WriteFile(filepath.Join(chDir, "docker-compose.yml"), []byte(compose), 0o644)
	}
	d := &challenge.DeployBlock{Subdomain: sub, TCP: tcp, NoSSL: noSSL}
	if internal > 0 {
		ip := internal
		d.InternalPort = &ip
	}
	if arch == challenge.ArchetypeInstanced {
		d.DynamicInstances = true
	}
	return &challenge.Challenge{
		Slug: slug, CategoryDir: cat, RootDir: chRoot, ChallengeDir: chDir,
		Manifest: challenge.Manifest{
			Schema:    1,
			Challenge: challenge.ChallengeBlock{Name: slug, Category: cat, NeedDeploy: true},
			Deploy:    d,
		},
		Archetype: arch,
	}
}

func sampleCfg() *config.Config {
	return &config.Config{
		Version: 1,
		Event:   config.Event{Slug: "evt", Domain: "ctf.example.com"},
		Deploy:  &config.Deploy{Mode: "docker", Docker: &config.DockerDeploy{Network: "evt-net", Ingress: "traefik"}},
	}
}

const composeApp = `services:
  app:
    build:
      context: .
`

func TestPlanEntries_SkipsInstanced(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		mkChallenge(t, root, "web", "i", challenge.ArchetypeInstanced, 8000, "iweb", false, false, composeApp),
		mkChallenge(t, root, "misc", "s", challenge.ArchetypeService, 8000, "sweb", false, false, composeApp),
	}
	got, err := PlanEntries(sampleCfg(), challs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Challenge != "misc/s" {
		t.Errorf("plan = %+v", got)
	}
}

func TestPlanEntries_HTTPEntryShape(t *testing.T) {
	root := t.TempDir()
	c := mkChallenge(t, root, "misc", "welcome", challenge.ArchetypeService, 8000, "welcome", false, false, composeApp)
	got, err := PlanEntries(sampleCfg(), []*challenge.Challenge{c})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	e := got[0]
	if e.Name != "mtcf-evt-misc-welcome" {
		t.Errorf("name = %q", e.Name)
	}
	if e.HostRule != "Host(`welcome.ctf.example.com`)" {
		t.Errorf("host rule = %q", e.HostRule)
	}
	if e.Backend != "http://mtcf-evt-misc-welcome-app-1:8000" {
		t.Errorf("backend = %q", e.Backend)
	}
	if e.EntryPt != "websecure" {
		t.Errorf("entrypoint = %q", e.EntryPt)
	}
}

func TestPlanEntries_TCPEntryShape(t *testing.T) {
	root := t.TempDir()
	c := mkChallenge(t, root, "pwn", "baby", challenge.ArchetypeService, 5000, "baby", true, false, composeApp)
	got, err := PlanEntries(sampleCfg(), []*challenge.Challenge{c})
	if err != nil {
		t.Fatal(err)
	}
	e := got[0]
	if !strings.HasPrefix(e.HostRule, "HostSNI(") {
		t.Errorf("tcp router should use HostSNI: %q", e.HostRule)
	}
	if e.Backend != "mtcf-evt-pwn-baby-app-1:5000" {
		t.Errorf("backend = %q (no scheme for TCP)", e.Backend)
	}
	if e.EntryPt != "tcp" {
		t.Errorf("entrypoint = %q", e.EntryPt)
	}
}

func TestPlanEntries_RequiresInternalPort(t *testing.T) {
	root := t.TempDir()
	c := mkChallenge(t, root, "misc", "x", challenge.ArchetypeService, 0, "x", false, false, composeApp)
	_, err := PlanEntries(sampleCfg(), []*challenge.Challenge{c})
	if err == nil || !strings.Contains(err.Error(), "internal_port") {
		t.Errorf("expected internal_port error, got %v", err)
	}
}

func TestPlanEntries_RequiresSubdomain(t *testing.T) {
	root := t.TempDir()
	c := mkChallenge(t, root, "misc", "x", challenge.ArchetypeService, 8000, "", false, false, composeApp)
	_, err := PlanEntries(sampleCfg(), []*challenge.Challenge{c})
	if err == nil || !strings.Contains(err.Error(), "subdomain") {
		t.Errorf("expected subdomain error, got %v", err)
	}
}

func TestTraefikDynamic_StableOrder(t *testing.T) {
	// Three entries in arbitrary input order — output should be sorted
	// by router name and byte-deterministic.
	entries := []Entry{
		{Name: "z", HostRule: "Host(`z`)", Backend: "http://z:1", EntryPt: "websecure"},
		{Name: "a", HostRule: "Host(`a`)", Backend: "http://a:1", EntryPt: "websecure"},
		{Name: "m", HostRule: "Host(`m`)", Backend: "http://m:1", EntryPt: "websecure"},
	}
	a := TraefikDynamic(entries)
	b := TraefikDynamic(entries)
	if string(a) != string(b) {
		t.Errorf("non-deterministic output")
	}
	out := string(a)
	pa := strings.Index(out, "    a:")
	pm := strings.Index(out, "    m:")
	pz := strings.Index(out, "    z:")
	if pa >= pm || pm >= pz {
		t.Errorf("not sorted: a=%d m=%d z=%d\n%s", pa, pm, pz, out)
	}
}

func TestTraefikDynamic_HTTPAndTCPSplit(t *testing.T) {
	entries := []Entry{
		{Name: "h", HostRule: "Host(`h`)", Backend: "http://h:1", EntryPt: "websecure"},
		{Name: "t", HostRule: "HostSNI(`t`)", Backend: "t:1", EntryPt: "tcp", TCP: true},
	}
	out := string(TraefikDynamic(entries))
	httpAt := strings.Index(out, "http:")
	tcpAt := strings.Index(out, "tcp:")
	if httpAt < 0 || tcpAt < 0 {
		t.Fatalf("missing sections:\n%s", out)
	}
	if httpAt >= tcpAt {
		t.Errorf("http should come before tcp")
	}
}

func TestTraefikStatic_HasEntrypoints(t *testing.T) {
	out := string(TraefikStatic(sampleCfg()))
	for _, want := range []string{"web:", "websecure:", "tcp:", "providers:", "file:"} {
		if !strings.Contains(out, want) {
			t.Errorf("static missing %q\n%s", want, out)
		}
	}
}

func TestComposeServiceName_PicksFirstAlpha(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "docker-compose.yml")
	_ = os.WriteFile(p, []byte(`services:
  zeta: { image: x }
  alpha: { image: x }
`), 0o644)
	got, err := composeServiceName(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "alpha" {
		t.Errorf("want alpha, got %q", got)
	}
}

func TestComposeServiceName_HonorsContainerName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "docker-compose.yml")
	_ = os.WriteFile(p, []byte(`services:
  app:
    image: x
    container_name: my-pinned-app
`), 0o644)
	got, err := composeServiceName(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "my-pinned-app" {
		t.Errorf("want pinned name, got %q", got)
	}
}
