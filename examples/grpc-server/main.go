package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudflare/tableflip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

var (
	listenAddr      = flag.String("listen", "localhost:8080", "address to listen on")
	pidFile         = flag.String("pid-file", "", "path to the tableflip PID file")
	shutdownTimeout = flag.Duration("shutdown-timeout", 30*time.Second, "maximum time to drain active RPCs")
)

func main() {
	flag.Parse()
	log.SetPrefix(fmt.Sprintf("%d ", os.Getpid()))

	upg, err := tableflip.New(tableflip.Options{PIDFile: *pidFile})
	if err != nil {
		log.Fatal(err)
	}
	defer upg.Stop()

	go upgradeOnSignal(upg)

	ln, err := upg.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatal(err)
	}

	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(ln)
	}()

	if err := upg.Ready(); err != nil {
		log.Fatal(err)
	}
	log.Printf("serving gRPC on %s", *listenAddr)

	select {
	case <-upg.Exit():
		healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		if !gracefulStop(server, *shutdownTimeout) {
			log.Printf("graceful shutdown timed out after %s; stopped active RPCs", *shutdownTimeout)
		}
	case err := <-serveErr:
		if err != nil {
			log.Fatal(err)
		}
	}
}

func upgradeOnSignal(upg *tableflip.Upgrader) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)
	for range signals {
		if err := upg.Upgrade(); err != nil {
			log.Printf("upgrade failed: %v", err)
		}
	}
}

type gracefulServer interface {
	GracefulStop()
	Stop()
}

// gracefulStop waits for active RPCs and force-stops them after the deadline.
func gracefulStop(server gracefulServer, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		return true
	case <-timer.C:
		server.Stop()
		return false
	}
}
