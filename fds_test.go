package tableflip

import (
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

	fds := newFds(nil)
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
		{"unix", socketPath},
	}

	// Linux supports the abstract namespace for domain sockets.
	if runtime.GOOS == "linux" {
		addrs = append(addrs,
			[2]string{"unixpacket", socketPath + "Unixpacket"},
			[2]string{"unix", ""},
			[2]string{"unixpacket", ""},
		)
	}

	parent := newFds(nil)
	for _, addr := range addrs {
		ln, err := parent.Listen(addr[0], addr[1])
		if err != nil {
			t.Fatalf("Can't create %s listener: %s", addr[0], addr[1])
		}
		ln.Close()
	}

	child := newFds(parent.copy())
	for _, addr := range addrs {
		ln, err := child.Listener(addr[0], addr[1])
		if err != nil {
			t.Fatal("Can't get listener:", err)
		}
		if ln == nil {
			t.Fatal("Missing listener")
		}
		ln.Close()
	}
}

func TestFdsRemoveUnix(t *testing.T) {
	socketPath, cleanup := tempSocket(t)
	defer cleanup()

	addrs := [][2]string{
		{"unix", socketPath},
	}

	if runtime.GOOS == "linux" {
		addrs = append(addrs,
			[2]string{"unixpacket", socketPath + "Unixpacket"},
		)
	}

	makeFds := func(t *testing.T) *Fds {
		fds := newFds(nil)
		for _, addr := range addrs {
			c, err := fds.Listen(addr[0], addr[1])
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
		child := newFds(parent.copy())
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

	parent := newFds(nil)
	if err := parent.AddConn("unixgram", "", unix); err != nil {
		t.Fatal("Can't add conn:", err)
	}
	unix.Close()

	child := newFds(parent.copy())
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

	parent := newFds(nil)
	if err := parent.AddFile("test", w); err != nil {
		t.Fatal("Can't add file:", err)
	}
	w.Close()

	child := newFds(parent.copy())
	file, err := child.File("test")
	if err != nil {
		t.Fatal("Can't get file:", err)
	}
	if file == nil {
		t.Fatal("Missing file")
	}
	file.Close()
}
