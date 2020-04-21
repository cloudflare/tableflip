package tableflip

import "errors"

func dupFd(fd uintptr, name fileName) (*file, error) {
	return nil, errors.New("tableflip: duplicating file descriptors is not supported on this platform")
}
