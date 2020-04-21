package tableflip

import (
	"os"
)

// replace Unix-specific syscall with a no-op so it will build
// without errors.

var stdEnv = &env{
	newProc:     newOSProcess,
	newFile:     os.NewFile,
	environ:     os.Environ,
	getenv:      os.Getenv,
	closeOnExec: func(fd int) {},
}
