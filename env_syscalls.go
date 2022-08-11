//go:build !windows
// +build !windows

package tableflip

import (
	"os"

	"golang.org/x/sys/unix"
)

var stdEnv = &env{
	newProc:     newOSProcess,
	newFile:     os.NewFile,
	environ:     os.Environ,
	getenv:      os.Getenv,
	closeOnExec: unix.CloseOnExec,
}
