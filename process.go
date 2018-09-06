package tableflip

import (
	"fmt"
	"os"
	"os/exec"
)

var initialWD, _ = os.Getwd()

type process interface {
	fmt.Stringer
	Signal(sig os.Signal) error
	Wait() error
}

type osProcess struct {
	cmd *exec.Cmd
}

func newOSProcess(executable string, args []string, files []*os.File, env []string) (process, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = initialWD
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = files
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &osProcess{cmd}, nil
}

func (osp *osProcess) Signal(sig os.Signal) error {
	return osp.cmd.Process.Signal(sig)
}

func (osp *osProcess) Wait() error {
	return osp.cmd.Wait()
}

func (osp *osProcess) String() string {
	return fmt.Sprintf("pid=%d", osp.cmd.Process.Pid)
}
