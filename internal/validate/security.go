package validate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// dangerousCaps are Linux capabilities whose presence in `cap_add`
// effectively undoes most container isolation. They warrant an
// explicit author acknowledgement (we warn so authors can justify
// each one in code review).
var dangerousCaps = map[string]struct{}{
	"SYS_ADMIN":  {},
	"NET_ADMIN":  {},
	"SYS_PTRACE": {},
	"SYS_MODULE": {},
	"SYS_RAWIO":  {},
	"DAC_READ_SEARCH": {},
	"ALL":        {},
}

// composeShape is the minimal subset of docker-compose.yml we need to
// inspect for security checks. The actual YAML has many more keys; we
// only decode what we look at.
type composeShape struct {
	Services map[string]composeService `json:"services"`
}

type composeService struct {
	Privileged bool     `json:"privileged"`
	CapAdd     []string `json:"cap_add"`
	User       string   `json:"user"`
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
	}

	checkNonRootForPwn(r, cfg, c, manifestPath)
	checkAssetSize(r, cfg, c, eventRoot)
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
