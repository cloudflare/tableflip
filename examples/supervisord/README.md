# Running tableflip under supervisord

A successful tableflip upgrade replaces the process that supervisord started. The replacement keeps serving, but
supervisord sees its original child exit. If automatic restarts are enabled, it then starts a second copy of the
service.

The `tableflip-supervisor` proxy in this directory remains attached to supervisord while the service PID changes. It
reads the PID file written by `tableflip.Upgrader.Ready`, forwards signals to the current service process, and exits if
that process stops without handing off to a replacement.

Supervisor also ships a program named [`pidproxy`](https://supervisord.org/subprocess.html#pidproxy-program). Its
[main loop exits](https://github.com/Supervisor/supervisor/blob/main/supervisor/pidproxy.py) when the command it
started exits. Because a tableflip upgrade deliberately exits that process, the standard proxy does not remain
attached after the first upgrade.

## Build the proxy

From the repository root:

```sh
go build -o /usr/local/bin/tableflip-supervisor ./examples/supervisord
```

Your service must pass the same PID-file path to tableflip:

```go
upg, err := tableflip.New(tableflip.Options{
	PIDFile: "/run/myservice.pid",
})
```

## Configure supervisord

Copy [`supervisord.conf`](supervisord.conf) into your Supervisor configuration and replace the example paths and user.
The proxy syntax is:

```text
tableflip-supervisor PID_FILE -- COMMAND [ARG...]
```

The PID-file directory must be writable only by the account that runs the service. The proxy trusts the PID stored in
that file when it forwards signals.

After deploying a new binary, request a tableflip upgrade through the proxy:

```sh
supervisorctl signal HUP myservice
```

Do not use `supervisorctl restart` for a tableflip upgrade; it stops the running process before starting the new one.
Regular `supervisorctl stop` and `start` commands continue to work. If the service does not stop before
`stopwaitsecs`, `killasgroup=true` lets supervisord terminate the proxy and any remaining tableflip processes.
