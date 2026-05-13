package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// fixtureChallenge builds a manifest+filesystem skeleton large enough
// to clear the schema-level checks in checkOne so the W010 rule can be
// exercised in isolation.
func fixtureChallenge(t *testing.T, root, slug, description string) *challenge.Challenge {
	t.Helper()
	chRoot := filepath.Join(root, "misc", slug)
	chDir := filepath.Join(chRoot, "challenge")
	solDir := filepath.Join(chRoot, "solution")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(solDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(solDir, "writeup.md"), []byte("# w\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &challenge.Challenge{
		Slug: slug, CategoryDir: "misc", RootDir: chRoot, ChallengeDir: chDir,
		SolutionDir: solDir,
		AssetsDir:   filepath.Join(chRoot, "assets"),
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: slug, Author: "a", Category: "misc", Difficulty: "easy",
				Wave: 1, Description: description,
				Flags:      []challenge.Flag{{Flag: "mctf{x}", CaseSensitive: true}},
				NeedDeploy: false,
			},
		},
		Archetype: challenge.ArchetypeStatic,
	}
}

func TestW010_ShortDescriptionOK(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "ok", "short and sweet")
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W010", SeverityWarn) || hasRule(r, "W010", SeverityError) {
		t.Errorf("W010 should not fire for short description: %s", r.String())
	}
}

func TestW010_LongDescriptionWarns(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "long", strings.Repeat("a", 1600))
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W010", SeverityWarn) {
		t.Errorf("W010 should warn at >1500 chars: %s", r.String())
	}
}

func TestW010_VeryLongDescriptionFails(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "vlong", strings.Repeat("a", 4100))
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W010", SeverityError) {
		t.Errorf("W010 should hard-fail at >4000 chars: %s", r.String())
	}
}

func TestW006_MissingHintsWarns(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "x", "fine")
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W006", SeverityWarn) {
		t.Errorf("W006 should warn when no hints exist: %s", r.String())
	}
}

func TestW006_HintsFromManifestSatisfies(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "x", "fine")
	c.Manifest.Challenge.Hints = []challenge.Hint{{Cost: 0, Content: "free hint"}}
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W006", SeverityWarn) {
		t.Errorf("W006 should not warn when challenge.hints[] is non-empty: %s", r.String())
	}
}

func TestA007_ServiceMissingInternalPortFails(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "x", "fine")
	c.Manifest.Challenge.NeedDeploy = true
	c.Manifest.Challenge.Category = "misc"
	c.CategoryDir = "misc"
	port := 14000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain: "x",
		Port:      &port,
		// internal_port intentionally unset
	}
	c.Archetype = challenge.ArchetypeService
	// Need Dockerfile + compose to clear A006
	if err := os.WriteFile(filepath.Join(c.ChallengeDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte("services:\n  app: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "A007", SeverityError) {
		t.Errorf("A007 should hard-fail: %s", r.String())
	}
}

func TestA007_ServiceWithInternalPortOK(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "x", "fine")
	c.Manifest.Challenge.NeedDeploy = true
	c.Manifest.Challenge.Category = "misc"
	c.CategoryDir = "misc"
	port := 14000
	internal := 8000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain:    "x",
		Port:         &port,
		InternalPort: &internal,
	}
	c.Archetype = challenge.ArchetypeService
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte("services:\n  app: {}\n"), 0o644)
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "A007", SeverityError) {
		t.Errorf("A007 should not fire when internal_port is set: %s", r.String())
	}
}

func TestW007_HighWaveWarns(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "x", "fine")
	c.Manifest.Challenge.Wave = 7
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf", WavesNumber: 9}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W007", SeverityWarn) {
		t.Errorf("W007 should warn at wave > 5: %s", r.String())
	}
}

// Tests for W002, W009, and W011 heuristic validation rules
func TestW011_AssetFoundNoWarn(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "cub3", "Download `cub3.zip` to solve this challenge")
	c.Manifest.Challenge.Category = "crypto"
	c.CategoryDir = "crypto"
	// Create the referenced asset
	assetsDir := filepath.Join(c.RootDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "cub3.zip"), []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W011", SeverityWarn) {
		t.Errorf("W011 should not warn when asset exists: %s", r.String())
	}
}

func TestW011_AssetMissingWarns(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "cub3", "Download `cub3.zip` to solve this challenge")
	c.Manifest.Challenge.Category = "crypto"
	c.CategoryDir = "crypto"
	// Create assets dir but don't add the file
	assetsDir := filepath.Join(c.RootDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W011", SeverityWarn) {
		t.Errorf("W011 should warn when asset missing: %s", r.String())
	}
}

func TestW011_RecursiveAssetsFound(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "nested", "See `payload.bin` in the handouts")
	c.Manifest.Challenge.Category = "pwn"
	c.CategoryDir = "pwn"
	// Create nested assets structure
	assetsDir := filepath.Join(c.RootDir, "assets", "handouts")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "payload.bin"), []byte("DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W011", SeverityWarn) {
		t.Errorf("W011 should find files in nested assets/ dirs: %s", r.String())
	}
}

// Inline code like `whoami`, `mctf{flag}`, `nc host 1337` is not a filename
// and must not trigger W011 — the regex requires a `.<ext>` suffix.
func TestW011_InlineCodeNotAFilename(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "inline",
		"Run `whoami` and submit `mctf{flag_here}`. Try `nc host 1337`.")
	c.Manifest.Challenge.Category = "crypto"
	c.CategoryDir = "crypto"
	assetsDir := filepath.Join(c.RootDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W011", SeverityWarn) {
		t.Errorf("W011 false-positive on inline code: %s", r.String())
	}
}

func TestW011_URLsIgnored(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "url-test",
		"See [research](https://example.com/paper.pdf) and `solution.zip`")
	c.Manifest.Challenge.Category = "web"
	c.CategoryDir = "web"
	// Create assets dir without the solution.zip
	assetsDir := filepath.Join(c.RootDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	// Should warn about solution.zip but not about example.com
	warns := []Issue{}
	for _, f := range r.Issues {
		if f.Rule == "W011" {
			warns = append(warns, f)
		}
	}
	if len(warns) != 1 {
		t.Errorf("W011 should find 1 missing asset (not URLs): found %d, %s", len(warns), r.String())
	}
	if !strings.Contains(warns[0].Message, "solution.zip") {
		t.Errorf("W011 should warn about solution.zip, got: %s", warns[0].Message)
	}
}

func TestW002_MutableStateServiceWarns(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "login",
		"Login as admin to view other users' sessions. Exploit the broken access control.")
	c.Manifest.Challenge.Category = "web"
	c.CategoryDir = "web"
	c.Manifest.Challenge.NeedDeploy = true
	port := 8000
	internal := 5000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain:    "login",
		Port:         &port,
		InternalPort: &internal,
	}
	c.Archetype = challenge.ArchetypeService
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte("services:\n  app: {}\n"), 0o644)
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W002", SeverityWarn) {
		t.Errorf("W002 should warn on mutable state hints: %s", r.String())
	}
}

func TestW002_InstancedNoWarn(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "login",
		"Login as admin to view other users' sessions. Exploit the broken access control.")
	c.Manifest.Challenge.Category = "web"
	c.CategoryDir = "web"
	c.Manifest.Challenge.NeedDeploy = true
	internal := 5000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain:        "login",
		InternalPort:     &internal,
		DynamicInstances: true, // This makes it instanced, not service
	}
	c.Archetype = challenge.ArchetypeInstanced
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "Dockerfile"), []byte("FROM scratch\nHEALTHCHECK CMD true\n"), 0o644)
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte("services:\n  app: {}\n"), 0o644)
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W002", SeverityWarn) {
		t.Errorf("W002 should not warn for instanced challenges: %s", r.String())
	}
}

func TestW009_CryptoRateLimitedNoWarn(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "oracle",
		"The oracle is rate-limited to 1 request per second. Decrypt the flag.")
	c.Manifest.Challenge.Category = "crypto"
	c.CategoryDir = "crypto"
	c.Manifest.Challenge.NeedDeploy = true
	port := 9000
	internal := 5000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain:    "oracle",
		Port:         &port,
		InternalPort: &internal,
	}
	c.Archetype = challenge.ArchetypeService
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "Dockerfile"), []byte("FROM scratch\nHEALTHCHECK CMD true\n"), 0o644)
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte("services:\n  app: {}\n"), 0o644)
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W009", SeverityWarn) {
		t.Errorf("W009 should not warn when rate-limiting is mentioned: %s", r.String())
	}
}

func TestW009_CryptoPowNoWarn(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "pow-oracle",
		"Submit a proof-of-work to call the oracle. Solve the puzzle before attacking.")
	c.Manifest.Challenge.Category = "crypto"
	c.CategoryDir = "crypto"
	c.Manifest.Challenge.NeedDeploy = true
	port := 9001
	internal := 5000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain:    "pow",
		Port:         &port,
		InternalPort: &internal,
	}
	c.Archetype = challenge.ArchetypeService
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "Dockerfile"), []byte("FROM scratch\nHEALTHCHECK CMD true\n"), 0o644)
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte("services:\n  app: {}\n"), 0o644)
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if hasRule(r, "W009", SeverityWarn) {
		t.Errorf("W009 should not warn for pow mention: %s", r.String())
	}
}

func TestW009_CryptoNoProtectionWarns(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "weak-oracle",
		"Call the oracle endpoint to decrypt messages. Reverse the crypto algorithm.")
	c.Manifest.Challenge.Category = "crypto"
	c.CategoryDir = "crypto"
	c.Manifest.Challenge.NeedDeploy = true
	port := 9002
	internal := 5000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain:    "weak",
		Port:         &port,
		InternalPort: &internal,
	}
	c.Archetype = challenge.ArchetypeService
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "Dockerfile"), []byte("FROM scratch\nHEALTHCHECK CMD true\n"), 0o644)
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte("services:\n  app: {}\n"), 0o644)
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W009", SeverityWarn) {
		t.Errorf("W009 should warn when no rate-limit hint: %s", r.String())
	}
}

func TestW009_PowerfulNotPow(t *testing.T) {
	root := t.TempDir()
	c := fixtureChallenge(t, root, "power",
		"Use powerful algorithms to reverse the encryption scheme. Break the cipher.")
	c.Manifest.Challenge.Category = "crypto"
	c.CategoryDir = "crypto"
	c.Manifest.Challenge.NeedDeploy = true
	port := 9003
	internal := 5000
	c.Manifest.Deploy = &challenge.DeployBlock{
		Subdomain:    "power",
		Port:         &port,
		InternalPort: &internal,
	}
	c.Archetype = challenge.ArchetypeService
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "Dockerfile"), []byte("FROM scratch\nHEALTHCHECK CMD true\n"), 0o644)
	_ = os.WriteFile(filepath.Join(c.ChallengeDir, "docker-compose.yml"), []byte("services:\n  app: {}\n"), 0o644)
	r := Run(&config.Config{Event: config.Event{Slug: "x", FlagPrefix: "mctf"}}, root, []*challenge.Challenge{c}, nil)
	if !hasRule(r, "W009", SeverityWarn) {
		t.Errorf("W009 should warn (powerful is not pow): %s", r.String())
	}
}
