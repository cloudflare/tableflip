//go:build !windows
// +build !windows

package tableflip

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func dupFd(fd uintptr, name fileName) (*file, error) {
	dupfd, err := unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("can't dup fd using fcntl: %s", err)
	}

	return newFile(uintptr(dupfd), name), nil
}
