module github.com/cloudflare/tableflip/examples/grpc-server

go 1.14

require (
	github.com/cloudflare/tableflip v0.0.0
	google.golang.org/grpc v1.45.0
)

replace github.com/cloudflare/tableflip => ../..
