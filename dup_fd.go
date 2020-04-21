// +build !windows

package tableflip

import (
	"syscall"

	"github.com/pkg/errors"
)

func dupFd(fd uintptr, name fileName) (*file, error) {
	dupfd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		return nil, errors.Wrap(errno, "can't dup fd using fcntl")
	}

	return newFile(dupfd, name), nil
}
