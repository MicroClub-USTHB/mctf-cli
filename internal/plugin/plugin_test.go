package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is not a real test: when invoked by a plugin test with
// MCTF_PLUGIN_HELPER=1, the test binary behaves as an NDJSON plugin, scripted
// by MCTF_PLUGIN_MODE. This is Go's standard os/exec self-exec idiom.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("MCTF_PLUGIN_HELPER") != "1" {
		return
	}
	mode := os.Getenv("MCTF_PLUGIN_MODE")
	outfile := os.Getenv("MCTF_PLUGIN_OUTFILE")

	in := bufio.NewReader(os.Stdin)
	reply := func(id int, result any) {
		raw, _ := json.Marshal(result)
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": json.RawMessage(raw)})
		fmt.Fprintln(os.Stdout, string(b))
	}

	for {
		line, err := in.ReadString('\n')
		if err != nil {
			os.Exit(0)
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(line), &req) != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			if mode == "badhandshake" {
				fmt.Fprintln(os.Stdout, "this is not json") // desync the stream
				continue
			}
			hooks := []string{"before_deploy", "after_deploy", "before_build", "after_build"}
			if mode == "nohooks" {
				hooks = nil
			}
			reply(req.ID, map[string]any{"name": "helper-" + mode, "hooks": hooks})
		case "shutdown":
			os.Exit(0)
		case "before_deploy", "after_deploy", "before_build", "after_build":
			switch mode {
			case "crash":
				os.Exit(1)
			case "timeout":
				time.Sleep(30 * time.Second)
			default:
				if outfile != "" {
					f, _ := os.OpenFile(outfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
					fmt.Fprintf(f, "%s %s\n", req.Method, strings.TrimSpace(string(req.Params)))
					_ = f.Close()
				}
				reply(req.ID, map[string]any{"ok": true})
			}
		default:
			reply(req.ID, map[string]any{})
		}
	}
}

// helperManager starts a Manager whose single plugin is the test binary
// running as TestHelperProcess in the given mode.
func helperManager(t *testing.T, mode, outfile string) *Manager {
	t.Helper()
	newCmd := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(),
			"MCTF_PLUGIN_HELPER=1",
			"MCTF_PLUGIN_MODE="+mode,
			"MCTF_PLUGIN_OUTFILE="+outfile,
		)
		return cmd
	}
	raw := []map[string]any{{
		"name":    "helper",
		"command": os.Args[0],
		"args":    []any{"-test.run=TestHelperProcess"},
	}}
	return Load(context.Background(), raw, Options{
		HostVersion:     "test",
		DispatchTimeout: 2 * time.Second,
		StartTimeout:    5 * time.Second,
		Log:             os.Stderr,
		newCmd:          newCmd,
	})
}

func TestManager_HandshakeAndDispatch(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hooks.log")
	m := helperManager(t, "echo", out)
	if m == nil {
		t.Fatal("manager should have one live plugin")
	}
	defer m.Close()

	ctx := context.Background()
	m.Dispatch(ctx, HookBeforeDeploy, DeployPayload{Event: "e", Phase: "before", Mode: "docker"})
	m.Dispatch(ctx, HookAfterDeploy, DeployPayload{Event: "e", Phase: "after", Outcome: "success"})

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read hooks log: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "before_deploy") || !strings.Contains(s, `"phase":"before"`) {
		t.Errorf("before_deploy payload not delivered:\n%s", s)
	}
	if !strings.Contains(s, "after_deploy") || !strings.Contains(s, `"outcome":"success"`) {
		t.Errorf("after_deploy payload not delivered:\n%s", s)
	}
}

func TestManager_BuildHooksDelivered(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build_hooks.log")
	m := helperManager(t, "echo", out)
	if m == nil {
		t.Fatal("manager should have one live plugin")
	}
	defer m.Close()

	ctx := context.Background()
	m.Dispatch(ctx, HookBeforeBuild, BuildPayload{
		Event: "test-event",
		Phase: "before",
		SHA:   "abc1234",
		Images: []BuildImage{
			{
				Challenge: "web/cub3-clone",
				Category:  "web",
				Slug:      "cub3-clone",
				Tags:      []string{"registry.test/web:abc1234", "registry.test/web:latest"},
				Labels:    map[string]string{"mctf.challenge": "web/cub3-clone"},
			},
		},
	})
	m.Dispatch(ctx, HookAfterBuild, BuildPayload{
		Event:   "test-event",
		Phase:   "after",
		SHA:     "abc1234",
		Outcome: "success",
	})

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read hooks log: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "before_build") || !strings.Contains(s, `"web/cub3-clone"`) {
		t.Errorf("before_build payload not delivered:\n%s", s)
	}
	if !strings.Contains(s, "after_build") || !strings.Contains(s, `"outcome":"success"`) {
		t.Errorf("after_build payload not delivered:\n%s", s)
	}
}

func TestManager_UnsubscribedHookNotDelivered(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hooks.log")
	m := helperManager(t, "nohooks", out)
	if m == nil {
		t.Fatal("plugin subscribing to no hooks should still be live")
	}
	defer m.Close()

	m.Dispatch(context.Background(), HookBeforeDeploy, DeployPayload{Phase: "before"})
	if _, err := os.Stat(out); err == nil {
		t.Error("hook was delivered to a plugin that did not subscribe")
	}
}

func TestManager_CrashingPluginIsDroppedAndHostContinues(t *testing.T) {
	m := helperManager(t, "crash", "")
	if m == nil {
		t.Fatal("manager should start (crash happens on first hook)")
	}
	defer m.Close()

	// First dispatch crashes the plugin; must not panic/abort.
	m.Dispatch(context.Background(), HookBeforeDeploy, DeployPayload{Phase: "before"})
	// Plugin should now be dropped — second dispatch is a clean no-op.
	m.Dispatch(context.Background(), HookAfterDeploy, DeployPayload{Phase: "after"})
	if len(m.plugins) != 0 {
		t.Errorf("crashed plugin should have been dropped, %d remain", len(m.plugins))
	}
}

func TestManager_HangingPluginTimesOutAndIsDropped(t *testing.T) {
	m := helperManager(t, "timeout", "")
	if m == nil {
		t.Fatal("manager should start (hang happens on first hook)")
	}
	defer m.Close()

	start := time.Now()
	m.Dispatch(context.Background(), HookBeforeDeploy, DeployPayload{Phase: "before"})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("dispatch did not time out promptly: %v", elapsed)
	}
	if len(m.plugins) != 0 {
		t.Errorf("timed-out plugin should have been dropped, %d remain", len(m.plugins))
	}
}

func TestManager_BadHandshakeExcludesPlugin(t *testing.T) {
	if m := helperManager(t, "badhandshake", ""); m != nil {
		t.Errorf("plugin with a broken handshake should be excluded (got live manager)")
		m.Close()
	}
}

func TestManager_DisabledPluginNotStarted(t *testing.T) {
	disabled := false
	raw := []map[string]any{{
		"name":    "off",
		"command": "/nonexistent/should-never-run",
		"enabled": disabled,
	}}
	if m := Load(context.Background(), raw, Options{Log: os.Stderr}); m != nil {
		t.Errorf("disabled plugin should not start")
		m.Close()
	}
}

func TestManager_NilIsSafe(t *testing.T) {
	var m *Manager
	m.Dispatch(context.Background(), HookBeforeDeploy, nil) // must not panic
	m.Close()
}
