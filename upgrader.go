package tableflip

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// DefaultUpgradeTimeout is the duration before the Upgrader kills the new process if no
// readiness notification was received.
const DefaultUpgradeTimeout time.Duration = time.Minute

// Options control the behaviour of the Upgrader.
type Options struct {
	// Time after which an upgrade is considered failed. Defaults to
	// DefaultUpgradeTimeout.
	UpgradeTimeout time.Duration
	// The PID of a ready process is written to this file.
	PIDFile string
}

// Upgrader handles zero downtime upgrades and passing files between processes.
type Upgrader struct {
	*env
	opts       Options
	parent     *parent
	readyOnce  sync.Once
	readyC     chan struct{}
	stopOnce   sync.Once
	stopC      chan struct{}
	upgradeSem chan struct{}
	exitC      chan struct{}      // only close this if holding upgradeSem
	exitFd     neverCloseThisFile // protected by upgradeSem
	parentErr  error              // protected by upgradeSem

	Fds *Fds
}

var (
	stdEnvMu       sync.Mutex
	stdEnvUpgrader *Upgrader
)

// New creates a new Upgrader. Files are passed from the parent and may be empty.
//
// Only the first call to this function will succeed.
func New(opts Options) (upg *Upgrader, err error) {
	stdEnvMu.Lock()
	defer stdEnvMu.Unlock()

	if stdEnvUpgrader != nil {
		return nil, errors.New("tableflip: only a single Upgrader allowed")
	}

	upg, err = newUpgrader(stdEnv, opts)
	// Store a reference to upg in a private global variable, to prevent
	// it from being GC'ed and exitFd being closed prematurely.
	stdEnvUpgrader = upg
	return
}

func newUpgrader(env *env, opts Options) (*Upgrader, error) {
	parent, files, err := newParent(env)
	if err != nil {
		return nil, err
	}

	if opts.UpgradeTimeout <= 0 {
		opts.UpgradeTimeout = DefaultUpgradeTimeout
	}

	s := &Upgrader{
		env:        env,
		opts:       opts,
		parent:     parent,
		readyC:     make(chan struct{}),
		stopC:      make(chan struct{}),
		upgradeSem: make(chan struct{}, 1),
		exitC:      make(chan struct{}),
		Fds:        newFds(files),
	}

	return s, nil
}

// Ready signals that the current process is ready to accept connections.
// It must be called to finish the upgrade.
//
// All fds which were inherited but not used are closed after the call to Ready.
func (u *Upgrader) Ready() error {
	u.readyOnce.Do(func() {
		u.Fds.closeInherited()
		close(u.readyC)
	})

	if u.opts.PIDFile != "" {
		if err := writePIDFile(u.opts.PIDFile); err != nil {
			return errors.Wrap(err, "tableflip: can't write PID file")
		}
	}

	if u.parent == nil {
		return nil
	}
	return u.parent.sendReady()
}

// Exit returns a channel which is closed when the process should
// exit.
func (u *Upgrader) Exit() <-chan struct{} {
	return u.exitC
}

// Stop prevents any more upgrades from happening, and closes
// the exit channel.
func (u *Upgrader) Stop() {
	u.stopOnce.Do(func() {
		// Interrupt any running Upgrade(), and
		// prevent new upgrade from happening.
		close(u.stopC)

		// Make sure exitC is closed if no upgrade was running.
		u.upgradeSem <- struct{}{}
		select {
		case <-u.exitC:
		default:
			close(u.exitC)
		}
		<-u.upgradeSem

		u.Fds.closeUsed()
	})
}

// Upgrade triggers an upgrade.
func (u *Upgrader) Upgrade() error {
	// Acquire semaphore, but don't block. This allows informing
	// the user that they are doing too many upgrade requests.
	select {
	default:
		return errors.New("upgrade in progress")
	case u.upgradeSem <- struct{}{}:
	}

	defer func() {
		<-u.upgradeSem
	}()

	// Make sure we're still ok to perform an upgrade.
	select {
	case <-u.exitC:
		return errors.New("already upgraded")
	default:
	}

	if u.parent != nil {
		if u.parentErr != nil {
			return u.parentErr
		}

		// verify clean exit
		select {
		case err := <-u.parent.exited:
			if err != nil {
				u.parentErr = err
				return err
			}

		default:
			return errors.New("parent hasn't exited")
		}
	}

	select {
	case <-u.readyC:
	default:
		return errors.New("process is not ready")
	}

	child, err := startChild(u.env, u.Fds.copy())
	if err != nil {
		return errors.Wrap(err, "can't start child")
	}

	readyTimeout := time.After(u.opts.UpgradeTimeout)
	select {
	case err := <-child.exitedC:
		if err == nil {
			return errors.Errorf("child %s exited", child)
		}
		return errors.Wrapf(err, "child %s exited", child)

	case <-u.stopC:
		child.Kill()
		return errors.New("terminating")

	case <-readyTimeout:
		child.Kill()
		return errors.Errorf("new child %s timed out", child)

	case file := <-child.readyC:
		// Save file in exitFd, so that it's only closed when the process
		// exits. This signals to the new process that the old process
		// has exited.
		u.exitFd = neverCloseThisFile{file}
		close(u.exitC)
		return nil
	}
}

// This file must never be closed by the Go runtime, since its used by the
// child to determine when the parent has died. It must only be closed
// by the OS.
// Hence we make sure that this file can't be garbage collected by referencing
// it from an Upgrader.
type neverCloseThisFile struct {
	file *os.File
}

func writePIDFile(path string) error {
	dir, file := filepath.Split(path)
	fh, err := ioutil.TempFile(dir, file)
	if err != nil {
		return err
	}
	defer fh.Close()
	// Remove temporary PID file if something fails
	defer os.Remove(fh.Name())

	_, err = fh.WriteString(strconv.Itoa(os.Getpid()))
	if err != nil {
		return err
	}

	return os.Rename(fh.Name(), path)
}
