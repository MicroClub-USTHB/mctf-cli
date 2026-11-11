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

func TestParseDockerInspect(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		id         string
		wantStatus string
		wantErr    bool
	}{
		{
			name: "unhealthy container",
			raw: `[
				{
					"Id": "c1",
					"Name": "/test-c1",
					"State": {
						"Health": {"Status": "unhealthy"}
					}
				}
			]`,
			id:         "c1",
			wantStatus: "unhealthy",
		},
		{
			name: "healthy container",
			raw: `[
				{
					"Id": "c2",
					"Name": "/test-c2",
					"State": {
						"Health": {"Status": "healthy"}
					}
				}
			]`,
			id:         "c2",
			wantStatus: "healthy",
		},
		{
			name: "no healthcheck defined",
			raw: `[
				{
					"Id": "c3",
					"Name": "/test-c3",
					"State": {}
				}
			]`,
			id:         "c3",
			wantStatus: "",
		},
		{
			name:    "empty array",
			raw:     `[]`,
			id:      "c4",
			wantErr: true,
		},
		{
			name:    "malformed json",
			raw:     `{not json}`,
			id:      "c5",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health, err := parseDockerInspect([]byte(tt.raw), tt.id)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseDockerInspect() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && health.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", health.Status, tt.wantStatus)
			}
		})
	}
}
