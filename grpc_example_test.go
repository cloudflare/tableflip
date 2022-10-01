package tableflip_test

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudflare/tableflip"
	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

// This shows how to use the upgrader
// with the graceful shutdown facilities of grpc server.
func Example_GrpcServerShutdown() {
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
				log.Println("Upgrade failed:", err)
			}
		}
	}()

	// Listen must be called before Ready
	ln, err := upg.Fds.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalln("Can't listen:", err)
	}

	// set rpc server
	s := grpc.NewServer()                  // create rpc server
	pb.RegisterGreeterServer(s, &server{}) // register service
	go func() {
		err := s.Serve(ln)
		log.Printf("server listening at %v", ln.Addr())
		if err != http.ErrServerClosed {
			log.Printf("failed to serve: %v", err)
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

	// Wait for the server gracefully stops
	s.GracefulStop()
}

// server is used to implement helloworld.GreeterServer.
type server struct {
	pb.UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Printf("Received: %v", in.GetName())
	return &pb.HelloReply{Message: fmt.Sprintf("Hello %s", in.GetName())}, nil
}
