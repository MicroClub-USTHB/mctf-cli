package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func makeChallenge(t *testing.T, root, cat, slug string, dockerfile, compose string) *challenge.Challenge {
	t.Helper()
	chRoot := filepath.Join(root, cat, slug)
	chDir := filepath.Join(chRoot, "challenge")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if dockerfile != "" {
		_ = os.WriteFile(filepath.Join(chDir, "Dockerfile"), []byte(dockerfile), 0o644)
	}
	if compose != "" {
		_ = os.WriteFile(filepath.Join(chDir, "docker-compose.yml"), []byte(compose), 0o644)
	}
	return &challenge.Challenge{
		Slug: slug, CategoryDir: cat, RootDir: chRoot, ChallengeDir: chDir,
		AssetsDir: filepath.Join(chRoot, "assets"),
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: slug, Category: cat, NeedDeploy: true,
			},
			Deploy: &challenge.DeployBlock{},
		},
		Archetype: challenge.ArchetypeService,
	}
}

func TestSEC002_PrivilegedWarnsUnderSimple(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "pwn", "x", "FROM scratch\n",
		`services:
  app:
    privileged: true
`)
	r := &Report{}
	cfg := &config.Config{Profile: config.ProfileSimple}
	SecurityChecks(r, cfg, c, root)

	if !hasRule(r, "SEC002", SeverityWarn) {
		t.Errorf("SEC002 should warn under simple profile: %s", r.String())
	}
}

func TestSEC002_PrivilegedFailsUnderPro(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "pwn", "x", "FROM scratch\nUSER ctf\n",
		`services:
  app:
    privileged: true
`)
	r := &Report{}
	cfg := &config.Config{Profile: config.ProfilePro}
	SecurityChecks(r, cfg, c, root)
	if !hasRule(r, "SEC002", SeverityError) {
		t.Errorf("SEC002 should fail under pro profile: %s", r.String())
	}
}

func TestSEC003_PwnRequiresUserDirective(t *testing.T) {
	root := t.TempDir()
	noUser := makeChallenge(t, root, "pwn", "rootful", "FROM scratch\n", `services:
  app: { image: x }
`)
	r := &Report{}
	SecurityChecks(r, nil, noUser, root)
	if !hasRule(r, "SEC003", SeverityWarn) {
		t.Errorf("SEC003 should warn when pwn Dockerfile has no USER: %s", r.String())
	}
}

func TestSEC003_PwnWithUserOK(t *testing.T) {
	root := t.TempDir()
	withUser := makeChallenge(t, root, "pwn", "polite", "FROM scratch\nUSER nobody\n", `services:
  app: { image: x }
`)
	r := &Report{}
	SecurityChecks(r, nil, withUser, root)
	if hasRule(r, "SEC003", SeverityWarn) {
		t.Errorf("SEC003 should not warn when USER is set: %s", r.String())
	}
}

func TestSEC003_MultiStageUserOnlyInBuilder(t *testing.T) {
	root := t.TempDir()
	// USER ctf in the builder stage doesn't apply to the runtime stage,
	// so this Dockerfile should still trip SEC003.
	df := `FROM golang:1.23 AS builder
RUN adduser ctf
USER ctf
WORKDIR /src

FROM alpine:3.20
COPY --from=builder /src/app /app
CMD ["/app"]
`
	c := makeChallenge(t, root, "pwn", "ms", df, `services:
  app: { image: x }
`)
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if !hasRule(r, "SEC003", SeverityWarn) {
		t.Errorf("SEC003 should warn when USER is only in builder stage: %s", r.String())
	}
}

func TestSEC003_OnlyForPwnAndJailbreak(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "web", "no-user", "FROM scratch\n", `services:
  app: { image: x }
`)
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if hasRule(r, "SEC003", SeverityWarn) {
		t.Errorf("SEC003 should not apply to web: %s", r.String())
	}
}

func TestSEC003_EscalatesUnderPro(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "pwn", "rootful", "FROM scratch\n", `services:
  app: { image: x }
`)
	r := &Report{}
	SecurityChecks(r, &config.Config{Profile: config.ProfilePro}, c, root)
	if !hasRule(r, "SEC003", SeverityError) {
		t.Errorf("SEC003 should fail under pro profile: %s", r.String())
	}
}

func TestSEC004_DangerousCapabilities(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "misc", "x", "FROM scratch\n",
		`services:
  app:
    cap_add:
      - SYS_ADMIN
      - NET_RAW
`)
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if !hasRule(r, "SEC004", SeverityWarn) {
		t.Errorf("SEC004 should warn for SYS_ADMIN: %s", r.String())
	}
	// NET_RAW is not in our dangerous list — should not warn.
	count := 0
	for _, e := range r.Issues {
		if e.Rule == "SEC004" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 SEC004, got %d", count)
	}
}

func TestD008_LargeFileWarns(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "misc", "huge", "", "")
	if err := os.MkdirAll(c.AssetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, 101*1024*1024)
	if err := os.WriteFile(filepath.Join(c.AssetsDir, "huge.zip"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if !hasRule(r, "D008", SeverityWarn) {
		t.Errorf("D008 should warn for >100 MB file: %s", r.String())
	}
}

func TestD008_FailsUnderPro(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "misc", "huge", "", "")
	_ = os.MkdirAll(c.AssetsDir, 0o755)
	big := make([]byte, 101*1024*1024)
	_ = os.WriteFile(filepath.Join(c.AssetsDir, "huge.zip"), big, 0o644)
	r := &Report{}
	SecurityChecks(r, &config.Config{Profile: config.ProfilePro}, c, root)
	if !hasRule(r, "D008", SeverityError) {
		t.Errorf("D008 should fail under pro: %s", r.String())
	}
}

func TestD001_ServiceHostPortMismatchFails(t *testing.T) {
	root := t.TempDir()
	port := 14000
	internal := 8000
	c := makeChallenge(t, root, "misc", "x", "FROM scratch\nUSER ctf\n",
		`services:
  app:
    ports:
      - "9999:8000"
`)
	c.Manifest.Deploy = &challenge.DeployBlock{Port: &port, InternalPort: &internal}
	c.Archetype = challenge.ArchetypeService
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if !hasRule(r, "D001", SeverityError) {
		t.Errorf("D001 should fail when host port differs from deploy.port: %s", r.String())
	}
}

func TestD002_ServiceContainerPortMismatchFails(t *testing.T) {
	root := t.TempDir()
	port := 14000
	internal := 8000
	c := makeChallenge(t, root, "misc", "x", "FROM scratch\nUSER ctf\n",
		`services:
  app:
    ports:
      - "14000:9999"
`)
	c.Manifest.Deploy = &challenge.DeployBlock{Port: &port, InternalPort: &internal}
	c.Archetype = challenge.ArchetypeService
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if !hasRule(r, "D002", SeverityError) {
		t.Errorf("D002 should fail when container port differs from internal_port: %s", r.String())
	}
}

func TestD001_ServiceMatchingPortsClean(t *testing.T) {
	root := t.TempDir()
	port := 14000
	internal := 8000
	c := makeChallenge(t, root, "misc", "x", "FROM scratch\nUSER ctf\n",
		`services:
  app:
    ports:
      - "14000:8000"
`)
	c.Manifest.Deploy = &challenge.DeployBlock{Port: &port, InternalPort: &internal}
	c.Archetype = challenge.ArchetypeService
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if hasRule(r, "D001", SeverityError) || hasRule(r, "D002", SeverityError) {
		t.Errorf("matching ports should not fire D001/D002: %s", r.String())
	}
}

func TestD003_InstancedRequiresEnvVarPattern(t *testing.T) {
	root := t.TempDir()
	internal := 8000
	c := makeChallenge(t, root, "web", "x", "FROM scratch\n",
		`services:
  app:
    ports:
      - "8000:8000"
`)
	c.Manifest.Deploy = &challenge.DeployBlock{InternalPort: &internal, DynamicInstances: true}
	c.Archetype = challenge.ArchetypeInstanced
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if !hasRule(r, "D003", SeverityError) {
		t.Errorf("D003 should fail when instanced compose lacks ${PORT_N:-N}: %s", r.String())
	}
}

func TestD003_InstancedWithEnvPatternClean(t *testing.T) {
	root := t.TempDir()
	internal := 8000
	c := makeChallenge(t, root, "web", "x", "FROM scratch\n",
		`services:
  app:
    ports:
      - "${PORT_8000:-8000}:8000"
`)
	c.Manifest.Deploy = &challenge.DeployBlock{InternalPort: &internal, DynamicInstances: true}
	c.Archetype = challenge.ArchetypeInstanced
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if hasRule(r, "D003", SeverityError) {
		t.Errorf("instanced with env-var pattern should not fire D003: %s", r.String())
	}
}

func TestSplitPortString(t *testing.T) {
	cases := []struct {
		in           string
		wantHost     string
		wantContainer int
	}{
		{"14000:8000", "14000", 8000},
		{"${PORT_8000:-8000}:8000", "${PORT_8000:-8000}", 8000},
		{"127.0.0.1:14000:8000", "127.0.0.1:14000", 8000},
		{"8000", "", 8000},
	}
	for _, c := range cases {
		host, container := splitPortString(c.in)
		if host != c.wantHost || container != c.wantContainer {
			t.Errorf("splitPortString(%q) = (%q, %d), want (%q, %d)",
				c.in, host, container, c.wantHost, c.wantContainer)
		}
	}
}

func TestSecurityChecks_StaticChallengesSkipped(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "crypto", "static", "", "")
	c.Manifest.Challenge.NeedDeploy = false
	c.Archetype = challenge.ArchetypeStatic
	r := &Report{}
	SecurityChecks(r, nil, c, root)
	if len(r.Issues) != 0 {
		t.Errorf("static challenges should produce no security entries, got %d", len(r.Issues))
	}
}

func hasRule(r *Report, rule string, sev Severity) bool {
	for _, e := range r.Issues {
		if e.Rule == rule && e.Severity == sev {
			return true
		}
	}
	return false
}
