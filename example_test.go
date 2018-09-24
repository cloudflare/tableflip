package tableflip_test

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"code.cfops.it/go/tableflip"
)

var (
	listenAddr = flag.String("listen", "localhost:8080", "`Address` to listen on")
	pidFile    = flag.String("pid-file", "", "`Path` to pid file")
)

func Example() {
	flag.Parse()
	log.SetPrefix(fmt.Sprintf("%d ", os.Getpid()))

	upg, err := tableflip.New(tableflip.Options{
		PIDFile: *pidFile,
	})
	if err != nil {
		panic(err)
	}
	defer upg.Stop()

	go handleUpgrades(upg)

	ln, err := upg.Fds.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalln("Can't listen:", err)
	}
	defer ln.Close()

	go handleClients(ln)

	log.Printf("ready")
	if err := upg.Ready(); err != nil {
		panic(err)
	}
	<-upg.Exit()
}

func handleUpgrades(upg *tableflip.Upgrader) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	for range sig {
		err := upg.Upgrade()
		if err != nil {
			log.Println("Upgrade failed:", err)
			continue
		}

		log.Println("Upgrade succeeded")
	}
}

func handleClients(ln net.Listener) {
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
}
