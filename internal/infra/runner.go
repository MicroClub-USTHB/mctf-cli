package infra

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// BinEnv overrides the Terraform binary (e.g. MCTF_TERRAFORM_BIN=tofu).
const BinEnv = "MCTF_TERRAFORM_BIN"

// Runner drives Terraform in the rendered working directory. Every
// operation runs `terraform init` first; init is idempotent and cheap
// once the providers are cached.
type Runner interface {
	Plan(ctx context.Context, dir string) error
	Apply(ctx context.Context, dir string, autoApprove bool) error
	Destroy(ctx context.Context, dir string, autoApprove bool) error
}

// initArgs / planArgs / applyArgs / destroyArgs centralize the terraform
// invocations so the real and dry runners stay in lockstep. apply and
// destroy keep input enabled so Terraform can ask for confirmation
// unless autoApprove is set.
func initArgs() []string { return []string{"init", "-input=false"} }

func planArgs() []string { return []string{"plan", "-input=false"} }

func applyArgs(autoApprove bool) []string {
	args := []string{"apply"}
	if autoApprove {
		args = append(args, "-auto-approve")
	}
	return args
}

func destroyArgs(autoApprove bool) []string {
	args := []string{"destroy"}
	if autoApprove {
		args = append(args, "-auto-approve")
	}
	return args
}

// Bin returns the Terraform binary name: $MCTF_TERRAFORM_BIN or "terraform".
func Bin() string {
	if b := os.Getenv(BinEnv); b != "" {
		return b
	}
	return "terraform"
}

// TerraformRunner shells out to the terraform binary.
type TerraformRunner struct {
	Bin    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// NewTerraformRunner returns a runner wired to the process's stdio. It
// fails with an actionable error when the binary is not installed.
func NewTerraformRunner() (*TerraformRunner, error) {
	bin := Bin()
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%s not found on PATH — install Terraform (https://developer.hashicorp.com/terraform/install), set %s to an alternative such as tofu, or use --dry-run to only render the configuration", bin, BinEnv)
	}
	return &TerraformRunner{Bin: bin, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}, nil
}

// Plan runs `terraform init` then `terraform plan`.
func (r *TerraformRunner) Plan(ctx context.Context, dir string) error {
	return r.run(ctx, dir, initArgs(), planArgs())
}

// Apply runs `terraform init` then `terraform apply`.
func (r *TerraformRunner) Apply(ctx context.Context, dir string, autoApprove bool) error {
	return r.run(ctx, dir, initArgs(), applyArgs(autoApprove))
}

// Destroy runs `terraform init` then `terraform destroy`.
func (r *TerraformRunner) Destroy(ctx context.Context, dir string, autoApprove bool) error {
	return r.run(ctx, dir, initArgs(), destroyArgs(autoApprove))
}

func (r *TerraformRunner) run(ctx context.Context, dir string, steps ...[]string) error {
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, r.Bin, args...)
		cmd.Dir = dir
		cmd.Stdin = r.Stdin
		cmd.Stdout = r.Stdout
		cmd.Stderr = r.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s %s: %w", r.Bin, args[0], err)
		}
	}
	return nil
}

// DryRunner prints what would run instead of running it.
type DryRunner struct {
	Bin    string
	Stdout io.Writer
}

// NewDryRunner returns a runner that prints to stdout.
func NewDryRunner() *DryRunner { return &DryRunner{Bin: Bin(), Stdout: os.Stdout} }

// Plan prints the would-be init + plan invocations.
func (d *DryRunner) Plan(_ context.Context, dir string) error {
	d.print(dir, initArgs(), planArgs())
	return nil
}

// Apply prints the would-be init + apply invocations.
func (d *DryRunner) Apply(_ context.Context, dir string, autoApprove bool) error {
	d.print(dir, initArgs(), applyArgs(autoApprove))
	return nil
}

// Destroy prints the would-be init + destroy invocations.
func (d *DryRunner) Destroy(_ context.Context, dir string, autoApprove bool) error {
	d.print(dir, initArgs(), destroyArgs(autoApprove))
	return nil
}

func (d *DryRunner) print(dir string, steps ...[]string) {
	for _, args := range steps {
		fmt.Fprintf(d.Stdout, "(cd %s; %s %s)\n", dir, d.Bin, strings.Join(args, " "))
	}
}
