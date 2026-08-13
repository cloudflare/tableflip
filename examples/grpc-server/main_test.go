package main

import (
	"sync"
	"testing"
	"time"
)

type testServer struct {
	releaseGraceful chan struct{}
	stopCalled      chan struct{}
	stopOnce        sync.Once
}

func newTestServer() *testServer {
	return &testServer{
		releaseGraceful: make(chan struct{}),
		stopCalled:      make(chan struct{}),
	}
}

func (server *testServer) GracefulStop() {
	<-server.releaseGraceful
}

func (server *testServer) Stop() {
	server.stopOnce.Do(func() {
		close(server.stopCalled)
		close(server.releaseGraceful)
	})
}

func TestGracefulStopCompletesBeforeDeadline(t *testing.T) {
	server := newTestServer()
	close(server.releaseGraceful)

	if !gracefulStop(server, time.Second) {
		t.Fatal("expected graceful shutdown")
	}

	select {
	case <-server.stopCalled:
		t.Fatal("Stop called after GracefulStop completed")
	default:
	}
}

func TestGracefulStopForcesShutdownAtDeadline(t *testing.T) {
	server := newTestServer()

	if gracefulStop(server, time.Millisecond) {
		t.Fatal("expected forced shutdown")
	}

	select {
	case <-server.stopCalled:
	default:
		t.Fatal("Stop was not called")
	}
}
