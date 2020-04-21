package tableflip

import (
	"os"
)

type env struct {
	newProc     func(string, []string, []*os.File, []string) (process, error)
	newFile     func(fd uintptr, name string) *os.File
	environ     func() []string
	getenv      func(string) string
	closeOnExec func(fd int)
}
