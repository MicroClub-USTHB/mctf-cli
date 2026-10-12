// Package config loads and validates an MCTF event's mctf.config.yml.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/MicroClub-USTHB/mctf-cli/internal/jsonschema"
	"github.com/MicroClub-USTHB/mctf-cli/internal/schemas"
)

// FileName is the conventional config filename at the event repo root.
const FileName = "mctf.config.yml"

// Profile is one of the curated presets.
type Profile string

const (
	ProfileSimple    Profile = "simple"
	ProfileInstanced Profile = "instanced"
	ProfilePro       Profile = "pro"
)

// Config is the typed view of mctf.config.yml.
//
// Field names use JSON tags (the loader goes YAML -> JSON -> Go) so the
// public schema/mctf-config.schema.json drives validation; this struct
// only has to match shapes for fields the CLI actually consumes.
type Config struct {
	Version int     `json:"version"`
	Event   Event   `json:"event"`
	Profile Profile `json:"profile,omitempty"`

	Categories Categories `json:"categories,omitempty"`

	Registry  *Registry  `json:"registry,omitempty"`
	Deploy    *Deploy    `json:"deploy,omitempty"`
	Platform  *Platform  `json:"platform,omitempty"`
	Instancer *Instancer `json:"instancer,omitempty"`

	ChallengesDir string `json:"challenges_dir,omitempty"`

	// Reserved (validated but not yet consumed).
	Notifications map[string]any   `json:"notifications,omitempty"`
	Monitoring    map[string]any   `json:"monitoring,omitempty"`
	Plugins       []map[string]any `json:"plugins,omitempty"`
}

// Event holds top-level event identity and scoring defaults.
type Event struct {
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Domain      string   `json:"domain,omitempty"`
	FlagPrefix  string   `json:"flag_prefix,omitempty"`
	WavesNumber int      `json:"waves_number,omitempty"`
	CurrentWave *int     `json:"current_wave,omitempty"`
	Scoring     *Scoring `json:"scoring,omitempty"`
}

// Scoring describes how points are awarded. Used as the per-event default
// (a challenge can override).
type Scoring struct {
	Type    string `json:"type"`
	Initial int    `json:"initial,omitempty"`
	Decay   int    `json:"decay,omitempty"`
	Minimum int    `json:"minimum,omitempty"`
	Points  int    `json:"points,omitempty"`
}

// Categories controls the active category catalog.
type Categories struct {
	Extended []string         `json:"extended,omitempty"`
	Custom   []CustomCategory `json:"custom,omitempty"`
}

// CustomCategory is a per-event extension declared in mctf.config.yml.
type CustomCategory struct {
	Slug       string     `json:"slug"`
	Name       string     `json:"name"`
	Deployable bool       `json:"deployable,omitempty"`
	PortRange  *PortRange `json:"port_range,omitempty"`
}

// PortRange is an inclusive [Min, Max] interval.
type PortRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Registry is the container image registry config.
type Registry struct {
	URL string `json:"url"`
}

// Deploy selects the runtime mode and its mode-specific options.
type Deploy struct {
	Mode       string            `json:"mode"`
	Docker     *DockerDeploy     `json:"docker,omitempty"`
	SSH        *SSHDeploy        `json:"ssh,omitempty"`
	Kubernetes *KubernetesDeploy `json:"kubernetes,omitempty"`
}

// DockerDeploy is the docker-compose deploy mode.
type DockerDeploy struct {
	Network string `json:"network,omitempty"`
	Ingress string `json:"ingress,omitempty"`
}

// SSHDeploy is remote single-host deploy via ssh+scp.
type SSHDeploy struct {
	Host       string `json:"host"`
	Port       int    `json:"port,omitempty"`
	User       string `json:"user,omitempty"`
	KeyPath    string `json:"key_path,omitempty"`
	RemoteRoot string `json:"remote_root,omitempty"`
	// JumpHost is an ssh ProxyJump target (user@host[:port]). When set,
	// every ssh + scp invocation routes through it.
	JumpHost string `json:"jump_host,omitempty"`
}

// KubernetesDeploy targets a K8s cluster.
type KubernetesDeploy struct {
	Kubeconfig   string `json:"kubeconfig,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	IngressClass string `json:"ingress_class,omitempty"`
	// NodePortBase is the first NodePort assigned to TCP challenges:
	// nodePort = base + (deploy.port - category range min). nil means the
	// default (30000); 0 disables the mapping and uses deploy.port verbatim
	// (needs a widened --service-node-port-range on the cluster).
	NodePortBase *int `json:"node_port_base,omitempty"`
}

// Platform identifies the CTF scoring/registration platform.
type Platform struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

// Instancer holds defaults consumed by the per-team instancer.
type Instancer struct {
	URL               string `json:"url,omitempty"`
	DefaultTimeout    int    `json:"default_timeout,omitempty"`
	DefaultExtendTime int    `json:"default_extend_time,omitempty"`
}

// Load reads, schema-validates, and decodes mctf.config.yml from dir.
//
// dir is the event repo root; the file is expected at <dir>/mctf.config.yml.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no %s found in %s — run `mctf init` to scaffold one", FileName, dir)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	v, err := jsonschema.New("mctf-config.schema.json", schemas.MCTFConfig)
	if err != nil {
		return nil, err
	}
	if _, err := v.ValidateYAML(raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	jsonBytes, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: parse yaml: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(jsonBytes, &c); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", path, err)
	}
	c.applyDefaults()
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Profile == "" {
		c.Profile = ProfileSimple
	}
	if c.ChallengesDir == "" {
		c.ChallengesDir = "./challenges"
	}
	if c.Event.FlagPrefix == "" {
		c.Event.FlagPrefix = "mctf"
	}
	if c.Event.WavesNumber == 0 {
		c.Event.WavesNumber = 3
	}
}
