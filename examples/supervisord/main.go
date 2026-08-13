//go:build !windows
// +build !windows

// tableflip-supervisor keeps supervisord attached to a service across
// tableflip upgrades and forwards signals to the process in its PID file.
package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const defaultPollInterval = 100 * time.Millisecond

func main() {
	log.SetFlags(0)

	pidFile, command, err := parseArgs(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
	)
	defer signal.Stop(signals)

	if err := runProxy(pidFile, command, signals, defaultPollInterval); err != nil {
		log.Fatal(err)
	}
}

func parseArgs(args []string) (string, []string, error) {
	if len(args) < 3 || args[1] != "--" {
		return "", nil, errors.New("usage: tableflip-supervisor PID_FILE -- COMMAND [ARG...]")
	}
	return args[0], args[2:], nil
}

func runProxy(pidFile string, command []string, signals <-chan os.Signal, pollInterval time.Duration) error {
	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale PID file: %v", err)
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %v", command[0], err)
	}

	directPID := cmd.Process.Pid
	activePID := directPID
	ready := false
	stopping := false
	directExit := make(chan error, 1)
	go func() {
		directExit <- cmd.Wait()
	}()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case sig := <-signals:
			pid, err := livePID(pidFile)
			if err == nil {
				activePID = pid
				ready = true
			}
			if err := signalProcess(activePID, sig); err != nil {
				return fmt.Errorf("forward %s to PID %d: %v", sig, activePID, err)
			}
			if isStopSignal(sig) {
				stopping = true
			}

		case err := <-directExit:
			directExit = nil
			pid, pidErr := livePID(pidFile)
			if pidErr == nil && pid != directPID {
				activePID = pid
				ready = true
				continue
			}
			if stopping {
				return nil
			}
			if err == nil {
				return errors.New("managed command exited before handing off to a replacement")
			}
			return fmt.Errorf("managed command exited: %v", err)

		case <-ticker.C:
			pid, err := livePID(pidFile)
			if err == nil {
				activePID = pid
				ready = true
				continue
			}

			if ready && !processAlive(activePID) {
				if stopping {
					return nil
				}
				return fmt.Errorf("managed process %d exited without handing off to a replacement", activePID)
			}
		}
	}
}

func readPID(path string) (int, error) {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid PID in %s", path)
	}
	return pid, nil
}

func livePID(path string) (int, error) {
	pid, err := readPID(path)
	if err != nil {
		return 0, err
	}
	if !processAlive(pid) {
		return 0, fmt.Errorf("process %d is not running", pid)
	}
	return pid, nil
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func signalProcess(pid int, sig os.Signal) error {
	syscallSignal, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("unsupported signal %v", sig)
	}
	return syscall.Kill(pid, syscallSignal)
}

func isStopSignal(sig os.Signal) bool {
	return sig == syscall.SIGINT || sig == syscall.SIGQUIT || sig == syscall.SIGTERM
}
