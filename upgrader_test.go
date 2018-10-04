package tableflip

import (
	"encoding/binary"
	"errors"
	"fmt"
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

func newTestUpgrader(opts Options) (*testUpgrader, map[string]*os.File) {
	env, procs := testEnv()
	u, files, err := newUpgrader(env, opts)
	if err != nil {
		panic(err)
	}

	return &testUpgrader{
		Upgrader: u,
		procs:    procs,
	}, files
}

func (tu *testUpgrader) upgradeAsync() <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- tu.Upgrade(nil)
	}()
	return ch
}

func TestMain(m *testing.M) {
	upg, files, err := New(Options{})
	if err != nil {
		panic(err)
	}

	if upg.parent == nil {
		// Execute test suite if there is no parent.
		os.Exit(m.Run())
	}

	if pid := files["pid"]; pid != nil {
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(os.Getpid()))
		pid.Write(buf)
		pid.Close()
	}
	delete(files, "pid")

	if files["benchmark"] == nil {
		for name, file := range files {
			file.WriteString(name)
			file.Close()
		}
	}

	if err := upg.Ready(); err != nil {
		panic(err)
	}
}

func TestUpgraderOnOS(t *testing.T) {
	u, files, err := newUpgrader(stdEnv, Options{})
	if err != nil {
		t.Fatal("Can't create Upgrader:", err)
	}
	defer u.Stop()

	if len(files) != 0 {
		t.Error("Expected files on clean Upgrader to be empty")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	rA, wA, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rA.Close()
	defer wA.Close()

	rB, wB, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rB.Close()
	defer wB.Close()

	send := map[string]*os.File{
		"pid": w,
		"A":   wA,
		"B":   wB,
	}

	if err := u.Upgrade(send); err != nil {
		t.Fatal("Upgrade failed:", err)
	}

	// Close write end of the pipe so that reading returns EOF
	w.Close()
	wA.Close()
	wB.Close()

	buf := make([]byte, 8)
	if _, err := r.Read(buf); err != nil {
		t.Fatal(err)
	}

	if int(binary.LittleEndian.Uint64(buf)) == os.Getpid() {
		t.Error("Child did not execute in new process")
	}

	name, err := ioutil.ReadAll(rA)
	if err != nil {
		t.Fatal("Can't read from A", err)
	}

	if string(name) != "A" {
		t.Errorf("File A has name %s in child", string(name))
	}

	name, err = ioutil.ReadAll(rB)
	if err != nil {
		t.Fatal("Can't read from B", err)
	}

	if string(name) != "B" {
		t.Errorf("File B has name %s in child", string(name))
	}
}

func TestUpgraderCleanExit(t *testing.T) {
	t.Parallel()

	u, _ := newTestUpgrader(Options{})
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

	u, _ := newTestUpgrader(Options{})
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

	u, _ := newTestUpgrader(Options{
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

	u, _ := newTestUpgrader(Options{})
	defer u.Stop()

	u.upgradeAsync()

	new := <-u.procs
	go new.recvSignal(nil)

	if err := u.Upgrade(nil); err == nil {
		t.Error("Expected Upgrade to refuse concurrent upgrade")
	}

	new.exit(nil)
}

func TestUpgraderReady(t *testing.T) {
	t.Parallel()

	u, _ := newTestUpgrader(Options{})
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
	case <-exited:
	case <-time.After(time.Second):
		t.Error("Child wasn't notified of parent exiting")
	}
}

func TestUpgraderShutdownCancelsUpgrade(t *testing.T) {
	t.Parallel()

	u, _ := newTestUpgrader(Options{})
	defer u.Stop()

	errs := u.upgradeAsync()

	new := <-u.procs
	go new.recvSignal(nil)

	u.Stop()
	if err := <-errs; err == nil {
		t.Error("Upgrade doesn't return an error when Stopp()ed")
	}

	if err := u.Upgrade(nil); err == nil {
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
	u, _ := newTestUpgrader(Options{
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
			files := make(map[string]*os.File, n)
			for i := 0; i < n; i += 2 {
				a, b, err := os.Pipe()
				if err != nil {
					panic(err)
				}

				files[strconv.Itoa(n)] = a
				files[strconv.Itoa(n+1)] = b
			}
			files["benchmark"] = files["0"]
			delete(files, "0")

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				u, _, err := newUpgrader(stdEnv, Options{})
				if err != nil {
					b.Fatal("Can't create Upgrader:", err)
				}

				if err := u.Upgrade(files); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()

			for _, f := range files {
				f.Close()
			}
		})
	}
}
