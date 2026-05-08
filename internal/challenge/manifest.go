// Package challenge loads and validates per-challenge manifests, derives
// archetype, and exposes a typed Challenge view to the rest of the CLI.
package challenge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/MicroClub-USTHB/mctf-cli/internal/jsonschema"
	"github.com/MicroClub-USTHB/mctf-cli/internal/schemas"
)

// Archetype is one of the three runtime shapes a challenge can take.
type Archetype string

const (
	ArchetypeStatic    Archetype = "static"
	ArchetypeService   Archetype = "service"
	ArchetypeInstanced Archetype = "instanced"
)

// Manifest is the typed view of challenge/challenge.yml.
type Manifest struct {
	Schema    int            `json:"schema,omitempty"`
	Challenge ChallengeBlock `json:"challenge"`
	Deploy    *DeployBlock   `json:"deploy,omitempty"`
}

// ChallengeBlock holds challenge metadata.
type ChallengeBlock struct {
	Name        string   `json:"name"`
	Author      string   `json:"author"`
	Category    string   `json:"category"`
	Difficulty  string   `json:"difficulty"`
	Description string   `json:"description"`
	Wave        int      `json:"wave"`
	ForceHidden bool     `json:"force_hidden,omitempty"`
	Flags       []Flag   `json:"flags"`
	NeedDeploy  bool     `json:"need_deploy"`
	Links       []string `json:"links,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Hints       []Hint   `json:"hints,omitempty"`
	Scoring     *Scoring `json:"scoring,omitempty"`
}

// Flag is one accepted flag for a challenge.
type Flag struct {
	Flag          string `json:"flag"`
	CaseSensitive bool   `json:"case_sensitive"`
}

// Hint is one cost-tiered hint shown in the platform.
type Hint struct {
	Cost    int    `json:"cost"`
	Content string `json:"content"`
}

// Scoring overrides the event-level scoring config for this challenge.
type Scoring struct {
	Type    string `json:"type"`
	Initial int    `json:"initial,omitempty"`
	Decay   int    `json:"decay,omitempty"`
	Minimum int    `json:"minimum,omitempty"`
	Points  int    `json:"points,omitempty"`
}

// DeployBlock is required when ChallengeBlock.NeedDeploy is true.
type DeployBlock struct {
	Subdomain        string        `json:"subdomain"`
	Port             *int          `json:"port"`
	InternalPort     *int          `json:"internal_port"`
	DynamicInstances bool          `json:"dynamic_instances"`
	TCP              bool          `json:"tcp,omitempty"`
	NoSSL            bool          `json:"noSsl,omitempty"`
	Resources        *Resources    `json:"resources,omitempty"`
	Healthcheck      *Healthcheck  `json:"healthcheck,omitempty"`
	Instance         *InstanceCfg  `json:"instance,omitempty"`
	SecurityOpts     *SecurityOpts `json:"security_opts,omitempty"`
}

// Resources caps a challenge's runtime footprint.
type Resources struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
	PIDs   int    `json:"pids,omitempty"`
	Disk   string `json:"disk,omitempty"`
}

// Healthcheck controls liveness probing.
type Healthcheck struct {
	Type     string   `json:"type"`
	Path     string   `json:"path,omitempty"`
	Cmd      []string `json:"cmd,omitempty"`
	Interval string   `json:"interval,omitempty"`
	Timeout  string   `json:"timeout,omitempty"`
	Retries  int      `json:"retries,omitempty"`
}

// InstanceCfg controls per-team spawn behavior for instanced challenges.
type InstanceCfg struct {
	Timeout    int `json:"timeout,omitempty"`
	ExtendTime int `json:"extend_time,omitempty"`
	MaxPerTeam int `json:"max_per_team,omitempty"`
}

// SecurityOpts narrows or widens default container security.
type SecurityOpts struct {
	AllowCaps       []string `json:"allow_caps,omitempty"`
	AllowPrivileged bool     `json:"allow_privileged,omitempty"`
}

// Challenge is a manifest plus discovered filesystem context.
type Challenge struct {
	Manifest     Manifest
	Slug         string    // directory name under <category>/
	CategoryDir  string    // directory name under challenges/ (must match Manifest.Challenge.Category)
	RootDir      string    // absolute path to the challenge root (parent of challenge/, solution/, ...)
	ChallengeDir string    // absolute path to <root>/challenge/
	SolutionDir  string    // absolute path to <root>/solution/
	AssetsDir    string    // absolute path to <root>/assets/
	OtherDir     string    // absolute path to <root>/other/
	Archetype    Archetype // derived
}

// ManifestPath returns the path to challenge/challenge.yml.
func (c *Challenge) ManifestPath() string {
	return filepath.Join(c.ChallengeDir, "challenge.yml")
}

// HasDockerfile reports whether challenge/Dockerfile exists.
func (c *Challenge) HasDockerfile() bool {
	return fileExists(filepath.Join(c.ChallengeDir, "Dockerfile"))
}

// HasCompose reports whether challenge/docker-compose.yml exists.
func (c *Challenge) HasCompose() bool {
	return fileExists(filepath.Join(c.ChallengeDir, "docker-compose.yml"))
}

// HasWriteup reports whether solution/writeup.md exists and is non-empty.
func (c *Challenge) HasWriteup() bool {
	fi, err := os.Stat(filepath.Join(c.SolutionDir, "writeup.md"))
	if err != nil {
		return false
	}
	return fi.Size() > 0
}

// LoadManifest parses and schema-validates challenge/challenge.yml at the
// given absolute manifest path. It returns the typed Manifest and the raw
// YAML bytes (the latter so callers can do extra string-level checks like
// the static-flag image-leak guard).
func LoadManifest(path string) (Manifest, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("read %s: %w", path, err)
	}
	v, err := jsonschema.New("challenge.schema.json", schemas.Challenge)
	if err != nil {
		return Manifest{}, raw, err
	}
	if _, err := v.ValidateYAML(raw); err != nil {
		return Manifest{}, raw, err
	}
	jsonBytes, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return Manifest{}, raw, fmt.Errorf("parse yaml: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(jsonBytes, &m); err != nil {
		return Manifest{}, raw, fmt.Errorf("decode: %w", err)
	}
	if m.Schema == 0 {
		m.Schema = 1
	}
	return m, raw, nil
}

// DeriveArchetype computes a challenge's runtime archetype from its manifest.
//
// Rules: need_deploy=false -> static; need_deploy=true && dynamic=false ->
// service; need_deploy=true && dynamic=true -> instanced.
func DeriveArchetype(m Manifest) Archetype {
	if !m.Challenge.NeedDeploy {
		return ArchetypeStatic
	}
	if m.Deploy != nil && m.Deploy.DynamicInstances {
		return ArchetypeInstanced
	}
	return ArchetypeService
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// SlugFromName produces the conventional dir slug for a challenge.name —
// lowercased, spaces and slashes replaced with underscores, kept ASCII.
func SlugFromName(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}
