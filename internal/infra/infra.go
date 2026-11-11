// Package infra renders and runs the Terraform configuration behind
// `mctf infra plan|apply|destroy`: a single VPS (Hetzner Cloud or
// DigitalOcean) with a firewall and docker preinstalled, sized and placed
// from the `infra` block of mctf.config.yml.
//
// The configuration is rendered from embedded templates into
// <eventRoot>/.mctf/infra/, which is also where Terraform keeps its local
// state. Provider credentials never touch the rendered files: the
// providers read them from their own environment variables.
package infra

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/validate"
)

// WorkDir is the Terraform working directory relative to the event root.
var WorkDir = filepath.Join(".mctf", "infra")

// StateFile is Terraform's local state file inside WorkDir.
const StateFile = "terraform.tfstate"

//go:embed templates/*.tmpl
var templateFS embed.FS

// Provider describes one supported infrastructure provider.
type Provider struct {
	Name     string // value of infra.provider
	TokenEnv string // env var the Terraform provider reads its API token from
	Region   string // default region / location
	Size     string // default server type / droplet size
	Image    string // default OS image (must ship cloud-init)
}

// Providers lists the supported providers, keyed by infra.provider.
var Providers = map[string]Provider{
	"hetzner": {
		Name:     "hetzner",
		TokenEnv: "HCLOUD_TOKEN",
		Region:   "fsn1",
		Size:     "cx23",
		Image:    "ubuntu-24.04",
	},
	"digitalocean": {
		Name:     "digitalocean",
		TokenEnv: "DIGITALOCEAN_TOKEN",
		Region:   "fra1",
		Size:     "s-2vcpu-4gb",
		Image:    "ubuntu-24-04-x64",
	},
}

// DefaultSSHPublicKey is the operator key installed on the server when
// infra.ssh_public_key is unset.
const DefaultSSHPublicKey = "~/.ssh/id_ed25519.pub"

// Options is the fully resolved input to the templates.
type Options struct {
	Provider     Provider
	Name         string   // resource name: mctf-<event.slug>
	EventSlug    string   // label / tag value
	Region       string   // provider region / location
	Size         string   // server type / droplet size
	Image        string   // OS image
	SSHPublicKey string   // path to the operator's public key (absolute or ~-prefixed)
	Ports        []string // inbound TCP ports / ranges, e.g. "22", "10000-17999"
}

// OptionsFromConfig resolves the infra block of cfg against provider
// defaults. eventRoot anchors a relative ssh_public_key path.
func OptionsFromConfig(cfg *config.Config, eventRoot string) (Options, error) {
	if cfg == nil || cfg.Infra == nil {
		return Options{}, fmt.Errorf("no `infra` block in %s — add one to use `mctf infra` (see docs/spec/infra.md)", config.FileName)
	}
	in := cfg.Infra
	p, ok := Providers[in.Provider]
	if !ok {
		return Options{}, fmt.Errorf("infra.provider %q is not supported (want one of: %s)", in.Provider, strings.Join(ProviderNames(), ", "))
	}

	o := Options{
		Provider:     p,
		Name:         "mctf-" + cfg.Event.Slug,
		EventSlug:    cfg.Event.Slug,
		Region:       firstNonEmpty(in.Region, p.Region),
		Size:         firstNonEmpty(in.Size, p.Size),
		Image:        firstNonEmpty(in.Image, p.Image),
		SSHPublicKey: firstNonEmpty(in.SSHPublicKey, DefaultSSHPublicKey),
	}
	// Terraform runs from WorkDir, so a relative key path would resolve
	// against the wrong directory. ~-prefixed paths are left to
	// Terraform's pathexpand().
	if !strings.HasPrefix(o.SSHPublicKey, "~") && !filepath.IsAbs(o.SSHPublicKey) {
		abs, err := filepath.Abs(filepath.Join(eventRoot, o.SSHPublicKey))
		if err != nil {
			return Options{}, fmt.Errorf("infra.ssh_public_key: %w", err)
		}
		o.SSHPublicKey = abs
	}

	ports, err := FirewallPorts(cfg)
	if err != nil {
		return Options{}, err
	}
	o.Ports = ports
	return o, nil
}

// ProviderNames returns the supported provider names, sorted.
func ProviderNames() []string {
	names := make([]string, 0, len(Providers))
	for n := range Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// portRange is an inclusive TCP port interval.
type portRange struct{ min, max int }

func (r portRange) String() string {
	if r.min == r.max {
		return strconv.Itoa(r.min)
	}
	return fmt.Sprintf("%d-%d", r.min, r.max)
}

// FirewallPorts returns the inbound TCP ports the server firewall opens,
// as sorted, merged ports / ranges: ssh (22, plus deploy.ssh.port when
// set), http/https for ingress, every active category's challenge port
// range, and any extra infra.open_ports.
func FirewallPorts(cfg *config.Config) ([]string, error) {
	ranges := []portRange{{22, 22}, {80, 80}, {443, 443}}
	if cfg.Deploy != nil && cfg.Deploy.SSH != nil && cfg.Deploy.SSH.Port != 0 {
		ranges = append(ranges, portRange{cfg.Deploy.SSH.Port, cfg.Deploy.SSH.Port})
	}
	for _, info := range validate.ResolveCategories(cfg) {
		if !info.Range.Empty() {
			ranges = append(ranges, portRange{info.Range.Min, info.Range.Max})
		}
	}
	if cfg.Infra != nil {
		for _, s := range cfg.Infra.OpenPorts {
			r, err := parsePortRange(s)
			if err != nil {
				return nil, fmt.Errorf("infra.open_ports: %w", err)
			}
			ranges = append(ranges, r)
		}
	}

	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].min != ranges[j].min {
			return ranges[i].min < ranges[j].min
		}
		return ranges[i].max < ranges[j].max
	})
	merged := []portRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.min <= last.max+1 {
			last.max = max(last.max, r.max)
			continue
		}
		merged = append(merged, r)
	}

	out := make([]string, len(merged))
	for i, r := range merged {
		out[i] = r.String()
	}
	return out, nil
}

// parsePortRange parses "1337" or "20000-20100".
func parsePortRange(s string) (portRange, error) {
	lo, hi, isRange := strings.Cut(s, "-")
	minPort, err := strconv.Atoi(lo)
	if err != nil {
		return portRange{}, fmt.Errorf("%q is not a port or port range", s)
	}
	maxPort := minPort
	if isRange {
		if maxPort, err = strconv.Atoi(hi); err != nil {
			return portRange{}, fmt.Errorf("%q is not a port or port range", s)
		}
	}
	if minPort < 1 || maxPort > 65535 || minPort > maxPort {
		return portRange{}, fmt.Errorf("%q is out of range (want 1-65535, low <= high)", s)
	}
	return portRange{minPort, maxPort}, nil
}

// Workspace is the rendered Terraform configuration, keyed by absolute
// path, plus the directory Terraform runs in.
type Workspace struct {
	Dir   string
	Files map[string][]byte
}

// Plan renders the Terraform configuration for the event without
// touching disk.
func Plan(cfg *config.Config, eventRoot string) (*Workspace, error) {
	opts, err := OptionsFromConfig(cfg, eventRoot)
	if err != nil {
		return nil, err
	}
	mainTF, err := Render(opts)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(eventRoot, WorkDir)
	return &Workspace{
		Dir: dir,
		Files: map[string][]byte{
			filepath.Join(dir, "main.tf"):        mainTF,
			filepath.Join(dir, "cloud-init.yml"): CloudInit(),
		},
	}, nil
}

// Render executes the provider's main.tf template.
func Render(o Options) ([]byte, error) {
	name := o.Provider.Name + ".tf.tmpl"
	tmpl, err := template.New(name).Funcs(template.FuncMap{"hcl": hclString}).ParseFS(templateFS, "templates/"+name)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, o); err != nil {
		return nil, fmt.Errorf("render %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// CloudInit is the user data every provider boots the server with: it
// installs docker + the compose plugin so the host is ready for
// `deploy.mode: ssh` as soon as it is up.
func CloudInit() []byte {
	return []byte(`#cloud-config
# Generated by mctf infra. Prepares the host for deploy.mode: ssh.
package_update: true
packages:
  - docker.io
  - docker-compose-v2
runcmd:
  - systemctl enable --now docker
`)
}

// hclString renders s as a quoted HCL string literal, escaping quotes,
// backslashes, control characters, and template sequences (${ and %{) so
// config values are always taken literally.
func hclString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case (c == '$' || c == '%') && i+1 < len(s) && s[i+1] == '{':
			b.WriteByte(c)
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// SortedPaths returns the planned file paths in deterministic order.
func (w *Workspace) SortedPaths() []string {
	paths := make([]string, 0, len(w.Files))
	for p := range w.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// Write writes every rendered file, creating the working directory as
// needed, and returns the written paths in sorted order. Terraform's own
// files in the directory (state, .terraform/, lock file) are left alone.
func (w *Workspace) Write() ([]string, error) {
	if err := os.MkdirAll(w.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", w.Dir, err)
	}
	paths := w.SortedPaths()
	for _, p := range paths {
		if err := os.WriteFile(p, w.Files[p], 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", p, err)
		}
	}
	return paths, nil
}

// HasState reports whether dir holds Terraform state, i.e. whether
// there may be provisioned resources to destroy.
func HasState(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, StateFile))
	return err == nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
