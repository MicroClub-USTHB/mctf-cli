package instancer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/plugin"
)

// ContainerHealth contains the health information Docker reports for a container.
type ContainerHealth struct {
	ID     string
	Name   string
	Status string
}

// HealthMonitor periodically checks instance containers and dispatches a hook
// when a container first becomes unhealthy.
type HealthMonitor struct {
	Store         *Store
	Interval      time.Duration
	Event         string
	PluginManager *plugin.Manager
	Logger        io.Writer

	// Inspect is replaceable in tests so they do not need Docker.
	Inspect func(context.Context, string) (ContainerHealth, error)

	// Dispatch overrides PluginManager.Dispatch in tests.
	Dispatch func(context.Context, string, any)

	mu        sync.Mutex
	unhealthy map[string]bool
}

// Run checks health immediately, then repeats until the context is cancelled.
func (m *HealthMonitor) Run(ctx context.Context) error {
	if m.Store == nil {
		return fmt.Errorf("health monitor: store is nil")
	}

	if m.Interval <= 0 {
		m.Interval = 30 * time.Second
	}
	if m.Logger == nil {
		m.Logger = os.Stdout
	}
	if m.Inspect == nil {
		m.Inspect = inspectContainerHealth
	}
	if m.unhealthy == nil {
		m.unhealthy = make(map[string]bool)
	}

	ticker := time.NewTicker(m.Interval)
	defer ticker.Stop()

	if err := m.Tick(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(m.Logger, "health monitor: initial check: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := m.Tick(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(m.Logger, "health monitor: check: %v\n", err)
			}
		}
	}
}

// Tick checks all containers belonging to currently stored instances.
func (m *HealthMonitor) Tick(ctx context.Context) error {
	if m.Store == nil {
		return fmt.Errorf("health monitor: store is nil")
	}
	if m.Inspect == nil {
		m.Inspect = inspectContainerHealth
	}
	if m.Logger == nil {
		m.Logger = os.Stdout
	}
	if m.unhealthy == nil {
		m.unhealthy = make(map[string]bool)
	}

	instances := m.Store.List()
	active := make(map[string]bool)

	for _, inst := range instances {
		for _, containerID := range inst.Containers {
			if containerID == "" {
				continue
			}
			active[containerID] = true

			health, err := m.Inspect(ctx, containerID)
			if err != nil {
				fmt.Fprintf(m.Logger, "health monitor: inspect %s: %v\n", containerID, err)
				continue
			}

			m.mu.Lock()
			wasUnhealthy := m.unhealthy[containerID]

			if health.Status != "unhealthy" {
				// A recovery (or a container without a health check) resets
				// the transition so a later failure can trigger again.
				delete(m.unhealthy, containerID)
				m.mu.Unlock()
				continue
			}

			if wasUnhealthy {
				m.mu.Unlock()
				continue
			}
			m.unhealthy[containerID] = true
			m.mu.Unlock()

			payload := plugin.HealthFailurePayload{
				Event:         m.Event,
				InstanceID:    inst.ID,
				ChallengeSlug: inst.ChallengeSlug,
				Container:     containerID,
				Status:        health.Status,
				Timestamp:     time.Now().UTC(),
			}

			if m.Dispatch != nil {
				m.Dispatch(ctx, plugin.HookOnHealthFailure, payload)
			} else if m.PluginManager != nil {
				m.PluginManager.Dispatch(ctx, plugin.HookOnHealthFailure, payload)
			}
		}
	}

	// Drop remembered states for containers no longer tracked by the store.
	m.mu.Lock()
	for id := range m.unhealthy {
		if !active[id] {
			delete(m.unhealthy, id)
		}
	}
	m.mu.Unlock()

	return nil
}

// inspectContainerHealth reads Docker's structured health state.
// Containers without a HEALTHCHECK have an empty status and do not trigger
// on_health_failure.
func inspectContainerHealth(ctx context.Context, id string) (ContainerHealth, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", id)
	raw, err := cmd.Output()
	if err != nil {
		return ContainerHealth{}, fmt.Errorf("docker inspect: %w", err)
	}

	var containers []struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Health *struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
	}
	if err := json.Unmarshal(raw, &containers); err != nil {
		return ContainerHealth{}, fmt.Errorf("decode docker inspect output: %w", err)
	}
	if len(containers) == 0 {
		return ContainerHealth{}, fmt.Errorf("docker inspect returned no container for %s", id)
	}

	container := containers[0]
	result := ContainerHealth{
		ID:   container.ID,
		Name: container.Name,
	}
	if container.State.Health != nil {
		result.Status = container.State.Health.Status
	}
	return result, nil
}
