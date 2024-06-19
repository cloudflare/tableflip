package testing_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudflare/tableflip"
	"github.com/cloudflare/tableflip/testing"
)

type upgrader interface {
	Listen(network, addr string) (net.Listener, error)
	Stop()
	Upgrade() error
	Ready() error
	Exit() <-chan struct{}
}

// This shows how to use the upgrader
// with the graceful shutdown facilities of net/http
// and using the stub implementation if on an unsupported platform.
func Example_httpShutdown() {
	var (
		listenAddr = flag.String("listen", "localhost:8080", "`Address` to listen on")
		pidFile    = flag.String("pid-file", "", "`Path` to pid file")
	)

	flag.Parse()
	log.SetPrefix(fmt.Sprintf("%d ", os.Getpid()))

	var upg upgrader
	upg, err := tableflip.New(tableflip.Options{
		PIDFile: *pidFile,
	})
	if errors.Is(err, tableflip.ErrNotSupported) {
		upg, _ = testing.New()
	} else if err != nil {
		panic(err)
	}
	defer upg.Stop()

	// Do an upgrade on SIGHUP
	// NOTE: With `testing.Upgrader` this goroutine is useless
	// You may choose to enclose it inside an `if` statement block.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP)
		for range sig {
			err := upg.Upgrade()
			if err != nil {
				log.Println("Upgrade failed:", err)
			}
		}
	}()

	// Listen must be called before Ready
	ln, err := upg.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalln("Can't listen:", err)
	}

	server := http.Server{
		// Set timeouts, etc.
	}

	go func() {
		err := server.Serve(ln)
		if err != http.ErrServerClosed {
			log.Println("HTTP server:", err)
		}
	}()

	log.Printf("ready")
	if err := upg.Ready(); err != nil {
		panic(err)
	}
	<-upg.Exit()

	// Make sure to set a deadline on exiting the process
	// after upg.Exit() is closed. No new upgrades can be
	// performed if the parent doesn't exit.
	time.AfterFunc(30*time.Second, func() {
		log.Println("Graceful shutdown timed out")
		os.Exit(1)
	})

	// Wait for connections to drain.
	_ = server.Shutdown(context.Background())
}
