package plugin

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
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
	dispatchMu sync.Mutex
	plugins    []*Plugin
	opts       Options
	log        io.Writer
	procCancel context.CancelFunc // cancels every plugin's process on Close
}

// Load parses raw plugin config, starts each enabled plugin, and runs the
// handshake. It is best-effort and crash-isolated: a plugin that fails to
// parse, start, or handshake is logged and excluded, never aborting the host
// command. Returns nil when no plugin is live.
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
			continue
		}
		hctx, cancel := context.WithTimeout(ctx, opts.StartTimeout)
		err := p.handshake(hctx, opts.HostVersion)
		cancel()
		if err != nil {
			fmt.Fprintf(log, "warning: plugin %q handshake failed: %v\n", c.Name, err)
			p.markDeadAndKill()
			continue
		}
		m.plugins = append(m.plugins, p)
	}
	if len(m.plugins) == 0 {
		procCancel()
		return nil
	}
	return m
}

// Dispatch sends a hook to every subscribing plugin, sequentially, each under
// its own DispatchTimeout. A plugin that errors, crashes, or times out is
// logged and dropped from future dispatch. It never returns an error.
func (m *Manager) Dispatch(ctx context.Context, hook string, payload any) {
	if m == nil {
		return
	}

	m.dispatchMu.Lock()
	defer m.dispatchMu.Unlock()

	live := m.plugins[:0]
	for _, p := range m.plugins {
		dctx, cancel := context.WithTimeout(ctx, m.opts.DispatchTimeout)
		err := p.dispatch(dctx, hook, payload)
		cancel()
		if err != nil {
			fmt.Fprintf(m.log, "warning: plugin %q hook %q: %v\n", p.name, hook, err)
			continue // drop it
		}
		live = append(live, p)
	}
	m.plugins = live
}

// Close shuts every plugin down and reaps it. Safe to call on a nil Manager.
func (m *Manager) Close() {
	if m == nil {
		return
	}

	m.dispatchMu.Lock()
	defer m.dispatchMu.Unlock()

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
