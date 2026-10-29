// Package plugin runs language-agnostic MCTF plugins as subprocesses and
// dispatches lifecycle-hook events to them over a small JSON-RPC protocol.
//
// Wire format: newline-delimited JSON (one JSON object per line). The host
// writes requests to the plugin's stdin; the plugin writes responses to its
// stdout. The plugin's stderr is passed through to the host's stderr for
// debugging. Any executable that speaks this contract — in any language — is a
// valid plugin.
//
// Plugins are best-effort and crash-isolated (mirroring internal/notify): a
// plugin that errors, crashes, or hangs is logged and dropped, and never
// aborts the MCTF command.
package plugin

import (
	"encoding/json"
	"time"
)

// protocolVersion is bumped when the request/response contract changes.
const protocolVersion = 1

// Method names.
const (
	methodInitialize = "initialize"
	methodShutdown   = "shutdown"
)

// request is a host→plugin JSON-RPC message.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is a plugin→host JSON-RPC message.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// initParams is sent by the host in the initialize request.
type initParams struct {
	ProtocolVersion int            `json:"protocol_version"`
	MCTFVersion     string         `json:"mctf_version"`
	Config          map[string]any `json:"config,omitempty"`
}

// initResult is the plugin's handshake reply: its name and the hooks it wants.
type initResult struct {
	Name  string   `json:"name"`
	Hooks []string `json:"hooks"`
}

// Hook names.
const (
	HookBeforeDeploy    = "before_deploy"
	HookAfterDeploy     = "after_deploy"
	HookBeforeBuild     = "before_build"
	HookAfterBuild      = "after_build"
	HookOnInstanceSpawn = "on_instance_spawn"
	HookOnHealthFailure = "on_health_failure"
)

// DeployPayload is the JSON body for the before_/after_deploy hooks.
type DeployPayload struct {
	Event   string       `json:"event"`
	Phase   string       `json:"phase"` // "before" | "after"
	Mode    string       `json:"mode"`  // "docker" | "ssh"
	DryRun  bool         `json:"dry_run"`
	Units   []DeployUnit `json:"units"`
	Outcome string       `json:"outcome,omitempty"` // after: "success" | "failed"
	Error   string       `json:"error,omitempty"`
}

// DeployUnit is one challenge in a deploy payload.
type DeployUnit struct {
	Challenge   string `json:"challenge"`
	Category    string `json:"category"`
	Slug        string `json:"slug"`
	ProjectName string `json:"project_name"`
	ImageTag    string `json:"image_tag,omitempty"`
}

// BuildPayload is the JSON body for the before_/after_build hooks.
type BuildPayload struct {
	Event   string       `json:"event"`
	Phase   string       `json:"phase"` // "before" | "after"
	SHA     string       `json:"sha,omitempty"`
	NoCache bool         `json:"no_cache"`
	Push    bool         `json:"push"`
	DryRun  bool         `json:"dry_run"`
	Images  []BuildImage `json:"images"`
	Outcome string       `json:"outcome,omitempty"` // after: "success" | "failed"
	Error   string       `json:"error,omitempty"`
}

// BuildImage is one image in a build payload.
type BuildImage struct {
	Challenge string            `json:"challenge"`
	Category  string            `json:"category"`
	Slug      string            `json:"slug"`
	Tags      []string          `json:"tags"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// InstanceSpawnPayload is the JSON body for the on_instance_spawn hook.
type InstanceSpawnPayload struct {
	Event         string      `json:"event"`
	InstanceID    string      `json:"instance_id"`
	ChallengeSlug string      `json:"challenge_slug"`
	TeamID        string      `json:"team_id"`
	Ports         map[int]int `json:"ports,omitempty"`
	Flag          string      `json:"flag,omitempty"`
	Containers    []string    `json:"containers,omitempty"`
	ExpiresAt     time.Time   `json:"expires_at"`
}

// HealthFailurePayload is the JSON body for the on_health_failure hook.
type HealthFailurePayload struct {
	Event         string    `json:"event,omitempty"`
	InstanceID    string    `json:"instance_id"`
	ChallengeSlug string    `json:"challenge_slug"`
	Container     string    `json:"container"`
	Status        string    `json:"status"`
	Timestamp     time.Time `json:"timestamp"`
}
