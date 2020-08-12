package tableflip

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

// Listener can be shared between processes.
type Listener interface {
	net.Listener
	syscall.Conn
}

// PacketConn can be shared between processes.
type PacketConn interface {
	net.PacketConn
	syscall.Conn
}

// Conn can be shared between processes.
type Conn interface {
	net.Conn
	syscall.Conn
}

const (
	listenKind = "listener"
	packetKind = "packet"
	connKind   = "conn"
	fdKind     = "fd"
)

type fileName [3]string

func (name fileName) String() string {
	return strings.Join(name[:], ":")
}

func (name fileName) isUnix() bool {
	if name[0] == listenKind && (name[1] == "unix" || name[1] == "unixpacket") {
		return true
	}
	if name[0] == packetKind && (name[1] == "unixgram") {
		return true
	}
	return false
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
	lc        *net.ListenConfig
}

func newFds(inherited map[fileName]*file, lc *net.ListenConfig) *Fds {
	if inherited == nil {
		inherited = make(map[fileName]*file)
	}

	if lc == nil {
		lc = &net.ListenConfig{}
	}

	return &Fds{
		inherited: inherited,
		used:      make(map[fileName]*file),
		lc:        lc,
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

	ln, err = f.lc.Listen(context.Background(), network, addr)
	if err != nil {
		return nil, fmt.Errorf("can't create new listener: %s", err)
	}

	if _, ok := ln.(Listener); !ok {
		ln.Close()
		return nil, fmt.Errorf("%T doesn't implement tableflip.Listener", ln)
	}

	err = f.addListenerLocked(network, addr, ln.(Listener))
	if err != nil {
		ln.Close()
		return nil, err
	}

	return ln, nil
}

// ListenWithCallback returns a listener inherited from the parent process,
// or calls the supplied callback to create a new one.
//
// This should be used in case some customization has to be applied to create the
// connection. Note that the callback must not use the underlying `Fds` object
// as it will be locked during the call.
func (f *Fds) ListenWithCallback(network, addr string, callback func(network, addr string) (net.Listener, error)) (net.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ln, err := f.listenerLocked(network, addr)
	if err != nil {
		return nil, err
	}

	if ln != nil {
		return ln, nil
	}

	ln, err = callback(network, addr)
	if err != nil {
		return nil, fmt.Errorf("can't create new listener: %s", err)
	}

	if _, ok := ln.(Listener); !ok {
		ln.Close()
		return nil, fmt.Errorf("%T doesn't implement tableflip.Listener", ln)
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
		return nil, fmt.Errorf("can't inherit listener %s %s: %s", network, addr, err)
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

	return f.addSyscallConnLocked(listenKind, network, addr, ln)
}

// ListenPacket returns a packet conn inherited from the parent process, or creates a new one.
func (f *Fds) ListenPacket(network, addr string) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	conn, err := f.packetConnLocked(network, addr)
	if err != nil {
		return nil, err
	}

	if conn != nil {
		return conn, nil
	}

	conn, err = f.lc.ListenPacket(context.Background(), network, addr)
	if err != nil {
		return nil, fmt.Errorf("can't create new listener: %s", err)
	}

	if _, ok := conn.(PacketConn); !ok {
		return nil, fmt.Errorf("%T doesn't implement tableflip.PacketConn", conn)
	}

	err = f.addSyscallConnLocked(packetKind, network, addr, conn.(PacketConn))
	if err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// ListenPacketWithCallback returns a packet conn inherited from the parent process,
// or calls the supplied callback to create a new one.
//
// This should be used in case some customization has to be applied to create the
// connection. Note that the callback must not use the underlying `Fds` object
// as it will be locked during the call.
func (f *Fds) ListenPacketWithCallback(network, addr string, callback func(network, addr string) (net.PacketConn, error)) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	conn, err := f.packetConnLocked(network, addr)
	if err != nil {
		return nil, err
	}

	if conn != nil {
		return conn, nil
	}

	conn, err = callback(network, addr)
	if err != nil {
		return nil, fmt.Errorf("can't create new listener: %s", err)
	}

	if _, ok := conn.(PacketConn); !ok {
		return nil, fmt.Errorf("%T doesn't implement tableflip.PacketConn", conn)
	}

	err = f.addSyscallConnLocked(packetKind, network, addr, conn.(PacketConn))
	if err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// PacketConn returns an inherited packet connection or nil.
//
// It is safe to close the returned packet connection.
func (f *Fds) PacketConn(network, addr string) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.packetConnLocked(network, addr)
}

// AddPacketConn adds a PacketConn.
//
// It is safe to close conn after calling the method.
// Any existing packet connection with the same address is overwitten.
func (f *Fds) AddPacketConn(network, addr string, conn PacketConn) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.addSyscallConnLocked(packetKind, network, addr, conn)
}

func (f *Fds) packetConnLocked(network, addr string) (net.PacketConn, error) {
	key := fileName{packetKind, network, addr}
	file := f.inherited[key]
	if file == nil {
		return nil, nil
	}

	conn, err := net.FilePacketConn(file.File)
	if err != nil {
		return nil, fmt.Errorf("can't inherit packet conn %s %s: %s", network, addr, err)
	}

	delete(f.inherited, key)
	f.used[key] = file
	return conn, nil
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
		return nil, fmt.Errorf("can't inherit connection %s %s: %s", network, addr, err)
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

	return f.addSyscallConnLocked(connKind, network, addr, conn)
}

func (f *Fds) addSyscallConnLocked(kind, network, addr string, conn syscall.Conn) error {
	key := fileName{kind, network, addr}
	file, err := dupConn(conn, key)
	if err != nil {
		return fmt.Errorf("can't dup %s (%s %s): %s", kind, network, addr, err)
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
		if key.isUnix() {
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
		if key.isUnix() {
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
		return nil, fmt.Errorf("can't access fd: %s", err)
	}
	return dup, duperr
}
