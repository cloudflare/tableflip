package tableflip

import (
	"encoding/gob"
	"fmt"
	"os"

	"github.com/pkg/errors"
)

type child struct {
	*env
	proc           process
	readyR, namesW *os.File
	readyC         <-chan *os.File
	exitedC        <-chan error
	doneC          <-chan struct{}
}

func startChild(env *env, passedFiles map[fileName]*file) (*child, error) {
	// These pipes are used for communication between parent and child
	// readyW is passed to the child, readyR stays with the parent
	readyR, readyW, err := os.Pipe()
	if err != nil {
		return nil, errors.Wrap(err, "pipe failed")
	}

	namesR, namesW, err := os.Pipe()
	if err != nil {
		readyR.Close()
		readyW.Close()
		return nil, errors.Wrap(err, "pipe failed")
	}

	// Copy passed fds and append the notification pipe
	fds := []*os.File{readyW, namesR}
	var fdNames [][]string
	for name, file := range passedFiles {
		nameSlice := make([]string, len(name))
		copy(nameSlice, name[:])
		fdNames = append(fdNames, nameSlice)
		fds = append(fds, file.File)
	}

	// Copy environment and append the notification env vars
	environ := append([]string(nil), env.environ()...)
	environ = append(environ,
		fmt.Sprintf("%s=yes", sentinelEnvVar))

	proc, err := env.newProc(os.Args[0], os.Args[1:], fds, environ)
	if err != nil {
		readyR.Close()
		readyW.Close()
		namesR.Close()
		namesW.Close()
		return nil, errors.Wrapf(err, "can't start process %s", os.Args[0])
	}

	doneC := make(chan struct{})
	exitedC := make(chan error, 1)
	readyC := make(chan *os.File, 1)

	c := &child{
		env,
		proc,
		readyR,
		namesW,
		readyC,
		exitedC,
		doneC,
	}
	go c.writeNames(fdNames)
	go c.waitExit(exitedC, doneC)
	go c.waitReady(readyC)
	return c, nil
}

func (c *child) String() string {
	return c.proc.String()
}

func (c *child) Kill() {
	c.proc.Signal(os.Kill)
}

func (c *child) waitExit(exitedC chan<- error, doneC chan<- struct{}) {
	exitedC <- c.proc.Wait()
	close(doneC)
	// Unblock waitReady and writeNames
	c.readyR.Close()
	c.namesW.Close()
}

func (c *child) waitReady(readyC chan<- *os.File) {
	var b [1]byte
	if n, _ := c.readyR.Read(b[:]); n > 0 && b[0] == notifyReady {
		// We know that writeNames has exited by this point.
		// Closing the FD now signals to the child that the parent
		// has exited.
		readyC <- c.namesW
	}
	c.readyR.Close()
}

func (c *child) writeNames(names [][]string) {
	enc := gob.NewEncoder(c.namesW)
	if names == nil {
		// Gob panics on nil
		_ = enc.Encode([][]string{})
		return
	}
	_ = enc.Encode(names)
}
