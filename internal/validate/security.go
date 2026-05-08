package validate

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// UnmarshalJSON handles compose's polymorphic ports entries: string
// shorthand ("14000:8000", "${PORT_8000:-8000}:8000", "8000") or
// object form ({published, target}).
func (p *composePort) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		p.Raw = s
		host, container := splitPortString(s)
		p.Host = host
		p.Container = container
		return nil
	}
	var obj struct {
		Target    int `json:"target"`
		Published any `json:"published"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	p.Container = obj.Target
	switch v := obj.Published.(type) {
	case string:
		p.Host = v
		p.Raw = fmt.Sprintf("%s:%d", v, obj.Target)
	case float64:
		p.Host = strconv.Itoa(int(v))
		p.Raw = fmt.Sprintf("%d:%d", int(v), obj.Target)
	}
	return nil
}

// splitPortString splits "<host>:<container>" or just "<container>".
// host comes back as the verbatim text (so callers can detect env-var
// patterns); container is parsed as int. Bind-address forms like
// "127.0.0.1:14000:8000" are flattened to host="127.0.0.1:14000".
func splitPortString(s string) (host string, container int) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		host = strings.TrimSpace(s[:i])
		container, _ = strconv.Atoi(strings.TrimSpace(s[i+1:]))
		return
	}
	container, _ = strconv.Atoi(s)
	return
}

// dangerousCaps are Linux capabilities whose presence in `cap_add`
// effectively undoes most container isolation. They warrant an
// explicit author acknowledgement (we warn so authors can justify
// each one in code review).
var dangerousCaps = map[string]struct{}{
	"SYS_ADMIN":       {},
	"NET_ADMIN":       {},
	"SYS_PTRACE":      {},
	"SYS_MODULE":      {},
	"SYS_RAWIO":       {},
	"DAC_READ_SEARCH": {},
	"ALL":             {},
}

// composeShape is the minimal subset of docker-compose.yml we need to
// inspect for security checks. The actual YAML has many more keys; we
// only decode what we look at.
type composeShape struct {
	Services map[string]composeService `json:"services"`
}

type composeService struct {
	Privileged bool          `json:"privileged"`
	CapAdd     []string      `json:"cap_add"`
	User       string        `json:"user"`
	Ports      []composePort `json:"ports"`
}

// composePort is the polymorphic compose ports shape. The compose
// spec accepts either a "host:container" string ("14000:8000") or
// an object ({published: 14000, target: 8000}). UnmarshalJSON below
// handles both.
type composePort struct {
	Raw       string // verbatim string when the entry was a string
	Host      string // empty if not pinned ("8000:" -> "")
	Container int
}

// SecurityChecks runs Dockerfile + docker-compose.yml security rules
// for one challenge. Called from checks.go::checkOne but kept here so
// the per-rule rationale is easy to find.
func SecurityChecks(r *Report, cfg *config.Config, c *challenge.Challenge, eventRoot string) {
	if !c.Manifest.Challenge.NeedDeploy {
		return
	}

	manifestPath := relTo(eventRoot, c.ManifestPath())
	composePath := filepath.Join(c.ChallengeDir, "docker-compose.yml")

	if compose, ok := loadCompose(composePath); ok {
		checkPrivileged(r, cfg, compose, manifestPath)
		checkCapabilities(r, cfg, compose, manifestPath)
		checkComposePorts(r, c, compose, manifestPath)
	}

	checkNonRootForPwn(r, cfg, c, manifestPath)
	checkAssetSize(r, cfg, c, eventRoot)
}

// checkComposePorts emits D001-D003: the compose ports must agree with
// the manifest's deploy.port / deploy.internal_port, and instanced
// challenges must use the ${PORT_N:-N} env-var convention so the
// instancer can fill in per-team host ports.
func checkComposePorts(r *Report, c *challenge.Challenge, compose *composeShape, manifestPath string) {
	if c.Manifest.Deploy == nil {
		return
	}
	d := c.Manifest.Deploy
	for _, svc := range compose.Services {
		for _, port := range svc.Ports {
			if port.Container == 0 {
				continue
			}
			if c.Archetype == challenge.ArchetypeService {
				// D002 — container port must equal internal_port (if set).
				if d.InternalPort != nil && *d.InternalPort != 0 && port.Container != *d.InternalPort {
					r.Errorf("D002", manifestPath,
						"compose maps container port %d but deploy.internal_port is %d",
						port.Container, *d.InternalPort)
				}
				// D001 — host side must match deploy.port literally.
				if d.Port != nil {
					hostInt, _ := strconv.Atoi(port.Host)
					if hostInt != *d.Port {
						r.Errorf("D001", manifestPath,
							"compose maps host %q -> %d but deploy.port is %d",
							port.Host, port.Container, *d.Port)
					}
				}
			}
			if c.Archetype == challenge.ArchetypeInstanced {
				// D002 (container port match) still applies.
				if d.InternalPort != nil && *d.InternalPort != 0 && port.Container != *d.InternalPort {
					r.Errorf("D002", manifestPath,
						"compose maps container port %d but deploy.internal_port is %d",
						port.Container, *d.InternalPort)
				}
				// D003 — host side must use ${PORT_<container>:-...} pattern.
				if !isInstancerPortPattern(port.Host, port.Container) {
					r.Errorf("D003", manifestPath,
						"instanced compose ports[%q] must use ${PORT_%d:-%d} so the instancer can override the host port",
						port.Raw, port.Container, port.Container)
				}
			}
		}
	}
}

// isInstancerPortPattern reports whether the compose host side is the
// expected ${PORT_<container>:-...} form (with any default value).
func isInstancerPortPattern(host string, container int) bool {
	want := fmt.Sprintf("${PORT_%d", container)
	return strings.HasPrefix(host, want)
}

// SEC002 — privileged: true. Always warn; fail under the pro profile.
func checkPrivileged(r *Report, cfg *config.Config, compose *composeShape, manifestPath string) {
	for name, svc := range compose.Services {
		if !svc.Privileged {
			continue
		}
		profile := config.ProfileSimple
		if cfg != nil && cfg.Profile != "" {
			profile = cfg.Profile
		}
		msg := "service " + name + " runs with privileged: true (escapes most container isolation)"
		if profile == config.ProfilePro {
			r.Errorf("SEC002", manifestPath, "%s — disallowed under the pro profile", msg)
		} else {
			r.Warnf("SEC002", manifestPath, "%s — review carefully before deploying", msg)
		}
	}
}

// SEC003 — pwn / jailbreak Dockerfiles must end with a USER directive
// after the last FROM (the runtime stage of a multi-stage build).
// Pro profile escalates to a hard-fail.
//
// In a multi-stage build, a USER in the builder stage doesn't apply to
// the final image, so we look for USER positioned after the last FROM
// to ensure the runtime user is non-root.
var (
	reFromLine = regexp.MustCompile(`(?im)^\s*FROM\b`)
	reUserLine = regexp.MustCompile(`(?im)^\s*USER\b`)
)

func checkNonRootForPwn(r *Report, cfg *config.Config, c *challenge.Challenge, manifestPath string) {
	cat := c.Manifest.Challenge.Category
	if cat != "pwn" && cat != "jailbreak" {
		return
	}
	dockerfile := filepath.Join(c.ChallengeDir, "Dockerfile")
	data, err := os.ReadFile(dockerfile)
	if err != nil {
		return
	}
	if userAfterLastFrom(data) {
		return
	}
	msg := fmt.Sprintf("%s Dockerfile has no USER directive after the last FROM — process will run as root inside the container", cat)
	if profileIs(cfg, config.ProfilePro) {
		r.Errorf("SEC003", manifestPath, "%s", msg)
	} else {
		r.Warnf("SEC003", manifestPath, "%s", msg)
	}
}

func userAfterLastFrom(dockerfile []byte) bool {
	froms := reFromLine.FindAllIndex(dockerfile, -1)
	if len(froms) == 0 {
		// No FROM at all — odd, but not our problem to flag here.
		return reUserLine.Match(dockerfile)
	}
	lastFrom := froms[len(froms)-1][0]
	return reUserLine.Match(dockerfile[lastFrom:])
}

// SEC004 — `cap_add` contains a capability that defeats container
// isolation. Warn under simple/instanced; fail under pro.
func checkCapabilities(r *Report, cfg *config.Config, compose *composeShape, manifestPath string) {
	for name, svc := range compose.Services {
		for _, cap := range svc.CapAdd {
			normalized := strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(cap), "CAP_"))
			if _, dangerous := dangerousCaps[normalized]; !dangerous {
				continue
			}
			msg := fmt.Sprintf("service %s requests cap_add: %s — review whether the challenge needs it", name, cap)
			if profileIs(cfg, config.ProfilePro) {
				r.Errorf("SEC004", manifestPath, "%s — disallowed under the pro profile", msg)
			} else {
				r.Warnf("SEC004", manifestPath, "%s", msg)
			}
		}
	}
}

// D008 / D009 — asset size limits.
//
// D008 fires per-file when an asset exceeds 100 MB (CTFd practical
// per-file ceiling). D009 fires once when the assets/ directory
// totals more than 500 MB. Pro profile escalates both to hard-fail.
const (
	assetPerFileLimit = int64(100 * 1024 * 1024)
	assetTotalLimit   = int64(500 * 1024 * 1024)
)

func checkAssetSize(r *Report, cfg *config.Config, c *challenge.Challenge, eventRoot string) {
	pro := profileIs(cfg, config.ProfilePro)
	emit := func(rule, file, format string, args ...any) {
		if pro {
			r.Errorf(rule, file, format, args...)
		} else {
			r.Warnf(rule, file, format, args...)
		}
	}
	var total int64
	_ = filepath.WalkDir(c.AssetsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		total += fi.Size()
		if fi.Size() > assetPerFileLimit {
			emit("D008", relTo(eventRoot, p),
				"asset %s is %d MB — CTFd uploads typically cap around 100 MB",
				d.Name(), fi.Size()/(1024*1024))
		}
		return nil
	})
	if total > assetTotalLimit {
		emit("D009", relTo(eventRoot, c.AssetsDir),
			"assets/ totals %d MB — review whether players need this much",
			total/(1024*1024))
	}
}

func profileIs(cfg *config.Config, p config.Profile) bool {
	if cfg == nil {
		return false
	}
	return cfg.Profile == p
}

func loadCompose(path string) (*composeShape, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	jsonBytes, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, false
	}
	var c composeShape
	if err := yaml.Unmarshal(jsonBytes, &c); err != nil {
		return nil, false
	}
	return &c, true
}
