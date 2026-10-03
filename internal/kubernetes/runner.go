package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// KubeRunner applies and deletes K8s manifests by shelling out to kubectl.
type KubeRunner struct {
	Kubeconfig        string
	Stdout            io.Writer
	Stderr            io.Writer
	ensuredNamespaces map[string]bool
	mu                sync.Mutex
}

// NewKubeRunner returns a runner that streams kubectl output to os.Stdout/Stderr.
func NewKubeRunner(kubeconfig string) *KubeRunner {
	return &KubeRunner{
		Kubeconfig:        kubeconfig,
		Stdout:            os.Stdout,
		Stderr:            os.Stderr,
		ensuredNamespaces: make(map[string]bool),
	}
}

// Up generates manifests and applies them via `kubectl apply -f -`.
func (r *KubeRunner) Up(ctx context.Context, u KubeUnit) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH — please install kubectl to use kubernetes deploy mode")
	}

	if err := r.ensureNamespace(ctx, u.Namespace); err != nil {
		return fmt.Errorf("ensure namespace %s: %w", u.Namespace, err)
	}

	manifests, err := ManifestsYAML(u)
	if err != nil {
		return err
	}
	if err := r.kubectl(ctx, manifests, "apply", "-f", "-"); err != nil {
		return err
	}

	return r.kubectl(ctx, nil, "rollout", "status", "deployment/"+u.Name, "-n", u.Namespace, "--timeout=120s")
}

func (r *KubeRunner) ensureNamespace(ctx context.Context, ns string) error {
	if ns == "" || ns == "default" {
		return nil
	}
	r.mu.Lock()
	if r.ensuredNamespaces == nil {
		r.ensuredNamespaces = make(map[string]bool)
	}
	if r.ensuredNamespaces[ns] {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	// Check if namespace already exists
	checkCmd := exec.CommandContext(ctx, "kubectl", "get", "namespace", ns)
	if r.Kubeconfig != "" {
		checkCmd.Args = append([]string{"--kubeconfig", r.Kubeconfig}, checkCmd.Args...)
	}
	if err := checkCmd.Run(); err == nil {
		r.mu.Lock()
		r.ensuredNamespaces[ns] = true
		r.mu.Unlock()
		return nil
	}

	// Create namespace
	if err := r.kubectl(ctx, nil, "create", "namespace", ns); err != nil {
		return err
	}

	r.mu.Lock()
	r.ensuredNamespaces[ns] = true
	r.mu.Unlock()
	return nil
}

// Down deletes all resources for the unit by exact name.
func (r *KubeRunner) Down(ctx context.Context, u KubeUnit) error {
	return r.kubectl(ctx, nil,
		"delete", "deployment,service,ingress",
		u.Name,
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
		"kubectl delete deployment,service,ingress %s -n %s --ignore-not-found\n",
		u.Name, u.Namespace)
	return nil
}
