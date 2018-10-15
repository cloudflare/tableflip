package tableflip

import (
	"fmt"
	"os"
	"strings"
)

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
				return fds[fd-3]
			},
			getenv: func(key string) string {
				return environ[key]
			},
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
	return files, parent.exited, parent.sendReady()
}
