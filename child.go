package tableflip

import (
	"encoding/gob"
	"fmt"
	"os"
)

type child struct {
	*env
	proc           process
	readyR, namesW *os.File
	ready          <-chan *os.File
	result         <-chan error
	exited         <-chan struct{}
}

func startChild(env *env, passedFiles map[fileName]*file) (*child, error) {
	// These pipes are used for communication between parent and child
	// readyW is passed to the child, readyR stays with the parent
	readyR, readyW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("pipe failed: %s", err)
	}

	namesR, namesW, err := os.Pipe()
	if err != nil {
		readyR.Close()
		readyW.Close()
		return nil, fmt.Errorf("pipe failed: %s", err)
	}

	// Copy passed fds and append the notification pipe
	fds := []*os.File{os.Stdin, os.Stdout, os.Stderr, readyW, namesR}
	var fdNames [][]string
	for name, file := range passedFiles {
		nameSlice := make([]string, len(name))
		copy(nameSlice, name[:])
		fdNames = append(fdNames, nameSlice)
		fds = append(fds, file.File)
	}

	// Copy environment and append the notification env vars
	sentinel := fmt.Sprintf("%s=yes", sentinelEnvVar)
	var environ []string
	for _, val := range env.environ() {
		if val != sentinel {
			environ = append(environ, val)
		}
	}
	environ = append(environ, sentinel)

	proc, err := env.newProc(os.Args[0], os.Args[1:], fds, environ)
	if err != nil {
		readyR.Close()
		readyW.Close()
		namesR.Close()
		namesW.Close()
		return nil, fmt.Errorf("can't start process %s: %s", os.Args[0], err)
	}

	exited := make(chan struct{})
	result := make(chan error, 1)
	ready := make(chan *os.File, 1)

	c := &child{
		env,
		proc,
		readyR,
		namesW,
		ready,
		result,
		exited,
	}
	go c.writeNames(fdNames)
	go c.waitExit(result, exited)
	go c.waitReady(ready)
	return c, nil
}

func (c *child) String() string {
	return c.proc.String()
}

func (c *child) Kill() {
	c.proc.Signal(os.Kill)
}

func (c *child) waitExit(result chan<- error, exited chan<- struct{}) {
	result <- c.proc.Wait()
	close(exited)
	// Unblock waitReady and writeNames
	c.readyR.Close()
	c.namesW.Close()
}

func (c *child) waitReady(ready chan<- *os.File) {
	var b [1]byte
	if n, _ := c.readyR.Read(b[:]); n > 0 && b[0] == notifyReady {
		// We know that writeNames has exited by this point.
		// Closing the FD now signals to the child that the parent
		// has exited.
		ready <- c.namesW
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
