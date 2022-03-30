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

**`tableflip` works on Linux and macOS.**

## Using the library

```Go
upg, _ := tableflip.New(tableflip.Options{})
defer upg.Stop()

go func() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	for range sig {
		upg.Upgrade()
	}
}()

// Listen must be called before Ready
ln, _ := upg.Listen("tcp", "localhost:8080")
defer ln.Close()

go http.Serve(ln, nil)

if err := upg.Ready(); err != nil {
	panic(err)
}

<-upg.Exit()
```

Please see the more elaborate [graceful shutdown with net/http](http_example_test.go) example.

## Integration with `systemd`

```text
[Unit]
Description=Service using tableflip

[Service]
ExecStart=/path/to/binary -some-flag /path/to/pid-file
ExecReload=/bin/kill -HUP $MAINPID
PIDFile=/path/to/pid-file
```

See the [documentation](https://godoc.org/github.com/cloudflare/tableflip) as well.

The logs of a process using `tableflip` may go missing due to a [bug in journald](https://github.com/systemd/systemd/issues/13708),
which has been fixed by systemd v244 release. If you are running an older version
of systemd, you can work around this by logging directly to journald, for example
by using [go-systemd/journal](https://godoc.org/github.com/coreos/go-systemd/journal)
and looking for the [$JOURNAL_STREAM](https://www.freedesktop.org/software/systemd/man/systemd.exec.html#$JOURNAL_STREAM)
environment variable.
