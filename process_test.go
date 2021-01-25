package tableflip

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFilesAreNonblocking(t *testing.T) {
	pipe := func() (r, w *os.File) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			r.Close()
			w.Close()
		})
		return r, w
	}

	// Set up our own blocking stdin since CI runs tests with stdin closed.
	rStdin, _ := pipe()
	rStdin.Fd()

	r, _ := pipe()
	if !isNonblock(t, r) {
		t.Fatal("Read pipe is blocking")
	}

	proc, err := newOSProcess("cat", nil, []*os.File{rStdin, os.Stdout, os.Stderr, r}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := proc.Signal(os.Kill); err != nil {
		t.Fatal("Can't signal:", err)
	}

	var exitErr *exec.ExitError
	if err := proc.Wait(); !errors.As(err, &exitErr) {
		t.Fatalf("Wait should return an ExitError after sending os.Kill, have %T: %s", err, err)
	}

	if err := proc.Wait(); err == nil {
		t.Fatal("Waiting a second time should return an error")
	}

	if !isNonblock(t, r) {
		t.Fatal("Read pipe is blocking after newOSProcess")
	}
}

func TestArgumentsArePassedCorrectly(t *testing.T) {
	proc, err := newOSProcess("printf", []string{""}, []*os.File{os.Stdin, os.Stdout, os.Stderr}, nil)
	if err != nil {
		t.Fatal("Can't execute printf:", err)
	}

	// If the argument handling is wrong we'll call printf without any arguments.
	// In that case printf exits non-zero.
	if err = proc.Wait(); err != nil {
		t.Fatal("printf exited non-zero:", err)
	}
}

func isNonblock(tb testing.TB, file *os.File) (nonblocking bool) {
	tb.Helper()

	raw, err := file.SyscallConn()
	if err != nil {
		tb.Fatal("SyscallConn:", err)
	}

	err = raw.Control(func(fd uintptr) {
		flags, err := unix.FcntlInt(fd, unix.F_GETFL, 0)
		if err != nil {
			tb.Fatal("IsNonblock:", err)
		}
		nonblocking = flags&unix.O_NONBLOCK > 0
	})
	if err != nil {
		tb.Fatal("Control:", err)
	}
	return
}

type testProcess struct {
	fds     []*os.File
	env     env
	signals chan os.Signal
	sigErr  chan error
	waitErr chan error
	quit    chan struct{}
}

func newTestProcess(fds []*os.File, envstr []string) (*testProcess, error) {
	environ := make(map[string]string)
	for _, entry := range envstr {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid env entry: %s", entry)
		}
		environ[parts[0]] = parts[1]
	}

	return &testProcess{
		fds,
		env{
			newFile: func(fd uintptr, name string) *os.File {
				return fds[fd]
			},
			getenv: func(key string) string {
				return environ[key]
			},
			closeOnExec: func(int) {},
		},
		make(chan os.Signal, 1),
		make(chan error),
		make(chan error),
		make(chan struct{}),
	}, nil
}

func (tp *testProcess) Signal(sig os.Signal) error {
	select {
	case tp.signals <- sig:
		return <-tp.sigErr
	case <-tp.quit:
		return nil
	}
}

func (tp *testProcess) Wait() error {
	select {
	case err := <-tp.waitErr:
		return err
	case <-tp.quit:
		return nil
	}
}

func (tp *testProcess) String() string {
	return fmt.Sprintf("tp=%p", tp)
}

func (tp *testProcess) exit(err error) {
	select {
	case tp.waitErr <- err:
		close(tp.quit)
	case <-tp.quit:
	}
}

func (tp *testProcess) recvSignal(err error) os.Signal {
	sig := <-tp.signals
	tp.sigErr <- err
	return sig
}

func (tp *testProcess) notify() (map[fileName]*file, <-chan error, error) {
	parent, files, err := newParent(&tp.env)
	if err != nil {
		return nil, nil, err
	}
	return files, parent.result, parent.sendReady()
}
