package tableflip

import (
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFdsAddListener(t *testing.T) {
	socketPath, cleanup := tempSocket(t)
	defer cleanup()

	addrs := [][2]string{
		{"unix", socketPath},
		{"tcp", "localhost:0"},
	}

	fds := newFds(nil, nil)

	for _, addr := range addrs {
		ln, err := net.Listen(addr[0], addr[1])
		if err != nil {
			t.Fatal(err)
		}
		if err := fds.AddListener(addr[0], addr[1], ln.(Listener)); err != nil {
			t.Fatalf("Can't add %s listener: %s", addr[0], err)
		}
		ln.Close()
	}
}

func TestFdsAddPacketConn(t *testing.T) {
	socketPath, cleanup := tempSocket(t)
	defer cleanup()

	addrs := [][2]string{
		{"unix", socketPath},
		{"udp", "localhost:0"},
	}

	fds := newFds(nil, nil)
	for _, addr := range addrs {
		conn, err := net.ListenPacket(addr[0], addr[1])
		if err != nil {
			t.Fatal(err)
		}
		if err := fds.AddPacketConn(addr[0], addr[1], conn.(PacketConn)); err != nil {
			t.Fatalf("Can't add %s listener: %s", addr[0], err)
		}
		conn.Close()
	}
}

func tempSocket(t *testing.T) (string, func()) {
	t.Helper()

	temp, err := ioutil.TempDir("", "tableflip")
	if err != nil {
		t.Fatal(err)
	}

	return filepath.Join(temp, "socket"), func() { os.RemoveAll(temp) }
}

func TestFdsListen(t *testing.T) {
	socketPath, cleanup := tempSocket(t)
	defer cleanup()

	addrs := [][2]string{
		{"tcp", "localhost:0"},
		{"udp", "localhost:0"},
		{"unix", socketPath},
		{"unixgram", socketPath + "Unixgram"},
	}

	// Linux supports the abstract namespace for domain sockets.
	if runtime.GOOS == "linux" {
		addrs = append(addrs,
			[2]string{"unixpacket", socketPath + "Unixpacket"},
			[2]string{"unix", ""},
			[2]string{"unixpacket", ""},
			[2]string{"unixgram", ""},
		)
	}

	var (
		ln  io.Closer
		err error
	)

	parent := newFds(nil, nil)
	for _, addr := range addrs {
		switch addr[0] {
		case "udp", "unixgram":
			ln, err = parent.ListenPacket(addr[0], addr[1])
		default:
			ln, err = parent.Listen(addr[0], addr[1])
		}
		if err != nil {
			t.Fatalf("Can't create %s listener: %s", addr[0], err)
		}
		if ln == nil {
			t.Fatalf("Got a nil %s listener", addr[0])
		}
		ln.Close()
	}

	child := newFds(parent.copy(), nil)
	for _, addr := range addrs {
		switch addr[0] {
		case "udp", "unixgram":
			ln, err = child.PacketConn(addr[0], addr[1])
		default:
			ln, err = child.Listener(addr[0], addr[1])
		}
		if err != nil {
			t.Fatalf("Can't get retrieve %s from child: %s", addr[0], err)
		}
		if ln == nil {
			t.Fatalf("Missing %s listener", addr[0])
		}
		ln.Close()
	}
}

func TestFdsListenWithCallback(t *testing.T) {
	socketPath, cleanup := tempSocket(t)
	defer cleanup()

	addrs := [][2]string{
		{"tcp", "localhost:0"},
		{"udp", "localhost:0"},
		{"unix", socketPath},
		{"unixgram", socketPath + "Unixgram"},
	}
	parent := newFds(nil, nil)

	var (
		ln  io.Closer
		err error
	)

	called := false
	packetCb := func(network, addr string) (net.PacketConn, error) {
		called = true
		return net.ListenPacket(network, addr)
	}
	listenerCb := func(network, addr string) (net.Listener, error) {
		called = true
		return net.Listen(network, addr)
	}

	for _, addr := range addrs {
		called = false
		switch addr[0] {
		case "udp", "unixgram":
			ln, err = parent.ListenPacketWithCallback(addr[0], addr[1], packetCb)
		default:
			ln, err = parent.ListenWithCallback(addr[0], addr[1], listenerCb)
		}
		if err != nil {
			t.Fatalf("Can't create %s listener: %s", addr[0], err)
		}
		if ln == nil {
			t.Fatalf("Got a nil %s listener", addr[0])
		}
		if !called {
			t.Fatalf("Callback not called for new %s listener", addr[0])
		}
		ln.Close()
	}

	child := newFds(parent.copy(), nil)
	for _, addr := range addrs {
		called = false
		switch addr[0] {
		case "udp", "unixgram":
			ln, err = child.ListenPacketWithCallback(addr[0], addr[1], packetCb)
		default:
			ln, err = child.ListenWithCallback(addr[0], addr[1], listenerCb)
		}
		if err != nil {
			t.Fatalf("Can't get retrieve %s from child: %s", addr[0], err)
		}
		if ln == nil {
			t.Fatalf("Missing %s listener", addr[0])
		}
		if called {
			t.Fatalf("Callback called for inherited %s listener", addr[0])
		}
		ln.Close()
	}
}

func TestFdsRemoveUnix(t *testing.T) {
	socketPath, cleanup := tempSocket(t)
	defer cleanup()

	addrs := [][2]string{
		{"unix", socketPath},
		{"unixgram", socketPath + "Unixgram"},
	}

	if runtime.GOOS == "linux" {
		addrs = append(addrs,
			[2]string{"unixpacket", socketPath + "Unixpacket"},
		)
	}

	makeFds := func(t *testing.T) *Fds {
		fds := newFds(nil, nil)
		for _, addr := range addrs {
			var c io.Closer
			var err error
			if addr[0] == "unixgram" {
				c, err = fds.ListenPacket(addr[0], addr[1])
			} else {
				c, err = fds.Listen(addr[0], addr[1])
			}
			if err != nil {
				t.Fatalf("Can't listen on socket %v: %v", addr, err)
			}
			c.Close()
			if _, err := os.Stat(addr[1]); err != nil {
				t.Errorf("%s Close() unlinked socket: %s", addr[0], err)
			}
		}
		return fds
	}

	t.Run("closeAndRemoveUsed", func(t *testing.T) {
		parent := makeFds(t)
		parent.closeAndRemoveUsed()
		for _, addr := range addrs {
			if _, err := os.Stat(addr[1]); err == nil {
				t.Errorf("Used %s listeners are not removed", addr[0])
			}
		}
	})

	t.Run("closeInherited", func(t *testing.T) {
		parent := makeFds(t)
		child := newFds(parent.copy(), nil)
		child.closeInherited()
		for _, addr := range addrs {
			if _, err := os.Stat(addr[1]); err == nil {
				t.Errorf("Inherited but unused %s listeners are not removed", addr[0])
			}
		}
	})

	t.Run("closeUsed", func(t *testing.T) {
		parent := makeFds(t)
		parent.closeUsed()
		for _, addr := range addrs {
			if _, err := os.Stat(addr[1]); err != nil {
				t.Errorf("Used %s listeners are removed", addr[0])
			}
		}
	})
}

func TestFdsConn(t *testing.T) {
	socketPath, cleanup := tempSocket(t)
	defer cleanup()
	unix, err := net.ListenUnixgram("unixgram", &net.UnixAddr{
		Net:  "unixgram",
		Name: socketPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	parent := newFds(nil, nil)
	if err := parent.AddConn("unixgram", "", unix); err != nil {
		t.Fatal("Can't add conn:", err)
	}
	unix.Close()

	child := newFds(parent.copy(), nil)
	conn, err := child.Conn("unixgram", "")
	if err != nil {
		t.Fatal("Can't get conn:", err)
	}
	if conn == nil {
		t.Fatal("Missing conn")
	}
	conn.Close()
}

func TestFdsFile(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	parent := newFds(nil, nil)
	if err := parent.AddFile("test", w); err != nil {
		t.Fatal("Can't add file:", err)
	}
	w.Close()

	child := newFds(parent.copy(), nil)
	file, err := child.File("test")
	if err != nil {
		t.Fatal("Can't get file:", err)
	}
	if file == nil {
		t.Fatal("Missing file")
	}
	file.Close()
}

func TestFdsFiles(t *testing.T) {
	r1, w1, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r1.Close()

	r2, w2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()

	testcases := []struct {
		f        *os.File
		name     string
		expected string
	}{
		{
			w1,
			"test1",
			"fd:test1:",
		},
		{
			w2,
			"test2",
			"fd:test2:",
		},
	}

	parent := newFds(nil, nil)
	for _, tc := range testcases {
		if err := parent.AddFile(tc.name, tc.f); err != nil {
			t.Fatal("Can't add file:", err)
		}
		tc.f.Close()
	}

	child := newFds(parent.copy(), nil)
	files, err := child.Files()
	if err != nil {
		t.Fatal("Can't get inherited files:", err)
	}

	if len(files) != len(testcases) {
		t.Fatalf("Expected %d files, got %d", len(testcases), len(files))
	}

	for i, ff := range files {
		tc := testcases[i]

		if ff.Name() != tc.expected {
			t.Errorf("Expected file %q, got %q", tc.expected, ff.Name())
		}

		ff.Close()
	}
}
