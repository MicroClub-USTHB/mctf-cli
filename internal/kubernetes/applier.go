package kubernetes

import (
	"context"
	"fmt"
)

// KubeApplier applies or removes K8s resources for one KubeUnit
type KubeApplier interface {
	Up(ctx context.Context, u KubeUnit) error
	Down(ctx context.Context, u KubeUnit) error
}

// ApplyUp calls Up on each KubeUnit in plan order
func ApplyUp(ctx context.Context, r KubeApplier, plan []KubeUnit) error {
	for _, u := range plan {
		if err := r.Up(ctx, u); err != nil {
			return fmt.Errorf("%s: up failed: %w", u.Challenge, err)
		}
	}
	return nil
}

// ApplyDown calls Down on each KubeUnit in reverse plan order
func ApplyDown(ctx context.Context, r KubeApplier, plan []KubeUnit) error {
	for i := len(plan) - 1; i >= 0; i-- {
		u := plan[i]
		if err := r.Down(ctx, u); err != nil {
			return fmt.Errorf("%s: down failed: %w", u.Challenge, err)
		}
	}
	return nil
}
