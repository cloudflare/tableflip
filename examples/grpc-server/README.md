# Graceful gRPC server upgrades

This example transfers the listening socket to a new process while the old gRPC server finishes its active RPCs. It registers the standard gRPC health service and server reflection, so no generated service code is required.

Build and start the server:

```sh
go build -o tableflip-grpc .
./tableflip-grpc -pid-file ./tableflip-grpc.pid
```

With [`grpcurl`](https://github.com/fullstorydev/grpcurl) installed, check the server:

```sh
grpcurl -plaintext localhost:8080 grpc.health.v1.Health/Check
```

Start an upgrade by sending `SIGHUP` to the current process:

```sh
kill -HUP "$(cat tableflip-grpc.pid)"
```

The replacement process inherits the listening socket and calls `Ready` before the old process stops accepting connections. The old gRPC server then calls `GracefulStop`, which lets active RPCs finish. If they do not finish within `-shutdown-timeout`, the server calls `Stop` and terminates them.
