package testing

import (
	"net"
	"os"
)

type Fds struct{}

// Listen returns a listener by calling net.Listen directly
//
// Note: In the stub implementation, this is the only function that
// actually does anything
func (f *Fds) Listen(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}

// Listener always returns nil, since it is impossible to inherit with
// the stub implementation
func (f *Fds) Listener(network, addr string) (net.Listener, error) {
	return nil, nil
}

// AddListener does nothing, since there is no reason to track connections
// in the stub implementation
func (f *Fds) AddListener(network, addr string, ln net.Listener) error {
	return nil
}

// Conn always returns nil, since it is impossible to inherit with
// the stub implementation
func (f *Fds) Conn(network, addr string) (net.Conn, error) {
	return nil, nil
}

// AddConn does nothing, since there is no reason to track connections
// in the stub implementation
func (f *Fds) AddConn(network, addr string, conn net.Conn) error {
	return nil
}

// File always returns nil, since it is impossible to inherit with
// the stub implementation
func (f *Fds) File(name string) (*os.File, error) {
	return nil, nil
}

// AddFile does nothing, since there is no reason to track connections
// in the stub implementation
func (f *Fds) AddFile(name string, file *os.File) error {
	return nil
}
