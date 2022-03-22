//go:build !windows
// +build !windows

package tableflip

import (
	"fmt"
	"syscall"
)

func dupFd(fd uintptr, name fileName) (*file, error) {
	dupfd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		return nil, fmt.Errorf("can't dup fd using fcntl: %s", errno)
	}

	return newFile(dupfd, name), nil
}
