package tableflip_test

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudflare/tableflip"
)

// This shows how to use the Upgrader
// with a listener based service.
func Example_tcpServer() {
	var (
		listenAddr = flag.String("listen", "localhost:8080", "`Address` to listen on")
		pidFile    = flag.String("pid-file", "", "`Path` to pid file")
	)

	flag.Parse()
	log.SetPrefix(fmt.Sprintf("%d ", os.Getpid()))

	upg, err := tableflip.New(tableflip.Options{
		PIDFile: *pidFile,
	})
	if err != nil {
		panic(err)
	}
	defer upg.Stop()

	// Do an upgrade on SIGHUP
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP)
		for range sig {
			err := upg.Upgrade()
			if err != nil {
				log.Println("upgrade failed:", err)
			}
		}
	}()

	ln, err := upg.Fds.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalln("Can't listen:", err)
	}

	go func() {
		defer ln.Close()

		log.Printf("listening on %s", ln.Addr())

		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			go func() {
				c.SetDeadline(time.Now().Add(time.Second))
				c.Write([]byte("It is a mistake to think you can solve any major problems just with potatoes.\n"))
				c.Close()
			}()
		}
	}()

	log.Printf("ready")
	if err := upg.Ready(); err != nil {
		panic(err)
	}
	<-upg.Exit()
}
