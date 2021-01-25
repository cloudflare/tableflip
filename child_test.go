package tableflip

import (
	"os"
	"testing"
)

func TestChildExit(t *testing.T) {
	env, procs := testEnv()

	child, err := startChild(env, nil)
	if err != nil {
		t.Fatal(err)
	}

	proc := <-procs
	proc.exit(nil)
	if err := <-child.result; err != nil {
		t.Error("Wait returns non-nil error:", err)
	}
}

func TestChildKill(t *testing.T) {
	env, procs := testEnv()

	child, err := startChild(env, nil)
	if err != nil {
		t.Fatal(err)
	}

	proc := <-procs

	go child.Kill()
	if sig := proc.recvSignal(nil); sig != os.Kill {
		t.Errorf("Received %v instead of os.Kill", sig)
	}

	proc.exit(nil)
}

func TestChildNotReady(t *testing.T) {
	env, procs := testEnv()

	child, err := startChild(env, nil)
	if err != nil {
		t.Fatal(err)
	}

	proc := <-procs
	proc.exit(nil)
	<-child.result
	<-child.exited

	select {
	case <-child.ready:
		t.Error("Child signals readiness without pipe being closed")
	default:
	}
}

func TestChildReady(t *testing.T) {
	env, procs := testEnv()

	child, err := startChild(env, nil)
	if err != nil {
		t.Fatal(err)
	}

	proc := <-procs
	if _, _, err := proc.notify(); err != nil {
		t.Fatal("Can't notify:", err)
	}
	<-child.ready
	proc.exit(nil)
}

func TestChildPassedFds(t *testing.T) {
	env, procs := testEnv()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	in := map[fileName]*file{
		{"r"}: newFile(r.Fd(), fileName{"r"}),
		{"w"}: newFile(w.Fd(), fileName{"w"}),
	}

	if _, err := startChild(env, in); err != nil {
		t.Fatal(err)
	}

	proc := <-procs
	out, _, err := proc.notify()
	if err != nil {
		t.Fatal("Notify failed:", err)
	}

	if len(out) != len(in) {
		t.Errorf("Expected %d files, got %d", len(in), len(out))
	}

	for name, inFd := range in {
		if outFd, ok := out[name]; !ok {
			t.Error(name, "is missing")
		} else if outFd.Fd() != inFd.Fd() {
			t.Error(name, "fd mismatch:", outFd.Fd(), inFd.Fd())
		}
	}

	proc.exit(nil)
}
