package plugin

import (
	"encoding/json"
	"fmt"
)

// Config is one plugin entry from mctf.config.yml `plugins:`.
type Config struct {
	Name     string         `json:"name"`
	Command  string         `json:"command"`
	Args     []string       `json:"args,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty"` // nil => enabled
	Required bool           `json:"required,omitempty"`
	Config   map[string]any `json:"config,omitempty"` // opaque, passed to the plugin
}

func (c Config) isEnabled() bool { return c.Enabled == nil || *c.Enabled }

// ParseConfigs converts the untyped cfg.Plugins ([]map[string]any) into typed
// Config. It round-trips through JSON so it stays in lockstep with the schema
// without a hand-written decoder.
func ParseConfigs(raw []map[string]any) ([]Config, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal plugins config: %w", err)
	}
	var out []Config
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode plugins config: %w", err)
	}
	for i, c := range out {
		if c.Command == "" {
			return nil, fmt.Errorf("plugins[%d]: command is required", i)
		}
		if c.Name == "" {
			out[i].Name = c.Command
		}
	}
	return out, nil
}
