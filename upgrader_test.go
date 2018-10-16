package tableflip

import (
	"bytes"
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

func (tu *testUpgrader) upgradeAsync() <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- tu.Upgrade()
	}()
	return ch
}

var names = []string{"zaphod", "beeblebrox"}

func TestMain(m *testing.M) {
	upg, err := New(Options{})
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

	if err := u.Upgrade(); err != nil {
		t.Fatal("Upgrade failed:", err)
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

	errs := u.upgradeAsync()

	first := <-u.procs
	first.exit(nil)

	if err := <-errs; err == nil {
		t.Error("Expected Upgrade to return error when new child exits clean")
	}
}

func TestUpgraderUncleanExit(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{})
	defer u.Stop()

	errs := u.upgradeAsync()

	first := <-u.procs
	first.exit(errors.New("some error"))

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

	errs := u.upgradeAsync()

	new := <-u.procs
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

	u.upgradeAsync()

	new := <-u.procs
	go new.recvSignal(nil)

	if err := u.Upgrade(); err == nil {
		t.Error("Expected Upgrade to refuse concurrent upgrade")
	}

	new.exit(nil)
}

func TestUpgraderReady(t *testing.T) {
	t.Parallel()

	u := newTestUpgrader(Options{})
	defer u.Stop()

	errs := u.upgradeAsync()

	new := <-u.procs
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
	u.exitFd.file.Close()

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

	errs := u.upgradeAsync()

	new := <-u.procs
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
