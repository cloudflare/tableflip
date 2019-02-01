package tableflip

import (
	"testing"
)

func TestParentExit(t *testing.T) {
	env, procs := testEnv()
	child, err := startChild(env, nil)
	if err != nil {
		t.Fatal(err)
	}

	proc := <-procs
	_, exited, err := proc.notify()
	if err != nil {
		t.Fatal(err)
	}

	readyFile := <-child.ready
	if _, err = readyFile.Write([]byte{1}); err != nil {
		t.Fatal("Can't inject garbage from parent")
	}
	if err := readyFile.Close(); err != nil {
		t.Fatal(err)
	}

	err = <-exited
	if err == nil {
		t.Fatal("Expect child to detect garbage from parent")
	}
}
