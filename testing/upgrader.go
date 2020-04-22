// Package testing provides a stub implementation that can be used for
// simplified testing of applications that normally use tableflip.
// It is also helpful for allowing projects that use tableflip
// able to run on Windows, which does not support tableflip.
package testing

import (
	"context"

	"github.com/cloudflare/tableflip"
)

// Upgrader has all the methods of tableflip.Upgrader, but they don't
// actually do anything special.
type Upgrader struct {
	*Fds
}

// New creates a new stub Upgrader.
//
// Unlike the real version, this can be called many times.
func New() (*Upgrader, error) {
	upg := newStubUpgrader()

	return upg, nil
}

func newStubUpgrader() *Upgrader {
	return &Upgrader{
		&Fds{},
	}
}

// Ready does nothing, since it is impossible to inherit with
// the stub implementation.
// However, the function still needs to be callable without errors
// in order to be useful.
func (u *Upgrader) Ready() error {
	return nil
}

// Exit returns a channel which is closed when the process should
// exit.
// We can return nil here because reading from a nil channel blocks
func (u *Upgrader) Exit() <-chan struct{} {
	return nil
}

// Stop does nothing, since there will never be anything to stop
// in the stub implementation
func (u *Upgrader) Stop() {
}

// WaitForParent returns immediately, since the stub implementation
// can never be a parent
func (u *Upgrader) WaitForParent(ctx context.Context) error {
	return nil
}

// HasParent is always false, since the stub implementation can never
// have a parent
func (u *Upgrader) HasParent() bool {
	return false
}

func (u *Upgrader) Upgrade() error {
	return tableflip.ErrNotSupported
}
