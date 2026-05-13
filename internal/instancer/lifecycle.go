package instancer

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// SpawnSpec is the resolved blueprint for one spawn — pure data,
// produced by PlanSpawn before any docker call.
type SpawnSpec struct {
	ID            string // unique handle (= compose project)
	ChallengeSlug string // <category>/<slug>
	TeamID        string
	Cwd           string            // challenge/ directory (compose context)
	ComposeFile   string            // absolute path to docker-compose.yml
	Env           map[string]string // PORT_<n>, FLAG, TEAM_ID, INSTANCE_ID
	Ports         map[int]int       // internal -> host (for store.Put)
	ExpiresAt     time.Time
}

// SpawnOptions configures a spawn.
type SpawnOptions struct {
	// HostPortStart / HostPortEnd bound the per-team port range. The
	// instancer allocates from this slice (default 30000-32999).
	HostPortStart int
	HostPortEnd   int
	// FlagOverride pins a specific flag value. When empty, the static
	// flag from the manifest is used. (Dynamic flag minting ships
	// later; the field is here so callers can already pass one.)
	FlagOverride string
	// Now lets tests stub time.
	Now func() time.Time
}

// PlanSpawn computes a SpawnSpec for the given (team, challenge) pair.
//
// Idempotency: this is a pure function — it doesn't touch the store
// or the docker socket. Callers should check the store for an
// existing instance first; if one exists, they can re-use its ID and
// ports rather than allocating fresh.
func PlanSpawn(cfg *config.Config, c *challenge.Challenge, teamID string, allocatedPorts map[int]bool, opts SpawnOptions) (*SpawnSpec, error) {
	if c.Archetype != challenge.ArchetypeInstanced {
		return nil, fmt.Errorf("%s/%s: not instanced (archetype=%s)", c.CategoryDir, c.Slug, c.Archetype)
	}
	if c.Manifest.Deploy == nil || c.Manifest.Deploy.InternalPort == nil || *c.Manifest.Deploy.InternalPort == 0 {
		return nil, fmt.Errorf("%s/%s: deploy.internal_port required for instanced spawn", c.CategoryDir, c.Slug)
	}
	if teamID == "" {
		return nil, fmt.Errorf("teamID required")
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	start, end := opts.HostPortStart, opts.HostPortEnd
	if start == 0 {
		start = 30000
	}
	if end == 0 {
		end = 32999
	}

	id := MakeInstanceID(c, teamID)
	internal := *c.Manifest.Deploy.InternalPort
	hostPort, err := allocateHostPort(start, end, allocatedPorts, id, internal)
	if err != nil {
		return nil, err
	}

	flag := opts.FlagOverride
	if flag == "" && len(c.Manifest.Challenge.Flags) > 0 {
		flag = c.Manifest.Challenge.Flags[0].Flag
	}

	timeout := defaultTimeout(cfg, c)
	spec := &SpawnSpec{
		ID:            id,
		ChallengeSlug: c.CategoryDir + "/" + c.Slug,
		TeamID:        teamID,
		Cwd:           c.ChallengeDir,
		ComposeFile:   c.ChallengeDir + "/docker-compose.yml",
		Ports:         map[int]int{internal: hostPort},
		ExpiresAt:     now().Add(timeout).UTC(),
		Env: map[string]string{
			fmt.Sprintf("PORT_%d", internal): fmt.Sprintf("%d", hostPort),
			"INSTANCE_ID":                    id,
			"TEAM_ID":                        teamID,
			"FLAG":                           flag,
		},
	}
	return spec, nil
}

// MakeInstanceID derives a stable-looking but unique ID for a (chall,
// team) pair. Format: "<slug>-<team>-<short>" where short is a 4-byte
// crypto-random hex (so concurrent spawns by the same team don't
// collide).
func MakeInstanceID(c *challenge.Challenge, teamID string) string {
	return fmt.Sprintf("mctf-inst-%s-%s-%s",
		sanitize(c.Slug), sanitize(teamID), shortToken(4))
}

// ProjectName returns the docker compose project name. For v0 it
// equals the instance ID, but kept as a function so the relationship
// is explicit and easy to change later.
func ProjectName(id string) string { return id }

// allocateHostPort picks a free host port from [start,end] using a
// hash of (id, internal) as the starting probe — so the same instance
// re-allocating gets the same port (subject to availability) and
// different instances spread out across the range.
func allocateHostPort(start, end int, allocated map[int]bool, id string, internalPort int) (int, error) {
	if start > end {
		return 0, fmt.Errorf("port range invalid: %d-%d", start, end)
	}
	if allocated == nil {
		allocated = map[int]bool{}
	}
	span := end - start + 1
	probe := (hashStr(id+":"+fmt.Sprint(internalPort)) % uint32(span)) + uint32(start)
	for i := 0; i < span; i++ {
		p := int(probe) + i
		if p > end {
			p -= span
		}
		if !allocated[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port in range %d-%d (%d ports allocated)", start, end, len(allocated))
}

// DestroyArgs returns the `docker compose down` invocation for an
// instance. Mirrors deploy.DownArgs but takes a SpawnSpec so callers
// don't need to reconstruct the mapping.
func DestroyArgs(s SpawnSpec, removeVolumes bool) []string {
	args := []string{"compose", "-p", s.ID, "-f", s.ComposeFile, "down"}
	if removeVolumes {
		args = append(args, "-v")
	}
	return args
}

// UpArgs returns the `docker compose up -d` invocation for an
// instance. Env vars are stamped via the cmd.Env, not as flags.
func UpArgs(s SpawnSpec) []string {
	return []string{"compose", "-p", s.ID, "-f", s.ComposeFile, "up", "-d"}
}

// EnvSlice returns Env as KEY=VALUE pairs sorted by key, suitable for
// exec.Cmd.Env after appending os.Environ().
func (s SpawnSpec) EnvSlice() []string {
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+s.Env[k])
	}
	return out
}

// sanitize produces a docker-compose-friendly token: lowercase
// letters, digits, and dashes. Any other rune is treated as a
// separator and collapsed; runs of dashes collapse to one.
func sanitize(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			// Treat any non-alnum (including space, _, /, .) as a
			// separator. Dot is dropped entirely (so "mctf6.0"
			// becomes "mctf60") to match common slug conventions
			// for version numbers.
			if r == '.' {
				continue
			}
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

// shortToken returns a hex-encoded crypto-random byte string of length n*2.
func shortToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// Fall back to a low-entropy timestamp hex — rand.Read
		// failure is exceedingly rare on supported platforms.
		return fmt.Sprintf("%08x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// hashStr is a small, dependency-free FNV-1a 32-bit hash used to seed
// port allocation probes. We don't need cryptographic strength —
// just deterministic spread across the port range.
func hashStr(s string) uint32 {
	const (
		offset32 uint32 = 2166136261
		prime32  uint32 = 16777619
	)
	h := offset32
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}

// defaultTimeout resolves the spawn TTL from per-challenge override,
// then event-level default, then a 1800s baseline.
func defaultTimeout(cfg *config.Config, c *challenge.Challenge) time.Duration {
	if c.Manifest.Deploy != nil && c.Manifest.Deploy.Instance != nil && c.Manifest.Deploy.Instance.Timeout > 0 {
		return time.Duration(c.Manifest.Deploy.Instance.Timeout) * time.Second
	}
	if cfg != nil && cfg.Instancer != nil && cfg.Instancer.DefaultTimeout > 0 {
		return time.Duration(cfg.Instancer.DefaultTimeout) * time.Second
	}
	return 1800 * time.Second
}
