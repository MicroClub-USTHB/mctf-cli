package plugin

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Options configures a plugin Manager.
type Options struct {
	HostVersion     string        // MCTF version reported in the handshake
	Dir             string        // event repo root; each plugin runs with this cwd
	StartTimeout    time.Duration // handshake budget (default 5s)
	DispatchTimeout time.Duration // per-hook budget (default 10s)
	Log             io.Writer     // warnings + plugin stderr (default os.Stderr)

	// newCmd is a test seam (mirrors SSHRunner.runSSHFn); nil uses
	// exec.CommandContext.
	newCmd func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// Manager owns a set of running plugins and fans lifecycle hooks out to them.
// A nil *Manager is valid and all methods no-op, so call sites don't branch
// when no plugins are configured.
type Manager struct {
	plugins    []*Plugin
	opts       Options
	log        io.Writer
	procCancel context.CancelFunc // cancels every plugin's process on Close
	loadErr    error
}

// Load parses raw plugin config, starts each enabled plugin, and runs the
// handshake. Optional plugin failures are logged and excluded without
// aborting the host command. Required plugin failures are retained and
// returned by Dispatch so they can block the host operation.
// Returns nil when no plugin is live and no required plugin has failed.
func Load(ctx context.Context, raw []map[string]any, opts Options) *Manager {
	log := opts.Log
	if log == nil {
		log = os.Stderr
	}
	if opts.StartTimeout == 0 {
		opts.StartTimeout = 5 * time.Second
	}
	if opts.DispatchTimeout == 0 {
		opts.DispatchTimeout = 10 * time.Second
	}

	cfgs, err := ParseConfigs(raw)
	if err != nil {
		fmt.Fprintf(log, "warning: plugins disabled: %v\n", err)

		// Preserve required-plugin configuration errors so Dispatch can
		// prevent the host operation from proceeding.
		for _, entry := range raw {
			required, _ := entry["required"].(bool)
			if required {
				return &Manager{
					opts:    opts,
					log:     log,
					loadErr: fmt.Errorf("required plugin configuration is invalid: %w", err),
				}
			}
		}

		return nil
	}

	// Process lifetime is the manager's, canceled in Close — NOT the
	// short-lived handshake context (which would kill the process the moment
	// the handshake returns). Per-call timeouts bound I/O without killing.
	procCtx, procCancel := context.WithCancel(ctx)
	m := &Manager{opts: opts, log: log, procCancel: procCancel}
	for _, c := range cfgs {
		if !c.isEnabled() {
			continue
		}
		p := newPlugin(c, log)
		if opts.newCmd != nil {
			p.newCmd = opts.newCmd
		}
		if err := p.start(procCtx, opts.Dir); err != nil {
			fmt.Fprintf(log, "warning: plugin %q failed to start: %v\n", c.Name, err)
			if c.Required && m.loadErr == nil {
				m.loadErr = fmt.Errorf("required plugin %q failed to start: %w", c.Name, err)
			}
			continue
		}
		hctx, cancel := context.WithTimeout(ctx, opts.StartTimeout)
		err := p.handshake(hctx, opts.HostVersion)
		cancel()
		if err != nil {
			fmt.Fprintf(log, "warning: plugin %q handshake failed: %v\n", c.Name, err)
			p.markDeadAndKill()
			if c.Required && m.loadErr == nil {
				m.loadErr = fmt.Errorf("required plugin %q handshake failed: %w", c.Name, err)
			}
			continue
		}
		m.plugins = append(m.plugins, p)
	}
	if len(m.plugins) == 0 && m.loadErr == nil {
		procCancel()
		return nil
	}
	return m
}

// Dispatch sends a hook to every subscribing plugin, sequentially, each under
// its own DispatchTimeout. Optional plugin failures are logged and dropped.
// Failures from required plugins are also returned so blocking hooks can veto
// the host operation.
func (m *Manager) Dispatch(ctx context.Context, hook string, payload any) error {
	if m == nil {
		return nil
	}

	if m.loadErr != nil {
		return m.loadErr
	}

	var requiredErr error
	live := m.plugins[:0]

	for _, p := range m.plugins {
		dctx, cancel := context.WithTimeout(ctx, m.opts.DispatchTimeout)
		err := p.dispatch(dctx, hook, payload)
		cancel()

		if err != nil {
			fmt.Fprintf(m.log, "warning: plugin %q hook %q: %v\n", p.name, hook, err)
			if p.cfg.Required && requiredErr == nil {
				requiredErr = fmt.Errorf(
					"required plugin %q failed on hook %q: %w",
					p.name, hook, err,
				)
			}
			continue
		}

		live = append(live, p)
	}

	m.plugins = live
	return requiredErr
}

// Close shuts every plugin down and reaps it. Safe to call on a nil Manager.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	for _, p := range m.plugins {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		p.shutdown(ctx)
		cancel()
	}
	m.plugins = nil
	if m.procCancel != nil {
		m.procCancel() // reap any straggler processes
	}
}
