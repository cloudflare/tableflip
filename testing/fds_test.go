package testing

import (
	"testing"
)

func TestFdsListen(t *testing.T) {
	addrs := [][2]string{
		{"tcp", "localhost:0"},
	}

	fds := &Fds{}

	for _, addr := range addrs {
		ln, err := fds.Listen(addr[0], addr[1])
		if err != nil {
			t.Fatal(err)
		}
		if ln == nil {
			t.Fatal("Missing listener", addr)
		}
		ln.Close()
	}
}
