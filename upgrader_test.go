package tableflip

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"testing"
	"time"
)

type testUpgrader struct {
	*Upgrader
	procs chan *testProcess
}

func newTestUpgrader(opts Options) *testUpgrader {
	env, procs := testEnv()
	u, err := newUpgrader(env, opts)
	if err != nil {
		panic(err)
	}
	err = u.Ready()
	if err != nil {
		panic(err)
	}

	return &testUpgrader{
		Upgrader: u,
		procs:    procs,
	}
}

func (tu *testUpgrader) upgradeProc(t *testing.T) (*testProcess, <-chan error) {
	t.Helper()

	ch := make(chan error, 1)
	go func() {
		for {
			err := tu.Upgrade()
			if err != errNotReady {
				ch <- err
				return
			}
		}
	}()

	select {
	case err := <-ch:
		t.Fatal("Upgrade failed:", err)
		return nil, nil

	case proc := <-tu.procs:
		return proc, ch
	}
}

var names = []string{"zaphod", "beeblebrox"}

func TestMain(m *testing.M) {
	upg, err := New(Options{})
	if errors.Is(err, ErrNotSupported) {
		fmt.Fprintln(os.Stderr, "Skipping tests, OS is not supported")
		os.Exit(0)
	}
	if err != nil {
		panic(err)
	}

	if upg.parent == nil {
		// Execute test suite if there is no parent.
		os.Exit(m.Run())
	}

	pid, err := upg.Fds.File("pid")
	if err != nil {
		panic(err)
	}

	if pid != nil {
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(os.Getpid()))
		pid.Write(buf)
		pid.Close()
	}

	parent, err := upg.Fds.File("hasParent")
	if err != nil {
		panic(err)
	}

	if parent != nil {
		if _, err := io.WriteString(parent, fmt.Sprint(upg.HasParent())); err != nil {
			panic(err)
		}
		parent.Close()
	}

	for _, name := range names {
		file, err := upg.Fds.File(name)
		if err != nil {
			panic(err)
		}
		if file == nil {
			continue
		}
		if _, err := io.WriteString(file, name); err != nil {
			panic(err)
		}
	}

	if err := upg.Ready(); err != nil {
		panic(err)
	}
}

func TestUpgraderOnOS(t *testing.T) {
	u, err := newUpgrader(stdEnv, Options{})
	if err != nil {
		t.Fatal("Can't create Upgrader:", err)
	}
	defer u.Stop()

	rPid, wPid, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rPid.Close()

	if err := u.Fds.AddFile("pid", wPid); err != nil {
		t.Fatal(err)
	}
	wPid.Close()

	rHasParent, wHasParent, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rHasParent.Close()

	if err := u.Fds.AddFile("hasParent", wHasParent); err != nil {
		t.Fatal(err)
	}
	wHasParent.Close()

	var readers []*os.File
	defer func() {
		for _, r := range readers {
			r.Close()
		}
	}()

	for _, name := range names {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, r)

		if err := u.Fds.AddFile(name, w); err != nil {
			t.Fatal(err)
		}
		w.Close()
	}

	if err := u.Upgrade(); err == nil {
		t.Error("Upgrade before Ready should return an error")
	}

	if err := u.Ready(); err != nil {
		t.Fatal("Ready failed:", err)
	}

	for {
		if err := u.Upgrade(); err == nil {
			break
		} else if err != errNotReady {
			t.Fatal("Upgrade failed:", err)
		}
	}

	// Close copies of write pipes, so that
	// reads below return EOF.
	u.Stop()

	buf := make([]byte, 8)
	if _, err := rPid.Read(buf); err != nil {
		t.Fatal(err)
	}

	if int(binary.LittleEndian.Uint64(buf)) == os.Getpid() {
		t.Error("Child did not execute in new process")
	}

	hasParentBytes, err := ioutil.ReadAll(rHasParent)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hasParentBytes, []byte("true")) {
		t.Fatal("Child did not recognize parent")
	}

	for i, name := range names {
		nameBytes, err := ioutil.ReadAll(readers[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(nameBytes, []byte(name)) {
			t.Fatalf("File %s has name %s in child", name, string(nameBytes))
		}
	}
}

func TestUpgraderCleanExit(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{})
	defer u.Stop()

	proc, errs := u.upgradeProc(t)

	proc.exit(nil)
	if err := <-errs; err == nil {
		t.Error("Expected Upgrade to return error when new child exits clean")
	}
}

func TestUpgraderUncleanExit(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{})
	defer u.Stop()

	proc, errs := u.upgradeProc(t)

	proc.exit(errors.New("some error"))
	if err := <-errs; err == nil {
		t.Error("Expected Upgrade to return error when new child exits unclean")
	}
}

func TestUpgraderTimeout(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{
		UpgradeTimeout: 10 * time.Millisecond,
	})
	defer u.Stop()

	new, errs := u.upgradeProc(t)

	if sig := new.recvSignal(nil); sig != os.Kill {
		t.Error("Expected os.Kill, got", sig)
	}

	if err := <-errs; err == nil {
		t.Error("Expected Upgrade to return error when new child times out")
	}
}

func TestUpgraderConcurrentUpgrade(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{})
	defer u.Stop()

	new, _ := u.upgradeProc(t)

	go new.recvSignal(nil)

	if err := u.Upgrade(); err == nil {
		t.Error("Expected Upgrade to refuse concurrent upgrade")
	}

	new.exit(nil)
}

func TestHasParent(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{})
	defer u.Stop()

	if u.HasParent() {
		t.Fatal("First process cannot have a parent")
	}
}

func TestUpgraderWaitForParent(t *testing.T) {
	t.Parallel()

	env, procs := testEnv()
	child, err := startChild(env, nil)
	if err != nil {
		t.Fatal(err)
	}

	proc := <-procs
	u, err := newUpgrader(&proc.env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Stop()

	if err := u.Ready(); err != nil {
		t.Fatal(err)
	}

	exited := make(chan error, 1)
	go func() {
		exited <- u.WaitForParent(context.Background())
	}()

	select {
	case <-exited:
		t.Fatal("Returned before parent exited")
	case <-time.After(time.Second):
	}

	readyFile := <-child.ready
	if err := readyFile.Close(); err != nil {
		t.Fatal(err)
	}

	if err := <-exited; err != nil {
		t.Fatal("Unexpected error:", err)
	}
}

func TestUpgraderReady(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{})
	defer u.Stop()

	new, errs := u.upgradeProc(t)

	_, exited, err := new.notify()
	if err != nil {
		t.Fatal("Can't notify Upgrader:", err)
	}

	if err := <-errs; err != nil {
		t.Fatal("Expected Upgrade to return nil when child is ready")
	}

	select {
	case <-u.Exit():
	default:
		t.Error("Expected Exit() to be closed when upgrade is done")
	}

	// Simulate the process exiting
	file := <-u.exitFd
	file.file.Close()

	select {
	case err := <-exited:
		if err != nil {
			t.Error("exit error", err)
		}
	case <-time.After(time.Second):
		t.Error("Child wasn't notified of parent exiting")
	}
}

func TestUpgraderShutdownCancelsUpgrade(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{})
	defer u.Stop()

	new, errs := u.upgradeProc(t)

	go new.recvSignal(nil)

	u.Stop()
	if err := <-errs; err == nil {
		t.Error("Upgrade doesn't return an error when Stopp()ed")
	}

	if err := u.Upgrade(); err == nil {
		t.Error("Upgrade doesn't return an error after Stop()")
	}
}

func TestReadyWritesPIDFile(t *testing.T) {
	t.Parallel()

	dir, err := ioutil.TempDir("", "tableflip")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	file := dir + "/pid"
	u := newTestUpgrader(Options{
		PIDFile: file,
	})
	defer u.Stop()

	if err := u.Ready(); err != nil {
		t.Fatal("Ready returned error:", err)
	}

	fh, err := os.Open(file)
	if err != nil {
		t.Fatal("PID file doesn't exist:", err)
	}
	defer fh.Close()

	var pid int
	if _, err := fmt.Fscan(fh, &pid); err != nil {
		t.Fatal("Can't read PID:", err)
	}

	if pid != os.Getpid() {
		t.Error("PID doesn't match")
	}
}

func TestWritePidFileWithoutPath(t *testing.T) {
	pidFile := "tableflip-test.pid"

	err := writePIDFile(pidFile)
	if err != nil {
		t.Fatal("Could not write pidfile:", err)
	}
	defer os.Remove(pidFile)

	// lets see if we are able to read the file back
	fh, err := os.Open(pidFile)
	if err != nil {
		t.Fatal("PID file doesn't exist:", err)
	}
	defer fh.Close()

	// just to be sure: check the pid for correctness
	// if something failed at a previous run we could be reading
	// a bogus pidfile
	var pid int
	if _, err := fmt.Fscan(fh, &pid); err != nil {
		t.Fatal("Can't read PID:", err)
	}

	if pid != os.Getpid() {
		t.Error("PID doesn't match")
	}
}

func BenchmarkUpgrade(b *testing.B) {
	for _, n := range []int{4, 400, 4000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			fds := newFds(nil)
			for i := 0; i < n; i += 2 {
				r, w, err := os.Pipe()
				if err != nil {
					b.Fatal(err)
				}

				err = fds.AddFile(strconv.Itoa(n), r)
				if err != nil {
					b.Fatal(err)
				}
				r.Close()

				err = fds.AddFile(strconv.Itoa(n), w)
				if err != nil {
					b.Fatal(err)
				}
				w.Close()
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				u, err := newUpgrader(stdEnv, Options{})
				if err != nil {
					b.Fatal("Can't create Upgrader:", err)
				}
				if err := u.Ready(); err != nil {
					b.Fatal("Can't call Ready:", err)
				}

				u.Fds = fds
				if err := u.Upgrade(); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()

			for _, f := range fds.used {
				f.Close()
			}
		})
	}
}
