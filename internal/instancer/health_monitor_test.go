package instancer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/plugin"
)

func TestHealthMonitor_DispatchesOnUnhealthyTransitions(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "instances.json"))
	if err != nil {
		t.Fatal(err)
	}

	err = store.Put(Instance{
		ID:            "instance-1",
		ChallengeSlug: "web",
		TeamID:        "team-1",
		Containers:    []string{"container-1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	status := "unhealthy"
	var payloads []plugin.HealthFailurePayload

	monitor := &HealthMonitor{
		Store: store,
		Event: "test-event",
		Inspect: func(_ context.Context, id string) (ContainerHealth, error) {
			return ContainerHealth{ID: id, Status: status}, nil
		},
		Dispatch: func(_ context.Context, hook string, payload any) {
			if hook != plugin.HookOnHealthFailure {
				t.Errorf("unexpected hook: %q", hook)
				return
			}

			got, ok := payload.(plugin.HealthFailurePayload)
			if !ok {
				t.Errorf("unexpected payload type: %T", payload)
				return
			}
			payloads = append(payloads, got)
		},
	}

	ctx := context.Background()

	// First unhealthy result should trigger the hook.
	if err := monitor.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 {
		t.Fatalf("after first unhealthy check: got %d hooks, want 1", len(payloads))
	}

	// Repeated unhealthy results must not trigger duplicate hooks.
	if err := monitor.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 {
		t.Fatalf("after repeated unhealthy check: got %d hooks, want 1", len(payloads))
	}

	// Recovery resets the transition state.
	status = "healthy"
	if err := monitor.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 {
		t.Fatalf("after recovery: got %d hooks, want 1", len(payloads))
	}

	// A new unhealthy transition should trigger the hook again.
	status = "unhealthy"
	if err := monitor.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 2 {
		t.Fatalf("after second unhealthy transition: got %d hooks, want 2", len(payloads))
	}

	first := payloads[0]
	if first.Event != "test-event" {
		t.Errorf("Event = %q, want test-event", first.Event)
	}
	if first.InstanceID != "instance-1" {
		t.Errorf("InstanceID = %q, want instance-1", first.InstanceID)
	}
	if first.ChallengeSlug != "web" {
		t.Errorf("ChallengeSlug = %q, want web", first.ChallengeSlug)
	}
	if first.Container != "container-1" {
		t.Errorf("Container = %q, want container-1", first.Container)
	}
	if first.Status != "unhealthy" {
		t.Errorf("Status = %q, want unhealthy", first.Status)
	}
	if first.Timestamp.IsZero() || first.Timestamp.Location() != nil &&
		first.Timestamp.Location().String() != "UTC" {
		t.Errorf("Timestamp = %v, want a non-zero UTC timestamp", first.Timestamp)
	}
}
