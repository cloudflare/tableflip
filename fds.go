package tableflip

import (
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/pkg/errors"
)

// Listener can be shared between processes.
type Listener interface {
	net.Listener
	syscall.Conn
}

// Conn can be shared between processes.
type Conn interface {
	net.Conn
	syscall.Conn
}

const (
	listenKind = "listener"
	connKind   = "conn"
	fdKind     = "fd"
)

type fileName [3]string

func (name fileName) String() string {
	return strings.Join(name[:], ":")
}

func (name fileName) isUnixListener() bool {
	return name[0] == listenKind && (name[1] == "unix" || name[1] == "unixpacket")
}

// file works around the fact that it's not possible
// to get the fd from an os.File without putting it into
// blocking mode.
type file struct {
	*os.File
	fd uintptr
}

func newFile(fd uintptr, name fileName) *file {
	f := os.NewFile(fd, name.String())
	if f == nil {
		return nil
	}

	return &file{
		f,
		fd,
	}
}

// Fds holds all file descriptors inherited from the
// parent process.
type Fds struct {
	mu sync.Mutex
	// NB: Files in these maps may be in blocking mode.
	inherited map[fileName]*file
	used      map[fileName]*file
}

func newFds(inherited map[fileName]*file) *Fds {
	if inherited == nil {
		inherited = make(map[fileName]*file)
	}
	return &Fds{
		inherited: inherited,
		used:      make(map[fileName]*file),
	}
}

// Listen returns a listener inherited from the parent process, or creates a new one.
func (f *Fds) Listen(network, addr string) (net.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ln, err := f.listenerLocked(network, addr)
	if err != nil {
		return nil, err
	}

	if ln != nil {
		return ln, nil
	}

	ln, err = net.Listen(network, addr)
	if err != nil {
		return nil, errors.Wrap(err, "can't create new listener")
	}

	if _, ok := ln.(Listener); !ok {
		ln.Close()
		return nil, errors.Errorf("%T doesn't implement tableflip.Listener", ln)
	}

	err = f.addListenerLocked(network, addr, ln.(Listener))
	if err != nil {
		ln.Close()
		return nil, err
	}

	return ln, nil
}

// Listener returns an inherited listener or nil.
//
// It is safe to close the returned listener.
func (f *Fds) Listener(network, addr string) (net.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.listenerLocked(network, addr)
}

func (f *Fds) listenerLocked(network, addr string) (net.Listener, error) {
	key := fileName{listenKind, network, addr}
	file := f.inherited[key]
	if file == nil {
		return nil, nil
	}

	ln, err := net.FileListener(file.File)
	if err != nil {
		return nil, errors.Wrapf(err, "can't inherit listener %s %s", network, addr)
	}

	delete(f.inherited, key)
	f.used[key] = file
	return ln, nil
}

// AddListener adds a listener.
//
// It is safe to close ln after calling the method.
// Any existing listener with the same address is overwitten.
func (f *Fds) AddListener(network, addr string, ln Listener) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.addListenerLocked(network, addr, ln)
}

type unlinkOnCloser interface {
	SetUnlinkOnClose(bool)
}

func (f *Fds) addListenerLocked(network, addr string, ln Listener) error {
	if ifc, ok := ln.(unlinkOnCloser); ok {
		ifc.SetUnlinkOnClose(false)
	}

	return f.addConnLocked(listenKind, network, addr, ln)
}

// Conn returns an inherited connection or nil.
//
// It is safe to close the returned Conn.
func (f *Fds) Conn(network, addr string) (net.Conn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := fileName{connKind, network, addr}
	file := f.inherited[key]
	if file == nil {
		return nil, nil
	}

	conn, err := net.FileConn(file.File)
	if err != nil {
		return nil, errors.Wrapf(err, "can't inherit connection %s %s", network, addr)
	}

	delete(f.inherited, key)
	f.used[key] = file
	return conn, nil
}

// AddConn adds a connection.
//
// It is safe to close conn after calling this method.
func (f *Fds) AddConn(network, addr string, conn Conn) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.addConnLocked(connKind, network, addr, conn)
}

func (f *Fds) addConnLocked(kind, network, addr string, conn syscall.Conn) error {
	key := fileName{kind, network, addr}
	file, err := dupConn(conn, key)
	if err != nil {
		return errors.Wrapf(err, "can't dup %s (%s %s)", kind, network, addr)
	}

	delete(f.inherited, key)
	f.used[key] = file
	return nil
}

// File returns an inherited file or nil.
//
// The descriptor may be in blocking mode.
func (f *Fds) File(name string) (*os.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := fileName{fdKind, name}
	file := f.inherited[key]
	if file == nil {
		return nil, nil
	}

	// Make a copy of the file, since we don't want to
	// allow the caller to invalidate fds in f.inherited.
	dup, err := dupFd(file.fd, key)
	if err != nil {
		return nil, err
	}

	delete(f.inherited, key)
	f.used[key] = file
	return dup.File, nil
}

// AddFile adds a file.
//
// Until Go 1.12, file will be in blocking mode
// after this call.
func (f *Fds) AddFile(name string, file *os.File) error {
	key := fileName{fdKind, name}

	dup, err := dupFile(file, key)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.inherited, key)
	f.used[key] = dup
	return nil
}

func (f *Fds) copy() map[fileName]*file {
	f.mu.Lock()
	defer f.mu.Unlock()

	files := make(map[fileName]*file, len(f.used))
	for key, file := range f.used {
		files[key] = file
	}

	return files
}

func (f *Fds) closeInherited() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for key, file := range f.inherited {
		if key.isUnixListener() {
			// Remove inherited but unused Unix sockets from the file system.
			// This undoes the effect of SetUnlinkOnClose(false).
			_ = unlinkUnixSocket(key[2])
		}
		_ = file.Close()
	}
	f.inherited = make(map[fileName]*file)
}

func unlinkUnixSocket(path string) error {
	if runtime.GOOS == "linux" && strings.HasPrefix(path, "@") {
		// Don't unlink sockets using the abstract namespace.
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSocket == 0 {
		return nil
	}

	return os.Remove(path)
}

func (f *Fds) closeUsed() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, file := range f.used {
		_ = file.Close()
	}
	f.used = make(map[fileName]*file)
}

func (f *Fds) closeAndRemoveUsed() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for key, file := range f.used {
		if key.isUnixListener() {
			// Remove used Unix Domain Sockets if we are shutting
			// down without having done an upgrade.
			// This undoes the effect of SetUnlinkOnClose(false).
			_ = unlinkUnixSocket(key[2])
		}
		_ = file.Close()
	}
	f.used = make(map[fileName]*file)
}

func dupConn(conn syscall.Conn, name fileName) (*file, error) {
	// Use SyscallConn instead of File to avoid making the original
	// fd non-blocking.
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}

	var dup *file
	var duperr error
	err = raw.Control(func(fd uintptr) {
		dup, duperr = dupFd(fd, name)
	})
	if err != nil {
		return nil, errors.Wrap(err, "can't access fd")
	}
	return dup, duperr
}

func dupFd(fd uintptr, name fileName) (*file, error) {
	dupfd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		return nil, errors.Wrap(errno, "can't dup fd using fcntl")
	}

	return newFile(dupfd, name), nil
}
