package tableflip

import (
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/pkg/errors"
)

// Listener can be shared between processes.
type Listener interface {
	net.Listener
	syscall.Conn
}

const (
	listenerPrefix = "listener:"
)

// Listeners retrieves all listeners from src.
//
// Closing the returned listeners does not affect entries in src, and vice versa.
func Listeners(src map[string]*os.File) (map[string]net.Listener, error) {
	lns := make(map[string]net.Listener)
	for key, file := range src {
		if !strings.HasPrefix(key, listenerPrefix) {
			continue
		}

		name := strings.TrimPrefix(key, listenerPrefix)
		ln, err := net.FileListener(file)
		if err != nil {
			return nil, errors.Wrapf(err, "tableflip: can't create listener %s", name)
		}
		lns[name] = ln
	}
	return lns, nil
}

// AddListener adds a listener to dst.
func AddListener(dst map[string]*os.File, name string, ln Listener) error {
	key := fmt.Sprintf("%s%s", listenerPrefix, name)
	if _, ok := dst[key]; ok {
		return errors.Errorf("tableflip: listener %s already exists", name)
	}

	f, err := dupConn(ln)
	if err != nil {
		return errors.Wrapf(err, "tableflip: can't dup listener %s", name)
	}

	dst[key] = f
	return nil
}

func dupConn(conn syscall.Conn) (*os.File, error) {
	// Use SyscallConn instead of File to avoid making the original
	// fd non-blocking.
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}

	var file *os.File
	var duperr error
	err = raw.Control(func(fd uintptr) {
		var nfd int
		nfd, duperr = syscall.Dup(int(fd))
		if duperr != nil {
			return
		}
		file = os.NewFile(uintptr(nfd), "passed")
	})
	if err != nil {
		return nil, err
	}
	if duperr != nil {
		return nil, duperr
	}
	return file, nil
}
