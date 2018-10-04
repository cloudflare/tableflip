package tableflip_test

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"code.cfops.it/go/tableflip"
)

var listenAddr = flag.String("listen", "localhost:0", "`Address` to listen on")

func Example() {
	flag.Parse()
	pid := os.Getpid()

	upg, files, err := tableflip.New(tableflip.Options{})
	if err != nil {
		panic(err)
	}
	defer upg.Stop()

	if len(files) == 0 {
		ln, err := net.Listen("tcp", *listenAddr)
		if err != nil {
			log.Fatalln("Can't listen:", err)
		}

		log.Printf("%d: listening on %s", pid, ln.Addr())

		err = tableflip.AddListener(files, "server", ln.(tableflip.Listener))
		if err != nil {
			log.Fatalln("Can't add listener:", err)
		}
	}

	// NB: Be careful not to modify files, otherwise the child
	// won't receive them.
	go handleUpgrades(upg, files)

	lns, err := tableflip.Listeners(files)
	if err != nil {
		log.Fatalln("Can't get listeners:", err)
	}

	ln := lns["server"]
	defer ln.Close()

	go handleClients(ln)

	log.Printf("%d: ready", pid)
	upg.Ready()
	<-upg.Exit()
}

func handleUpgrades(upg *tableflip.Upgrader, files map[string]*os.File) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	for range sig {
		err := upg.Upgrade(files)
		if err != nil {
			log.Println("Upgrade failed:", err)
			continue
		}

		log.Println("Upgrade succeeded")
	}
}

func handleClients(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}

		go func() {
			c.Write([]byte("It is a mistake to think you can solve any major problems just with potatoes.\n"))
			c.Close()
		}()
	}
}
