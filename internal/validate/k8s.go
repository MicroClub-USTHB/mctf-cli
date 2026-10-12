package validate

import (
	"fmt"
	"regexp"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

const (
	// DefaultNodePortBase is the first port of the stock Kubernetes
	// NodePort range (--service-node-port-range=30000-32767).
	DefaultNodePortBase = 30000
	// NodePortMax is the last port of the stock Kubernetes NodePort range.
	NodePortMax = 32767
	// maxK8sNameLen is the DNS-1123 label limit used for Service names.
	maxK8sNameLen = 63
)

var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// NodePortBase returns the effective node_port_base for the event config.
// An unset value defaults to DefaultNodePortBase; an explicit 0 disables
// the mapping (deploy.port is used verbatim, which requires a cluster with
// a widened --service-node-port-range).
func NodePortBase(cfg *config.Config) int {
	if cfg != nil && cfg.Deploy != nil && cfg.Deploy.Kubernetes != nil &&
		cfg.Deploy.Kubernetes.NodePortBase != nil {
		return *cfg.Deploy.Kubernetes.NodePortBase
	}
	return DefaultNodePortBase
}

// NodePortFor maps a challenge's public deploy.port to the Kubernetes
// NodePort used for its Service:
//
//	nodePort = node_port_base + (port - categoryRange.Min)
//
// The result is deterministic, so players always reach a challenge on the
// same port across redeploys. With node_port_base: 0 the port is used as-is.
func NodePortFor(cfg *config.Config, category string, port int) (int, error) {
	base := NodePortBase(cfg)
	if base == 0 {
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("deploy.port %d is not a valid TCP port", port)
		}
		return port, nil
	}
	cat, ok := ResolveCategories(cfg)[category]
	if !ok || cat.Range.Empty() {
		return 0, fmt.Errorf("category %q has no port range; cannot derive a NodePort from deploy.port %d", category, port)
	}
	if !cat.Range.Contains(port) {
		return 0, fmt.Errorf("deploy.port %d outside %s range %d-%d; cannot derive a NodePort",
			port, category, cat.Range.Min, cat.Range.Max)
	}
	np := base + (port - cat.Range.Min)
	if np < DefaultNodePortBase || np > NodePortMax {
		return 0, fmt.Errorf("derived NodePort %d (node_port_base %d + offset %d) is outside %d-%d",
			np, base, port-cat.Range.Min, DefaultNodePortBase, NodePortMax)
	}
	return np, nil
}

// K8sResourceName builds the Kubernetes resource name for a challenge.
func K8sResourceName(eventSlug, category, slug string) string {
	return fmt.Sprintf("mctf-%s-%s-%s", eventSlug, category, slug)
}

// CheckK8sName reports why name is not usable as a Kubernetes Service /
// Deployment name (DNS-1123 label, at most 63 characters).
func CheckK8sName(name string) error {
	if len(name) > maxK8sNameLen {
		return fmt.Errorf("resource name %q is %d chars (max %d); shorten the event slug, category or challenge slug",
			name, len(name), maxK8sNameLen)
	}
	if !dns1123Label.MatchString(name) {
		return fmt.Errorf("resource name %q is not a valid DNS-1123 label (lowercase a-z, 0-9 and '-', starting and ending alphanumeric)", name)
	}
	return nil
}
