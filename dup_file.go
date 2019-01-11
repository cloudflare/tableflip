// +build go1.12

package tableflip

import (
	"os"
)

func dupFile(fh *os.File, name fileName) (*file, error) {
	// os.File implements syscall.Conn from go 1.12
	return dupConn(fh, name)
}
