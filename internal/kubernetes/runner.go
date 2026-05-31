package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// KubeRunner applies and deletes K8s manifests by shelling out to kubectl.
type KubeRunner struct {
	Kubeconfig string
	Stdout     io.Writer
	Stderr     io.Writer
}

// NewKubeRunner returns a runner that streams kubectl output to os.Stdout/Stderr.
func NewKubeRunner(kubeconfig string) *KubeRunner {
	return &KubeRunner{Kubeconfig: kubeconfig, Stdout: os.Stdout, Stderr: os.Stderr}
}

// Up generates manifests and applies them via `kubectl apply -f -`.
func (r *KubeRunner) Up(ctx context.Context, u KubeUnit) error {
	manifests, err := ManifestsYAML(u)
	if err != nil {
		return err
	}
	return r.kubectl(ctx, manifests, "apply", "-f", "-")
}

// Down deletes all resources for the unit by label selector.
func (r *KubeRunner) Down(ctx context.Context, u KubeUnit) error {
	return r.kubectl(ctx, nil,
		"delete", "deployment,service,ingress",
		"-l", "app="+u.Name,
		"-n", u.Namespace,
		"--ignore-not-found",
	)
}

func (r *KubeRunner) kubectl(ctx context.Context, stdin []byte, args ...string) error {
	if r.Kubeconfig != "" {
		args = append([]string{"--kubeconfig", r.Kubeconfig}, args...)
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.Run()
}

// DryKubeRunner prints what kubectl would do instead of running it.
type DryKubeRunner struct {
	Stdout io.Writer
}

// NewDryKubeRunner returns a runner that prints to os.Stdout.
func NewDryKubeRunner() *DryKubeRunner { return &DryKubeRunner{Stdout: os.Stdout} }

// Up prints the manifests that would be applied.
func (d *DryKubeRunner) Up(_ context.Context, u KubeUnit) error {
	manifests, err := ManifestsYAML(u)
	if err != nil {
		return err
	}
	fmt.Fprintf(d.Stdout, "# kubectl apply -f - (dry-run: %s)\n%s\n", u.Challenge, manifests)
	return nil
}

// Down prints the kubectl delete command that would be executed.
func (d *DryKubeRunner) Down(_ context.Context, u KubeUnit) error {
	fmt.Fprintf(d.Stdout,
		"kubectl delete deployment,service,ingress -l app=%s -n %s --ignore-not-found\n",
		u.Name, u.Namespace)
	return nil
}