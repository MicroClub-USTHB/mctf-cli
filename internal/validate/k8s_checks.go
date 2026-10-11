package validate

import (
	"path/filepath"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// checkKubernetes runs the kubernetes-mode rules. It is a no-op unless
// deploy.mode is "kubernetes".
//
//   - K001: a TCP challenge's deploy.port cannot be mapped to a valid NodePort
//   - K002: the derived resource name is not a valid DNS-1123 label (<= 63 chars)
//   - K003: two TCP challenges map to the same NodePort
func checkKubernetes(r *Report, cfg *config.Config, eventRoot string, challs []*challenge.Challenge) {
	if cfg == nil || cfg.Deploy == nil || cfg.Deploy.Mode != "kubernetes" {
		return
	}
	seen := map[int]string{}
	for _, c := range challs {
		m := c.Manifest
		if m.Challenge.Name == "" || !m.Challenge.NeedDeploy || c.Archetype != challenge.ArchetypeService {
			continue
		}
		path := relTo(eventRoot, c.ManifestPath())
		key := filepath.Join(c.CategoryDir, c.Slug)

		name := K8sResourceName(cfg.Event.Slug, c.CategoryDir, c.Slug)
		if err := CheckK8sName(name); err != nil {
			r.Errorf("K002", path, "%v", err)
		}

		if m.Deploy == nil || !m.Deploy.TCP {
			continue
		}
		if m.Deploy.Port == nil || *m.Deploy.Port <= 0 {
			r.Errorf("K001", path, "tcp challenge requires deploy.port to derive its NodePort")
			continue
		}
		np, err := NodePortFor(cfg, c.CategoryDir, *m.Deploy.Port)
		if err != nil {
			r.Errorf("K001", path, "%v", err)
			continue
		}
		if other, dup := seen[np]; dup {
			r.Errorf("K003", path, "NodePort %d (from deploy.port %d) already assigned to %s", np, *m.Deploy.Port, other)
			continue
		}
		seen[np] = key
	}
}
