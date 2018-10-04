package tableflip

import (
	"net"
	"os"
	"testing"
)

func TestListeners(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	files := make(map[string]*os.File)
	err = AddListener(files, "test", ln.(Listener))
	if err != nil {
		t.Fatal("Can't add listener", err)
	}

	lns, err := Listeners(files)
	if err != nil {
		t.Fatal("Can't get listeners", err)
	}

	if _, ok := lns["test"]; !ok {
		t.Error("Listener 'test' doesn't exist")
	}
}
