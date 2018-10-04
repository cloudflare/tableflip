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
	if err := <-child.exitedC; err != nil {
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
	<-child.exitedC

	select {
	case <-child.readyC:
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
	<-child.readyC
	proc.exit(nil)
}

func TestChildPassedFds(t *testing.T) {
	env, procs := testEnv()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	in := map[string]*os.File{
		"r": r,
		"w": w,
	}

	if _, err := startChild(env, in); err != nil {
		t.Fatal(err)
	}

	proc := <-procs
	if len(proc.fds) != 2+2 {
		t.Error("Expected 4 files, got", len(proc.fds))
	}

	out, _, err := proc.notify()
	if err != nil {
		t.Fatal("Notify failed:", err)
	}

	if fd, ok := out["r"]; !ok {
		t.Error("r fd is missing")
	} else if fd.Fd() != r.Fd() {
		t.Error("r fd mismatch:", fd.Fd(), r.Fd())
	}

	if fd, ok := out["w"]; !ok {
		t.Error("w fd is missing")
	} else if fd.Fd() != w.Fd() {
		t.Error("w fd mismatch:", fd.Fd(), w.Fd())
	}

	proc.exit(nil)
}
