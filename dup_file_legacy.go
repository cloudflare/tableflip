// +build !go1.12

package tableflip

import (
	"os"
)

func dupFile(fh *os.File, name fileName) (*file, error) {
	return dupFd(fh.Fd(), name)
}
