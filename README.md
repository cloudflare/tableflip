# Graceful process restarts in Go

[![](https://godoc.org/github.com/cloudflare/tableflip?status.svg)](https://godoc.org/github.com/cloudflare/tableflip)

It is sometimes useful to update the running code and / or configuration of a
network service, without disrupting existing connections. Usually, this is
achieved by starting a new process, somehow transferring clients to it and
then exiting the old process.

There are [many ways to implement graceful upgrades](https://blog.cloudflare.com/graceful-upgrades-in-go/).
They vary wildly in the trade-offs they make, and how much control they afford the user. This library
has the following goals:

* No old code keeps running after a successful upgrade
* The new process has a grace period for performing initialisation
* Crashing during initialisation is OK
* Only a single upgrade is ever run in parallel

It's easy to get started:

```Go
upg, err := tableflip.New(tableflip.Options{})
if err != nil {
	panic(err)
}
defer upg.Stop()

go func() {
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
}()

ln, err := upg.Fds.Listen("tcp", "localhost:8080")
if err != nil {
	log.Fatalln("Can't listen:", err)
}

var server http.Server
go server.Serve(ln)

if err := upg.Ready(); err != nil {
	panic(err)
}
<-upg.Exit()

time.AfterFunc(30*time.Second, func() {
	os.Exit(1)
})

_ = server.Shutdown(context.Background())
```
