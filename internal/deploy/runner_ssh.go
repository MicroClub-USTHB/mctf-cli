package deploy

import (
	"context"
	"fmt"
)

type SSHRunner struct{}

func NewSSHRunner() *SSHRunner {
	return &SSHRunner{}
}

func (r *SSHRunner) Up(_ context.Context, u Unit) error {
	return fmt.Errorf("%s: ssh deploy mode not implemented yet", u.Challenge)
}

func (r *SSHRunner) Down(_ context.Context, u Unit, _ bool) error {
	return fmt.Errorf("%s: ssh deploy mode not implemented yet", u.Challenge)
}
