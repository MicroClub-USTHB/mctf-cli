package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// writeFile is a test helper that creates parent dirs and writes content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupEvent builds a realistic event directory with config + challenges and
// returns the loaded config and discovered challenges.
func setupEvent(t *testing.T, cfgYAML string, challs map[string]string) (*config.Config, []*challenge.Challenge) {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "mtcf.config.yml"), cfgYAML)

	for relPath, content := range challs {
		writeFile(t, filepath.Join(root, "challenges", relPath, "challenge", "challenge.yml"), content)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	discovered, loadErrs, err := challenge.Discover(root, cfg.ChallengesDir)
	if err != nil {
		t.Fatalf("challenge.Discover: %v", err)
	}
	for p, e := range loadErrs {
		t.Fatalf("challenge load error %s: %v", p, e)
	}
	return cfg, discovered
}

const k8sConfigYAML = `version: 1
event:
  name: "E2E Test"
  slug: "e2e-test"
  domain: "ctf.example.com"
registry:
  url: "ghcr.io/testing"
deploy:
  mode: kubernetes
  kubernetes:
    kubeconfig: "/tmp/kube.conf"
    namespace: "ctf-ns"
    ingress_class: "nginx"
`

const httpChallengeYAML = `schema: 1
challenge:
  name: "web-app"
  author: "alice"
  category: web
  difficulty: easy
  description: "A web challenge."
  wave: 1
  flags:
    - { flag: "mctf{test_flag}", case_sensitive: true }
  need_deploy: true
deploy:
  subdomain: "web-app"
  port: 14000
  internal_port: 8000
  dynamic_instances: false
  tcp: false
  resources:
    cpu: "0.5"
    memory: "128M"
  healthcheck:
    type: http
    path: "/"
    interval: "30s"
    timeout: "5s"
    retries: 3
`

const tcpChallengeYAML = `schema: 1
challenge:
  name: "pwn-me"
  author: "bob"
  category: pwn
  difficulty: medium
  description: "A pwn challenge."
  wave: 1
  flags:
    - { flag: "mctf{pwn_flag}", case_sensitive: true }
  need_deploy: true
deploy:
  subdomain: "pwn-me"
  port: 15000
  internal_port: 5000
  dynamic_instances: false
  tcp: true
  resources:
    cpu: "0.25"
    memory: "64M"
`

const staticChallengeYAML = `schema: 1
challenge:
  name: "crypto-easy"
  author: "carol"
  category: crypto
  difficulty: easy
  description: "A static crypto challenge."
  wave: 1
  flags:
    - { flag: "mctf{crypto_flag}", case_sensitive: true }
  need_deploy: false
`

const instancedChallengeYAML = `schema: 1
challenge:
  name: "inst-web"
  author: "dave"
  category: web
  difficulty: hard
  description: "An instanced web challenge."
  wave: 1
  flags:
    - { flag: "mctf{inst_flag}", case_sensitive: true }
  need_deploy: true
deploy:
  subdomain: "inst-web"
  port: null
  internal_port: 9000
  dynamic_instances: true
  resources:
    cpu: "0.5"
    memory: "256M"
`

// ---------- E2E: Full pipeline from config files to manifest generation ----------

func TestE2E_FullPipeline_HTTPChallenge(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(plan))
	}

	u := plan[0]

	// Verify config fields are honored
	if u.Namespace != "ctf-ns" {
		t.Errorf("namespace = %q, want ctf-ns", u.Namespace)
	}
	if u.IngressClass != "nginx" {
		t.Errorf("ingressClass = %q, want nginx", u.IngressClass)
	}
	if u.Name != "mtcf-e2e-test-web-web-app" {
		t.Errorf("name = %q, want mtcf-e2e-test-web-web-app", u.Name)
	}
	if u.Image != "ghcr.io/testing/e2e-test-web-web-app:latest" {
		t.Errorf("image = %q", u.Image)
	}
	if u.InternalPort != 8000 {
		t.Errorf("internalPort = %d, want 8000", u.InternalPort)
	}
	if u.TCP {
		t.Error("expected TCP=false for HTTP challenge")
	}
	if u.Domain != "ctf.example.com" {
		t.Errorf("domain = %q", u.Domain)
	}
	if u.Subdomain != "web-app" {
		t.Errorf("subdomain = %q", u.Subdomain)
	}

	// Verify manifest generation produces Deployment + Service + Ingress
	raw, err := ManifestsYAML(u)
	if err != nil {
		t.Fatalf("ManifestsYAML: %v", err)
	}
	s := string(raw)

	for _, want := range []string{
		"kind: Deployment",
		"kind: Service",
		"kind: Ingress",
		"namespace: ctf-ns",
		"image: ghcr.io/testing/e2e-test-web-web-app:latest",
		"containerPort: 8000",
		"web-app.ctf.example.com",
		"ingressClassName: nginx",
		"cpu: \"0.5\"",
		"memory: 128M",
		"httpGet:",
		"path: /",
		"secretName: mtcf-e2e-test-web-web-app-tls",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("manifest missing %q\nfull YAML:\n%s", want, s)
		}
	}

	// Should NOT contain NodePort (HTTP challenge uses ClusterIP)
	if strings.Contains(s, "NodePort") {
		t.Errorf("HTTP challenge should use ClusterIP, not NodePort")
	}
}

func TestE2E_FullPipeline_TCPChallenge(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"pwn/pwn-me": tcpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(plan))
	}

	u := plan[0]
	if !u.TCP {
		t.Error("expected TCP=true")
	}

	raw, err := ManifestsYAML(u)
	if err != nil {
		t.Fatalf("ManifestsYAML: %v", err)
	}
	s := string(raw)

	if strings.Contains(s, "kind: Ingress") {
		t.Error("TCP challenge must NOT produce Ingress")
	}
	if !strings.Contains(s, "NodePort") {
		t.Error("TCP challenge must use NodePort service type")
	}
	if !strings.Contains(s, "containerPort: 5000") {
		t.Errorf("wrong container port\n%s", s)
	}
}

func TestE2E_FullPipeline_FiltersStaticAndInstanced(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app":   httpChallengeYAML,
		"pwn/pwn-me":    tcpChallengeYAML,
		"crypto/easy":   staticChallengeYAML,
		"web/inst-web":  instancedChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Only service-archetype challenges (web-app and pwn-me) should appear
	if len(plan) != 2 {
		names := make([]string, len(plan))
		for i, u := range plan {
			names[i] = u.Challenge
		}
		t.Fatalf("expected 2 units (web-app, pwn-me), got %d: %v", len(plan), names)
	}

	// Sorted: pwn/pwn-me before web/web-app
	if plan[0].Challenge != "pwn/pwn-me" {
		t.Errorf("plan[0] = %q, want pwn/pwn-me", plan[0].Challenge)
	}
	if plan[1].Challenge != "web/web-app" {
		t.Errorf("plan[1] = %q, want web/web-app", plan[1].Challenge)
	}
}

func TestE2E_FullPipeline_CategoryFilter(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
		"pwn/pwn-me":  tcpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{CategoryFilter: "web"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Category != "web" {
		t.Errorf("category filter failed: got %+v", plan)
	}
}

func TestE2E_FullPipeline_SlugFilter(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
		"pwn/pwn-me":  tcpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{SlugFilter: "pwn-me"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Slug != "pwn-me" {
		t.Errorf("slug filter failed: got %+v", plan)
	}
}

func TestE2E_FullPipeline_EmptyPlanWhenNoMatch(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"crypto/easy": staticChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 0 {
		t.Errorf("expected 0 units for static-only event, got %d", len(plan))
	}
}

// ---------- E2E: Config field variations ----------

func TestE2E_ConfigFields_CustomNamespace(t *testing.T) {
	cfgYAML := `version: 1
event:
  name: "NS Test"
  slug: "ns-test"
  domain: "ctf.example.com"
registry:
  url: "ghcr.io/testing"
deploy:
  mode: kubernetes
  kubernetes:
    namespace: "prod-ctf"
`
	cfg, challs := setupEvent(t, cfgYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
	})
	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Namespace != "prod-ctf" {
		t.Errorf("namespace = %q, want prod-ctf", plan[0].Namespace)
	}
}

func TestE2E_ConfigFields_DefaultNamespace(t *testing.T) {
	cfgYAML := `version: 1
event:
  name: "Default NS"
  slug: "default-ns"
  domain: "ctf.example.com"
registry:
  url: "ghcr.io/testing"
deploy:
  mode: kubernetes
`
	cfg, challs := setupEvent(t, cfgYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
	})
	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Namespace != "default" {
		t.Errorf("namespace = %q, want default", plan[0].Namespace)
	}
}

func TestE2E_ConfigFields_KubeconfigPath(t *testing.T) {
	cfgYAML := `version: 1
event:
  name: "Kubeconfig Test"
  slug: "kube-test"
  domain: "ctf.example.com"
registry:
  url: "ghcr.io/testing"
deploy:
  mode: kubernetes
  kubernetes:
    kubeconfig: "/home/user/.kube/custom-config"
    namespace: "ctf"
`
	cfg, err := config.Load(func() string {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "mtcf.config.yml"), cfgYAML)
		writeFile(t, filepath.Join(root, "challenges", "web", "web-app", "challenge", "challenge.yml"), httpChallengeYAML)
		return root
	}())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Deploy.Kubernetes.Kubeconfig != "/home/user/.kube/custom-config" {
		t.Errorf("kubeconfig = %q", cfg.Deploy.Kubernetes.Kubeconfig)
	}
}

func TestE2E_ConfigFields_IngressClass(t *testing.T) {
	cfgYAML := `version: 1
event:
  name: "Ingress Test"
  slug: "ingress-test"
  domain: "ctf.example.com"
registry:
  url: "ghcr.io/testing"
deploy:
  mode: kubernetes
  kubernetes:
    ingress_class: "traefik"
`
	cfg, challs := setupEvent(t, cfgYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
	})
	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].IngressClass != "traefik" {
		t.Errorf("ingressClass = %q, want traefik", plan[0].IngressClass)
	}

	raw, err := ManifestsYAML(plan[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ingressClassName: traefik") {
		t.Errorf("manifest missing ingressClassName: traefik\n%s", raw)
	}
}

// ---------- E2E: Compose image resolution ----------

func TestE2E_ImageFromCompose(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "mtcf.config.yml"), k8sConfigYAML)
	writeFile(t, filepath.Join(root, "challenges", "web", "web-app", "challenge", "challenge.yml"), httpChallengeYAML)
	writeFile(t, filepath.Join(root, "challenges", "web", "web-app", "challenge", "docker-compose.yml"),
		`services:
  app:
    image: ghcr.io/myorg/web-app:v2.1
    ports:
      - "14000:8000"
`)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	challs, _, err := challenge.Discover(root, cfg.ChallengesDir)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Image != "ghcr.io/myorg/web-app:v2.1" {
		t.Errorf("image = %q, want ghcr.io/myorg/web-app:v2.1", plan[0].Image)
	}
}

// ---------- E2E: Apply up/down via mock runner ----------

func TestE2E_ApplyUp_MockRunner(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
		"pwn/pwn-me":  tcpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var applied []string
	var manifests []string
	runner := &recordApplier{
		up: func(u KubeUnit) error {
			applied = append(applied, u.Challenge)
			raw, err := ManifestsYAML(u)
			if err != nil {
				return err
			}
			manifests = append(manifests, string(raw))
			return nil
		},
	}

	if err := ApplyUp(context.Background(), runner, plan); err != nil {
		t.Fatalf("ApplyUp: %v", err)
	}

	// Verify order (sorted: pwn before web)
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied, got %d", len(applied))
	}
	if applied[0] != "pwn/pwn-me" || applied[1] != "web/web-app" {
		t.Errorf("apply order = %v", applied)
	}

	// Verify each manifest is valid multi-doc YAML
	for i, m := range manifests {
		if !strings.Contains(m, "kind: Deployment") {
			t.Errorf("manifest[%d] missing Deployment", i)
		}
		if !strings.Contains(m, "kind: Service") {
			t.Errorf("manifest[%d] missing Service", i)
		}
	}
}

func TestE2E_ApplyDown_MockRunner_ReverseOrder(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
		"pwn/pwn-me":  tcpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var downed []string
	runner := &recordApplier{
		down: func(u KubeUnit) error {
			downed = append(downed, u.Challenge)
			return nil
		},
	}

	if err := ApplyDown(context.Background(), runner, plan); err != nil {
		t.Fatalf("ApplyDown: %v", err)
	}

	// Down should be reverse of plan order
	if len(downed) != 2 {
		t.Fatalf("expected 2, got %d", len(downed))
	}
	if downed[0] != "web/web-app" || downed[1] != "pwn/pwn-me" {
		t.Errorf("down order = %v, want [web/web-app, pwn/pwn-me]", downed)
	}
}

func TestE2E_ApplyUp_ErrorStopsExecution(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
		"pwn/pwn-me":  tcpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var applied []string
	runner := &recordApplier{
		up: func(u KubeUnit) error {
			applied = append(applied, u.Challenge)
			if u.Challenge == "pwn/pwn-me" {
				return fmt.Errorf("kubectl failed")
			}
			return nil
		},
	}

	err = ApplyUp(context.Background(), runner, plan)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "kubectl failed") {
		t.Errorf("unexpected error: %v", err)
	}
	// Should have stopped after the first failure
	if len(applied) != 1 {
		t.Errorf("expected 1 applied before error, got %d: %v", len(applied), applied)
	}
}

// ---------- E2E: DryKubeRunner output ----------

func TestE2E_DryRunner_UpOutput(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	dry := &DryKubeRunner{Stdout: &buf}

	if err := ApplyUp(context.Background(), dry, plan); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "kubectl apply -f -") {
		t.Errorf("dry-run up missing kubectl apply header\n%s", out)
	}
	if !strings.Contains(out, "kind: Deployment") {
		t.Errorf("dry-run up missing Deployment\n%s", out)
	}
	if !strings.Contains(out, "kind: Service") {
		t.Errorf("dry-run up missing Service\n%s", out)
	}
	if !strings.Contains(out, "kind: Ingress") {
		t.Errorf("dry-run up missing Ingress\n%s", out)
	}
}

func TestE2E_DryRunner_DownOutput(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	dry := &DryKubeRunner{Stdout: &buf}

	if err := ApplyDown(context.Background(), dry, plan); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "kubectl delete") {
		t.Errorf("dry-run down missing kubectl delete\n%s", out)
	}
	if !strings.Contains(out, "app=mtcf-e2e-test-web-web-app") {
		t.Errorf("dry-run down missing label selector\n%s", out)
	}
	if !strings.Contains(out, "-n ctf-ns") {
		t.Errorf("dry-run down missing namespace\n%s", out)
	}
}

// ---------- E2E: NoSSL and probe variations ----------

func TestE2E_NoSSL_SkipsTLSBlock(t *testing.T) {
	noSSLChallenge := `schema: 1
challenge:
  name: "no-ssl"
  author: "eve"
  category: web
  difficulty: easy
  description: "No SSL challenge."
  wave: 1
  flags:
    - { flag: "mctf{no_ssl}", case_sensitive: true }
  need_deploy: true
deploy:
  subdomain: "no-ssl"
  port: 14001
  internal_port: 8080
  dynamic_instances: false
  noSsl: true
`
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/no-ssl": noSSLChallenge,
	})
	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := ManifestsYAML(plan[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	// Should have Ingress but no TLS
	if !strings.Contains(s, "kind: Ingress") {
		t.Error("expected Ingress for HTTP challenge")
	}
	if strings.Contains(s, "tls:") || strings.Contains(s, "secretName:") {
		t.Errorf("noSsl=true should omit TLS block\n%s", s)
	}
}

func TestE2E_CmdHealthcheck(t *testing.T) {
	cmdChallenge := `schema: 1
challenge:
  name: "cmd-probe"
  author: "frank"
  category: misc
  difficulty: easy
  description: "Challenge with cmd healthcheck."
  wave: 1
  flags:
    - { flag: "mctf{cmd_probe}", case_sensitive: true }
  need_deploy: true
deploy:
  subdomain: "cmd-probe"
  port: 14002
  internal_port: 9090
  dynamic_instances: false
  healthcheck:
    type: cmd
    cmd: ["/bin/sh", "-c", "curl -f http://localhost:9090/"]
    interval: "15s"
    timeout: "3s"
    retries: 5
`
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"misc/cmd-probe": cmdChallenge,
	})
	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := ManifestsYAML(plan[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if !strings.Contains(s, "exec:") {
		t.Errorf("cmd probe missing exec block\n%s", s)
	}
	if !strings.Contains(s, "/bin/sh") {
		t.Errorf("cmd probe missing command\n%s", s)
	}
	if !strings.Contains(s, "failureThreshold: 5") {
		t.Errorf("cmd probe missing retries=5\n%s", s)
	}
	if !strings.Contains(s, "periodSeconds: 15") {
		t.Errorf("cmd probe missing interval=15s\n%s", s)
	}
}

// ---------- E2E: Multi-challenge manifest correctness ----------

func TestE2E_MultiChallenge_ManifestIsolation(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
		"pwn/pwn-me":  tcpChallengeYAML,
	})

	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, u := range plan {
		raw, err := ManifestsYAML(u)
		if err != nil {
			t.Fatalf("%s: ManifestsYAML: %v", u.Challenge, err)
		}
		s := string(raw)

		// Each manifest should only reference its own name
		if !strings.Contains(s, "name: "+u.Name) {
			t.Errorf("%s: manifest missing own name %q", u.Challenge, u.Name)
		}

		// Label selector should match
		if !strings.Contains(s, "app: "+u.Name) {
			t.Errorf("%s: manifest missing app label %q", u.Challenge, u.Name)
		}
	}
}

// ---------- E2E: KubeRunner kubeconfig wiring ----------

func TestE2E_KubeRunner_KubeconfigWiring(t *testing.T) {
	cfg, challs := setupEvent(t, k8sConfigYAML, map[string]string{
		"web/web-app": httpChallengeYAML,
	})

	if cfg.Deploy.Kubernetes == nil {
		t.Fatal("kubernetes config should not be nil")
	}
	if cfg.Deploy.Kubernetes.Kubeconfig != "/tmp/kube.conf" {
		t.Errorf("kubeconfig = %q, want /tmp/kube.conf", cfg.Deploy.Kubernetes.Kubeconfig)
	}

	// Verify the runner gets constructed with the right kubeconfig
	runner := NewKubeRunner(cfg.Deploy.Kubernetes.Kubeconfig)
	if runner.Kubeconfig != "/tmp/kube.conf" {
		t.Errorf("runner.Kubeconfig = %q", runner.Kubeconfig)
	}

	// Plan should still work
	plan, err := Plan(cfg, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(plan))
	}
}

// ---------- E2E: Reject non-kubernetes mode ----------

func TestE2E_RejectDockerMode(t *testing.T) {
	cfgYAML := `version: 1
event:
  name: "Docker Mode"
  slug: "docker-mode"
  domain: "ctf.example.com"
deploy:
  mode: docker
`
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "mtcf.config.yml"), cfgYAML)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Plan(cfg, nil, Options{})
	if err == nil {
		t.Fatal("expected error for docker mode")
	}
	if !strings.Contains(err.Error(), "kubernetes") {
		t.Errorf("error should mention kubernetes: %v", err)
	}
}
