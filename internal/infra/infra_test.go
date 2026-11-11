package infra

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current templates")

// eventCfg is a minimal event config with an infra block for provider.
func eventCfg(provider string) *config.Config {
	return &config.Config{
		Version: 1,
		Event:   config.Event{Name: "Sample", Slug: "sample-event"},
		Infra:   &config.Infra{Provider: provider},
	}
}

// corePorts is what the core category catalog contributes: 10000-17999
// is contiguous, so it collapses into one rule next to ssh/http/https.
var corePorts = []string{"22", "80", "443", "10000-17999"}

func TestOptionsFromConfig_Defaults(t *testing.T) {
	for _, name := range ProviderNames() {
		t.Run(name, func(t *testing.T) {
			o, err := OptionsFromConfig(eventCfg(name), "/event")
			if err != nil {
				t.Fatal(err)
			}
			p := Providers[name]
			if o.Name != "mctf-sample-event" || o.EventSlug != "sample-event" {
				t.Errorf("name/slug = %q/%q", o.Name, o.EventSlug)
			}
			if o.Region != p.Region || o.Size != p.Size || o.Image != p.Image {
				t.Errorf("defaults not applied: %+v", o)
			}
			if o.SSHPublicKey != DefaultSSHPublicKey {
				t.Errorf("ssh key = %q, want %q", o.SSHPublicKey, DefaultSSHPublicKey)
			}
			if !reflect.DeepEqual(o.Ports, corePorts) {
				t.Errorf("ports = %v, want %v", o.Ports, corePorts)
			}
		})
	}
}

func TestOptionsFromConfig_Overrides(t *testing.T) {
	cfg := eventCfg("hetzner")
	cfg.Infra.Region = "hel1"
	cfg.Infra.Size = "cpx31"
	cfg.Infra.Image = "debian-12"
	cfg.Infra.SSHPublicKey = "keys/ops.pub"
	root := t.TempDir()

	o, err := OptionsFromConfig(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	if o.Region != "hel1" || o.Size != "cpx31" || o.Image != "debian-12" {
		t.Errorf("overrides not applied: %+v", o)
	}
	// Terraform runs from .mctf/infra, so a relative key path is anchored
	// at the event root instead.
	if want := filepath.Join(root, "keys", "ops.pub"); o.SSHPublicKey != want {
		t.Errorf("ssh key = %q, want %q", o.SSHPublicKey, want)
	}
}

func TestOptionsFromConfig_Errors(t *testing.T) {
	if _, err := OptionsFromConfig(&config.Config{}, "."); err == nil || !strings.Contains(err.Error(), "no `infra` block") {
		t.Errorf("missing infra block: err = %v", err)
	}
	if _, err := OptionsFromConfig(eventCfg("aws"), "."); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("unsupported provider: err = %v", err)
	}
}

func TestFirewallPorts(t *testing.T) {
	cfg := eventCfg("hetzner")
	cfg.Deploy = &config.Deploy{Mode: "ssh", SSH: &config.SSHDeploy{Host: "h", Port: 2222}}
	cfg.Categories.Extended = []string{"blockchain"} // 17000-17999, overlaps hardware
	cfg.Categories.Custom = []config.CustomCategory{
		{Slug: "koth", Name: "KotH", PortRange: &config.PortRange{Min: 18000, Max: 18099}}, // adjacent: merges
		{Slug: "quiz2", Name: "No ports"},
	}
	cfg.Infra.OpenPorts = []string{"8443", "25000-25010", "443"}

	got, err := FirewallPorts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"22", "80", "443", "2222", "8443", "10000-18099", "25000-25010"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ports = %v, want %v", got, want)
	}
}

func TestFirewallPorts_InvalidOpenPorts(t *testing.T) {
	for _, bad := range []string{"http", "0", "70000", "200-100", "1-", "-5"} {
		cfg := eventCfg("hetzner")
		cfg.Infra.OpenPorts = []string{bad}
		if _, err := FirewallPorts(cfg); err == nil {
			t.Errorf("open_ports %q: expected an error", bad)
		}
	}
}

func TestHCLString(t *testing.T) {
	cases := map[string]string{
		"fsn1":            `"fsn1"`,
		`a"b\c`:           `"a\"b\\c"`,
		"line\nbreak\t":   `"line\nbreak\t"`,
		"${var.x} %{if}":  `"$${var.x} %%{if}"`,
		"cost $5, 100%":   `"cost $5, 100%"`,
		"~/.ssh/id.pub":   `"~/.ssh/id.pub"`,
		"/home/a b/k.pub": `"/home/a b/k.pub"`,
	}
	for in, want := range cases {
		if got := hclString(in); got != want {
			t.Errorf("hclString(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestRender_Golden pins the rendered main.tf per provider. Run with
// -update after an intentional template change.
func TestRender_Golden(t *testing.T) {
	for _, name := range ProviderNames() {
		t.Run(name, func(t *testing.T) {
			o, err := OptionsFromConfig(eventCfg(name), "/event")
			if err != nil {
				t.Fatal(err)
			}
			got, err := Render(o)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", name+".main.tf.golden")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run `go test ./internal/infra -run Golden -update` to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("rendered main.tf differs from %s (rerun with -update if intended)\n--- got ---\n%s", golden, got)
			}
		})
	}
}

// TestRender_TerraformFmt checks the rendered HCL is canonically
// formatted, which also proves it parses. Skips without terraform.
func TestRender_TerraformFmt(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not on PATH")
	}
	for _, name := range ProviderNames() {
		t.Run(name, func(t *testing.T) {
			o, err := OptionsFromConfig(eventCfg(name), "/event")
			if err != nil {
				t.Fatal(err)
			}
			body, err := Render(o)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("terraform", "fmt", "-check", "-diff", "-")
			cmd.Stdin = bytes.NewReader(body)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("terraform fmt -check: %v\n%s", err, out)
			}
		})
	}
}

func TestPlanWrite_PreservesTerraformFiles(t *testing.T) {
	root := t.TempDir()
	ws, err := Plan(eventCfg("digitalocean"), root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Dir != filepath.Join(root, ".mctf", "infra") {
		t.Errorf("dir = %s", ws.Dir)
	}
	if HasState(ws.Dir) {
		t.Fatal("HasState before any state exists")
	}

	// Simulate state left by a previous apply; rendering must not touch it.
	if err := os.MkdirAll(ws.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(ws.Dir, StateFile)
	if err := os.WriteFile(state, []byte(`{"version":4}`), 0o644); err != nil {
		t.Fatal(err)
	}

	written, err := ws.Write()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(ws.Dir, "cloud-init.yml"), filepath.Join(ws.Dir, "main.tf")}
	if !reflect.DeepEqual(written, want) {
		t.Errorf("written = %v, want %v", written, want)
	}
	if b, _ := os.ReadFile(state); string(b) != `{"version":4}` {
		t.Errorf("state file was modified: %q", b)
	}
	if !HasState(ws.Dir) {
		t.Error("HasState = false with a state file present")
	}
	if b, _ := os.ReadFile(filepath.Join(ws.Dir, "cloud-init.yml")); !bytes.HasPrefix(b, []byte("#cloud-config\n")) {
		t.Errorf("cloud-init.yml must start with #cloud-config, got %q", b)
	}
}

func TestDryRunner(t *testing.T) {
	var out bytes.Buffer
	d := &DryRunner{Bin: "terraform", Stdout: &out}
	ctx := context.Background()
	_ = d.Plan(ctx, "/w")
	_ = d.Apply(ctx, "/w", true)
	_ = d.Destroy(ctx, "/w", false)
	want := strings.Join([]string{
		"(cd /w; terraform init -input=false)",
		"(cd /w; terraform plan -input=false)",
		"(cd /w; terraform init -input=false)",
		"(cd /w; terraform apply -auto-approve)",
		"(cd /w; terraform init -input=false)",
		"(cd /w; terraform destroy)",
	}, "\n") + "\n"
	if out.String() != want {
		t.Errorf("dry output:\n%s\nwant:\n%s", out.String(), want)
	}
}

// fakeTerraform writes a stand-in terraform that appends its working
// directory and arguments to a log, and exits 1 when its first argument
// matches failOn.
func fakeTerraform(t *testing.T, failOn string) (bin, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake terraform is a shell script")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, "terraform")
	log = filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\n" +
		"echo \"$(pwd) $*\" >> " + log + "\n" +
		"[ \"$1\" = \"" + failOn + "\" ] && exit 1\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func TestTerraformRunner_RunsInitThenCommand(t *testing.T) {
	bin, log := fakeTerraform(t, "")
	work := t.TempDir()
	r := &TerraformRunner{Bin: bin, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	ctx := context.Background()
	if err := r.Plan(ctx, work); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(ctx, work, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Destroy(ctx, work, true); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(work) // pwd reports the resolved path (macOS /private/var)
	want := strings.Join([]string{
		real + " init -input=false",
		real + " plan -input=false",
		real + " init -input=false",
		real + " apply",
		real + " init -input=false",
		real + " destroy -auto-approve",
	}, "\n") + "\n"
	if string(b) != want {
		t.Errorf("calls:\n%s\nwant:\n%s", b, want)
	}
}

func TestTerraformRunner_StopsOnInitFailure(t *testing.T) {
	bin, log := fakeTerraform(t, "init")
	r := &TerraformRunner{Bin: bin, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	err := r.Apply(context.Background(), t.TempDir(), true)
	if err == nil || !strings.Contains(err.Error(), "init") {
		t.Fatalf("err = %v, want an init failure", err)
	}
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "apply") {
		t.Errorf("apply ran after init failed:\n%s", b)
	}
}

func TestNewTerraformRunner_MissingBinary(t *testing.T) {
	t.Setenv(BinEnv, "mctf-no-such-terraform")
	_, err := NewTerraformRunner()
	if err == nil || !strings.Contains(err.Error(), "--dry-run") {
		t.Errorf("err = %v, want a not-found error pointing at --dry-run", err)
	}
}
