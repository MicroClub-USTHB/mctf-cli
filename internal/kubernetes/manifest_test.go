package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func cfgK8s(ns, ingress string) *config.Config {
	return &config.Config{
		Version:  1,
		Event:    config.Event{Slug: "evt", Domain: "ctf.example.com"},
		Registry: &config.Registry{URL: "registry.example.com"},
		Deploy: &config.Deploy{
			Mode: "kubernetes",
			Kubernetes: &config.KubernetesDeploy{
				Namespace:    ns,
				IngressClass: ingress,
			},
		},
	}
}

func newServiceChall(t *testing.T, root, cat, slug string, port, internal int, subdomain string) *challenge.Challenge {
	t.Helper()
	chDir := filepath.Join(root, cat, slug, "challenge")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := port
	ip := internal
	return &challenge.Challenge{
		Slug: slug, CategoryDir: cat,
		ChallengeDir: chDir,
		Archetype:    challenge.ArchetypeService,
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: slug, Category: cat, NeedDeploy: true,
			},
			Deploy: &challenge.DeployBlock{
				Subdomain:    subdomain,
				Port:         &p,
				InternalPort: &ip,
			},
		},
	}
}

func TestPlan_RequiresKubernetesMode(t *testing.T) {
	cfg := &config.Config{Deploy: &config.Deploy{Mode: "docker"}}
	_, err := Plan(cfg, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "kubernetes") {
		t.Errorf("expected kubernetes-mode error, got %v", err)
	}
}

func TestPlan_SkipsStaticAndInstanced(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		{Slug: "static", CategoryDir: "crypto", Archetype: challenge.ArchetypeStatic,
			Manifest: challenge.Manifest{Challenge: challenge.ChallengeBlock{Category: "crypto"}}},
		newServiceChall(t, root, "misc", "service-one", 14000, 8000, "misc"),
		{Slug: "inst", CategoryDir: "web", Archetype: challenge.ArchetypeInstanced,
			Manifest: challenge.Manifest{Challenge: challenge.ChallengeBlock{NeedDeploy: true},
				Deploy: &challenge.DeployBlock{DynamicInstances: true}}},
	}
	plan, err := Plan(cfgK8s("ctf", "nginx"), challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].Challenge != "misc/service-one" {
		t.Errorf("expected only service challenge, got %+v", plan)
	}
}

func TestPlan_NameAndNamespace(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{newServiceChall(t, root, "misc", "welcome", 14000, 8000, "welcome")}
	plan, err := Plan(cfgK8s("ctf", "nginx"), challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Name != "mctf-evt-misc-welcome" {
		t.Errorf("name = %q", plan[0].Name)
	}
	if plan[0].Namespace != "ctf" {
		t.Errorf("namespace = %q", plan[0].Namespace)
	}
}

func TestPlan_DefaultNamespace(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{newServiceChall(t, root, "misc", "x", 14000, 8000, "x")}
	cfg := cfgK8s("", "")
	cfg.Deploy.Kubernetes = nil
	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Namespace != "default" {
		t.Errorf("namespace = %q, want default", plan[0].Namespace)
	}
}

func TestPlan_ImageFromRegistry(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{newServiceChall(t, root, "misc", "welcome", 14000, 8000, "welcome")}
	plan, err := Plan(cfgK8s("ctf", "nginx"), challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := "registry.example.com/evt-misc-welcome:latest"
	if plan[0].Image != want {
		t.Errorf("image = %q, want %q", plan[0].Image, want)
	}
}

func TestPlan_ImageFromCompose(t *testing.T) {
	root := t.TempDir()
	c := newServiceChall(t, root, "misc", "welcome", 14000, 8000, "welcome")
	compose := `services:
  app:
    image: ghcr.io/example/welcome:v1
    ports:
      - "14000:8000"
`
	if err := os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(cfgK8s("ctf", "nginx"), []*challenge.Challenge{c}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Image != "ghcr.io/example/welcome:v1" {
		t.Errorf("image = %q", plan[0].Image)
	}
}

func TestPlan_MissingInternalPort(t *testing.T) {
	root := t.TempDir()
	chDir := filepath.Join(root, "misc", "x", "challenge")
	os.MkdirAll(chDir, 0o755)
	c := &challenge.Challenge{
		Slug: "x", CategoryDir: "misc", ChallengeDir: chDir,
		Archetype: challenge.ArchetypeService,
		Manifest: challenge.Manifest{
			Challenge: challenge.ChallengeBlock{NeedDeploy: true},
			Deploy:    &challenge.DeployBlock{Subdomain: "x"},
		},
	}
	_, err := Plan(cfgK8s("ctf", "nginx"), []*challenge.Challenge{c}, Options{})
	if err == nil || !strings.Contains(err.Error(), "internal_port") {
		t.Errorf("expected internal_port error, got %v", err)
	}
}

func TestManifestsYAML_HTTPHasThreeDocs(t *testing.T) {
	u := KubeUnit{
		Challenge: "misc/welcome", Name: "mctf-evt-misc-welcome",
		Namespace: "ctf", Image: "registry.example.com/evt-misc-welcome:latest",
		InternalPort: 8000, Subdomain: "welcome", Domain: "ctf.example.com",
		IngressClass: "nginx",
	}
	raw, err := ManifestsYAML(u)
	if err != nil {
		t.Fatal(err)
	}
	docs := strings.Count(string(raw), "\n---\n") + strings.Count(string(raw), "---\n")
	// 3 documents = Deployment + Service + Ingress, each starting with "---\n"
	if docs < 3 {
		t.Errorf("expected >=3 --- markers, got %d\nYAML:\n%s", docs, raw)
	}
	s := string(raw)
	for _, want := range []string{"Deployment", "Service", "Ingress", "mctf-evt-misc-welcome", "welcome.ctf.example.com"} {
		if !strings.Contains(s, want) {
			t.Errorf("manifests missing %q", want)
		}
	}
}

func TestManifestsYAML_TCPNoIngress(t *testing.T) {
	u := KubeUnit{
		Challenge: "misc/nc", Name: "mctf-evt-misc-nc",
		Namespace: "ctf", Image: "registry.example.com/evt-misc-nc:latest",
		InternalPort: 4444, NodePort: 31337, Subdomain: "nc", Domain: "ctf.example.com",
		TCP: true,
	}
	raw, err := ManifestsYAML(u)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "Ingress") {
		t.Errorf("TCP challenge should not produce an Ingress, got:\n%s", s)
	}
	if !strings.Contains(s, "NodePort") {
		t.Errorf("TCP challenge Service should be NodePort, got:\n%s", s)
	}
	if !strings.Contains(s, "nodePort: 31337") {
		t.Errorf("TCP challenge Service should have fixed nodePort: 31337, got:\n%s", s)
	}
}

func TestManifestsYAML_NoSSLSkipsTLS(t *testing.T) {
	u := KubeUnit{
		Challenge: "misc/plain", Name: "mctf-evt-misc-plain",
		Namespace: "ctf", Image: "registry.example.com/evt-misc-plain:latest",
		InternalPort: 8080, Subdomain: "plain", Domain: "ctf.example.com",
		NoSSL: true,
	}
	raw, err := ManifestsYAML(u)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tls:") {
		t.Errorf("noSSL=true should not produce tls: block, got:\n%s", raw)
	}
}

func TestManifestsYAML_ResourceLimits(t *testing.T) {
	u := KubeUnit{
		Challenge: "misc/heavy", Name: "mctf-evt-misc-heavy",
		Namespace: "ctf", Image: "img:latest",
		InternalPort: 8000, Subdomain: "heavy", Domain: "ctf.example.com",
		Resources: &challenge.Resources{CPU: "500m", Memory: "128Mi"},
	}
	raw, err := ManifestsYAML(u)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "cpu: 500m") && !strings.Contains(s, "cpu: \"500m\"") {
		t.Errorf("cpu requests missing from:\n%s", s)
	}
	if !strings.Contains(s, "cpu: 1000m") && !strings.Contains(s, "cpu: \"1000m\"") {
		t.Errorf("cpu limits (2x burstable) missing from:\n%s", s)
	}
	if !strings.Contains(s, "128Mi") {
		t.Errorf("memory limits missing from:\n%s", s)
	}
}

func TestManifestsYAML_ProbeInitialDelay(t *testing.T) {
	u := KubeUnit{
		Challenge: "web/probe", Name: "mctf-evt-web-probe",
		Namespace: "ctf", Image: "img:latest",
		InternalPort: 8080, Subdomain: "probe", Domain: "ctf.example.com",
		Healthcheck: &challenge.Healthcheck{
			Type:     "http",
			Path:     "/healthz",
			Interval: "15s",
			Timeout:  "3s",
			Retries:  3,
		},
	}
	raw, err := ManifestsYAML(u)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "initialDelaySeconds: 15") {
		t.Errorf("probe missing initialDelaySeconds: 15, got:\n%s", s)
	}
}

func TestApplyUp_Order(t *testing.T) {
	var order []string
	r := &recordApplier{up: func(u KubeUnit) error { order = append(order, u.Challenge); return nil }}
	plan := []KubeUnit{{Challenge: "a"}, {Challenge: "b"}, {Challenge: "c"}}
	if err := ApplyUp(context.Background(), r, plan); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "a,b,c" {
		t.Errorf("up order = %v", order)
	}
}

func TestApplyDown_ReverseOrder(t *testing.T) {
	var order []string
	r := &recordApplier{down: func(u KubeUnit) error { order = append(order, u.Challenge); return nil }}
	plan := []KubeUnit{{Challenge: "a"}, {Challenge: "b"}, {Challenge: "c"}}
	if err := ApplyDown(context.Background(), r, plan); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "c,b,a" {
		t.Errorf("down order = %v", order)
	}
}

type recordApplier struct {
	up   func(KubeUnit) error
	down func(KubeUnit) error
}

func (r *recordApplier) Up(_ context.Context, u KubeUnit) error   { return r.up(u) }
func (r *recordApplier) Down(_ context.Context, u KubeUnit) error { return r.down(u) }
