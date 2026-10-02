package kubernetes

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// Options narrows the planning to specific categories or slugs.
type Options struct {
	CategoryFilter string
	SlugFilter     string
}

// KubeUnit holds everything needed to generate K8s manifests for one
// service-archetype challenge.
type KubeUnit struct {
	Challenge    string // <category>/<slug>
	Category     string
	Slug         string
	Name         string // mctf-<event>-<cat>-<slug> — K8s resource name prefix
	Namespace    string
	Image        string
	InternalPort int
	NodePort     int // fixed NodePort if TCP and port > 0
	Subdomain    string
	Domain       string
	IngressClass string
	NoSSL        bool
	TCP          bool
	Resources    *challenge.Resources
	Healthcheck  *challenge.Healthcheck
}

// Plan builds KubeUnits for every deployable, service-archetype challenge
// after applying filters. cfg.Deploy.Mode must be "kubernetes".
func Plan(cfg *config.Config, challs []*challenge.Challenge, opts Options) ([]KubeUnit, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	if cfg.Deploy == nil || cfg.Deploy.Mode != "kubernetes" {
		return nil, fmt.Errorf("deploy.mode must be 'kubernetes' for kubernetes.Plan")
	}

	ns := "default"
	ingressClass := ""
	if k := cfg.Deploy.Kubernetes; k != nil {
		if k.Namespace != "" {
			ns = k.Namespace
		}
		ingressClass = k.IngressClass
	}

	registry := ""
	if cfg.Registry != nil {
		registry = strings.TrimRight(cfg.Registry.URL, "/")
	}

	var out []KubeUnit
	for _, c := range challs {
		if !c.Manifest.Challenge.NeedDeploy {
			continue
		}
		if c.Archetype == challenge.ArchetypeInstanced {
			continue
		}
		if opts.CategoryFilter != "" && c.CategoryDir != opts.CategoryFilter {
			continue
		}
		if opts.SlugFilter != "" && c.Slug != opts.SlugFilter {
			continue
		}
		d := c.Manifest.Deploy
		if d == nil {
			return nil, fmt.Errorf("%s/%s: need_deploy=true but no deploy block", c.CategoryDir, c.Slug)
		}
		if d.InternalPort == nil || *d.InternalPort == 0 {
			return nil, fmt.Errorf("%s/%s: deploy.internal_port required for kubernetes mode", c.CategoryDir, c.Slug)
		}

		image, err := resolveImage(c, cfg, registry)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", c.CategoryDir, c.Slug, err)
		}

		var nodePort int
		if d.TCP && d.Port != nil && *d.Port > 0 {
			nodePort = *d.Port
		}

		name := fmt.Sprintf("mctf-%s-%s-%s", cfg.Event.Slug, c.CategoryDir, c.Slug)
		out = append(out, KubeUnit{
			Challenge:    filepath.Join(c.CategoryDir, c.Slug),
			Category:     c.CategoryDir,
			Slug:         c.Slug,
			Name:         name,
			Namespace:    ns,
			Image:        image,
			InternalPort: *d.InternalPort,
			NodePort:     nodePort,
			Subdomain:    d.Subdomain,
			Domain:       cfg.Event.Domain,
			IngressClass: ingressClass,
			NoSSL:        d.NoSSL,
			TCP:          d.TCP,
			Resources:    d.Resources,
			Healthcheck:  d.Healthcheck,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Challenge < out[j].Challenge })
	return out, nil
}

// composeDoc is the docker-compose subset needed to extract the image name.
type composeDoc struct {
	Services map[string]struct {
		Image string `json:"image"`
	} `json:"services"`
}

// resolveImage returns the container image for the challenge. It first
// inspects the docker-compose.yml image: field; if absent it falls back
// to <registry>/<event>-<cat>-<slug>:latest (matching mctf build naming).
func resolveImage(c *challenge.Challenge, cfg *config.Config, registry string) (string, error) {
	composePath := filepath.Join(c.ChallengeDir, "docker-compose.yml")
	if data, err := os.ReadFile(composePath); err == nil {
		if jsonBytes, err := yaml.YAMLToJSON(data); err == nil {
			var doc composeDoc
			if yaml.Unmarshal(jsonBytes, &doc) == nil {
				// Use the first service's image, alphabetical order.
				names := make([]string, 0, len(doc.Services))
				for n := range doc.Services {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					if img := doc.Services[n].Image; img != "" {
						return img, nil
					}
				}
			}
		}
	}

	if registry == "" {
		return "", fmt.Errorf("no image: field in docker-compose.yml and registry.url not set — cannot resolve image for kubernetes deploy")
	}
	return fmt.Sprintf("%s/%s-%s-%s:latest", registry, cfg.Event.Slug, c.CategoryDir, c.Slug), nil
}

func parseSeconds(s string, def int) int {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return int(d.Seconds())
}
