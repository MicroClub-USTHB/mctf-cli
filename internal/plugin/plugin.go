package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// Plugin is one running plugin subprocess. Exactly one request is in flight at
// a time (serialized by mu), so responses need no correlation beyond an id check.
type Plugin struct {
	cfg   Config
	name  string
	hooks map[string]struct{}
	log   io.Writer

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu     sync.Mutex
	nextID int
	dead   bool

	// newCmd is the exec seam (mirrors SSHRunner.runSSHFn); tests swap it to
	// run the test binary as a plugin. Defaults to exec.CommandContext.
	newCmd func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func newPlugin(cfg Config, log io.Writer) *Plugin {
	return &Plugin{
		cfg:    cfg,
		name:   cfg.Name,
		log:    log,
		newCmd: exec.CommandContext,
	}
}

// start launches the subprocess and wires its stdio. Its own stderr is
// forwarded to the host log (exec drains it, so no backpressure deadlock).
func (p *Plugin) start(ctx context.Context, dir string) error {
	cmd := p.newCmd(ctx, p.cfg.Command, p.cfg.Args...)
	cmd.Dir = dir
	cmd.Stderr = p.log
	cmd.WaitDelay = 500 * time.Millisecond // bound orphaned-pipe reaping

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	p.stdin = stdin
	p.stdout = bufio.NewReader(stdout)
	return nil
}

// handshake runs the initialize exchange and records the plugin's name and
// the hooks it subscribes to.
func (p *Plugin) handshake(ctx context.Context, hostVersion string) error {
	res, err := p.call(ctx, methodInitialize, initParams{
		ProtocolVersion: protocolVersion,
		MCTFVersion:     hostVersion,
		Config:          p.cfg.Config,
	})
	if err != nil {
		return err
	}
	var ir initResult
	if err := json.Unmarshal(res, &ir); err != nil {
		return fmt.Errorf("decode initialize result: %w", err)
	}
	if ir.Name != "" {
		p.name = ir.Name
	}
	p.hooks = make(map[string]struct{}, len(ir.Hooks))
	for _, h := range ir.Hooks {
		p.hooks[h] = struct{}{}
	}
	return nil
}

func (p *Plugin) subscribed(hook string) bool {
	_, ok := p.hooks[hook]
	return ok
}

// dispatch sends a hook to the plugin when it subscribes; a no-op otherwise.
func (p *Plugin) dispatch(ctx context.Context, hook string, payload any) error {
	if !p.subscribed(hook) {
		return nil
	}
	_, err := p.call(ctx, hook, payload)
	return err
}

// call writes one request and waits for the matching response, honoring ctx:
// the blocking line-read runs in a goroutine so a hung plugin can be canceled.
func (p *Plugin) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead {
		return nil, errors.New("plugin is not running")
	}

	p.nextID++
	id := p.nextID

	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		raw = b
	}
	line, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: raw})
	if err != nil {
		return nil, err
	}
	if _, err := p.stdin.Write(append(line, '\n')); err != nil {
		p.dead = true
		p.killLocked()
		return nil, fmt.Errorf("write request: %w", err)
	}

	type readResult struct {
		line string
		err  error
	}
	// ReadString, not bufio.Scanner: a Scanner's token cap would silently
	// truncate a large payload.
	ch := make(chan readResult, 1)
	go func() { s, err := p.stdout.ReadString('\n'); ch <- readResult{s, err} }()

	select {
	case <-ctx.Done():
		p.dead = true
		p.killLocked()
		return nil, ctx.Err()
	case rr := <-ch:
		if rr.err != nil {
			p.dead = true
			return nil, fmt.Errorf("read response: %w", rr.err)
		}
		var resp response
		if err := json.Unmarshal([]byte(rr.line), &resp); err != nil {
			p.dead = true
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if resp.ID != id {
			p.dead = true
			return nil, fmt.Errorf("response id %d does not match request id %d", resp.ID, id)
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// shutdown asks the plugin to exit, closes its stdin (EOF cue), and reaps it.
func (p *Plugin) shutdown(ctx context.Context) {
	if !p.isDead() {
		_, _ = p.call(ctx, methodShutdown, nil) // best-effort
	}
	p.mu.Lock()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	p.mu.Unlock()
	if p.cmd != nil {
		_ = p.cmd.Wait() // WaitDelay bounds this
	}
}

func (p *Plugin) isDead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dead
}

// killLocked terminates the process; the caller holds mu.
func (p *Plugin) killLocked() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

// markDeadAndKill marks the plugin dead and terminates it (acquires mu).
func (p *Plugin) markDeadAndKill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dead = true
	p.killLocked()
}
