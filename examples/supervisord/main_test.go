//go:build !windows
// +build !windows

package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	pidFile, command, err := parseArgs([]string{"/run/service.pid", "--", "/usr/bin/service", "-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if pidFile != "/run/service.pid" {
		t.Fatalf("unexpected PID file %q", pidFile)
	}
	if len(command) != 2 || command[0] != "/usr/bin/service" || command[1] != "-flag" {
		t.Fatalf("unexpected command %q", command)
	}

	if _, _, err := parseArgs([]string{"service.pid", "/usr/bin/service"}); err == nil {
		t.Fatal("expected invalid arguments to fail")
	}
}

func TestProxyTracksReplacementProcess(t *testing.T) {
	if os.Getenv("TABLEFLIP_PROXY_HELPER") != "" {
		runHelperProcess()
		return
	}

	tempDir, err := ioutil.TempDir("", "tableflip-supervisor-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	pidFile := filepath.Join(tempDir, "service.pid")
	if err := os.Setenv("TABLEFLIP_PROXY_HELPER", "parent"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("TABLEFLIP_PROXY_HELPER")
	if err := os.Setenv("TABLEFLIP_PROXY_PID_FILE", pidFile); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("TABLEFLIP_PROXY_PID_FILE")

	signals := make(chan os.Signal, 1)
	result := make(chan error, 1)
	go func() {
		result <- runProxy(pidFile, []string{os.Args[0], "-test.run=TestProxyTracksReplacementProcess"}, signals, time.Millisecond)
	}()

	firstPID := waitForPID(t, pidFile, 0)
	secondPID := 0
	defer func() {
		syscall.Kill(firstPID, syscall.SIGKILL)
		if secondPID != 0 {
			syscall.Kill(secondPID, syscall.SIGKILL)
		}
	}()
	signals <- syscall.SIGHUP
	secondPID = waitForPID(t, pidFile, firstPID)
	if firstPID == secondPID {
		t.Fatal("upgrade did not replace the managed process")
	}

	select {
	case err := <-result:
		t.Fatalf("proxy exited during a successful handoff: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	signals <- syscall.SIGTERM
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("proxy returned an error while stopping: %v", err)
		}
	case <-time.After(5 * time.Second):
		syscall.Kill(secondPID, syscall.SIGKILL)
		t.Fatal("proxy did not exit after the managed process stopped")
	}
}

func waitForPID(t *testing.T, path string, previous int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pid, err := readPID(path)
		if err == nil && pid != previous {
			return pid
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("PID file %s was not updated", path)
	return 0
}

func runHelperProcess() {
	pidFile := os.Getenv("TABLEFLIP_PROXY_PID_FILE")
	if err := ioutil.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(2)
	}

	signals := make(chan os.Signal, 1)
	role := os.Getenv("TABLEFLIP_PROXY_HELPER")
	if role == "parent" {
		signal.Notify(signals, syscall.SIGHUP)
		<-signals

		cmd := exec.Command(os.Args[0], "-test.run=TestProxyTracksReplacementProcess")
		cmd.Env = replaceEnv(os.Environ(), "TABLEFLIP_PROXY_HELPER", "replacement")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		waitForReplacementPID(pidFile, os.Getpid())
		os.Exit(0)
	}

	signal.Notify(signals, syscall.SIGTERM)
	<-signals
	os.Exit(0)
}

func waitForReplacementPID(path string, previous int) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pid, err := readPID(path)
		if err == nil && pid != previous {
			return
		}
		time.Sleep(time.Millisecond)
	}
	os.Exit(4)
}

func replaceEnv(environ []string, name, value string) []string {
	prefix := name + "="
	for i, entry := range environ {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			environ[i] = prefix + value
			return environ
		}
	}
	return append(environ, prefix+value)
}
