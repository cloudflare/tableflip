package tableflip

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

var initialWD, _ = os.Getwd()

type process interface {
	fmt.Stringer
	Signal(sig os.Signal) error
	Wait() error
}

type osProcess struct {
	*os.Process
	finished bool
}

func newOSProcess(executable string, args []string, extraFiles []*os.File, env []string) (process, error) {
	executable, err := exec.LookPath(executable)
	if err != nil {
		return nil, err
	}

	files := append([]*os.File{os.Stdin, os.Stdout, os.Stderr}, extraFiles...)
	fds := make([]uintptr, 0, len(files))
	for _, file := range files {
		fd, err := sysConnFd(file)
		if err != nil {
			return nil, err
		}
		fds = append(fds, fd)
	}

	attr := &syscall.ProcAttr{
		Dir:   initialWD,
		Env:   env,
		Files: fds,
	}

	pid, _, err := syscall.StartProcess(executable, args, attr)
	if err != nil {
		return nil, fmt.Errorf("fork/exec: %s", err)
	}

	// Ensure that fds stay valid until after StartProcess finishes.
	runtime.KeepAlive(files)

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("find pid %d: %s", pid, err)
	}

	return &osProcess{Process: proc}, nil
}

func (osp *osProcess) Wait() error {
	if osp.finished {
		return fmt.Errorf("already waited")
	}
	osp.finished = true

	state, err := osp.Process.Wait()
	if err != nil {
		return err
	}

	if !state.Success() {
		return &exec.ExitError{ProcessState: state}
	}

	return nil
}

func (osp *osProcess) String() string {
	return fmt.Sprintf("pid=%d", osp.Pid)
}
