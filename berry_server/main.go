package main

import (
	"flag"
	"io"
	"net"
	"time"

	"google.golang.org/grpc"

	log "github.com/Sirupsen/logrus"
	pb "github.com/viru/berrybot/proto"
)

// server is used to implement hellowrld.GreeterServer.
type server struct{}

var grpcPort = flag.String("grpc-port", "31337", "gRPC listen port")

func (s *server) Drive(stream pb.Driver_DriveServer) error {
	for {
		direction, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.DirectionReply{
				Ok: true,
			})
		}
		if err != nil {
			return err
		}
		log.WithFields(log.Fields{
			"dx": direction.Dx,
			"dy": direction.Dy,
		}).Info("Direction")
	}
}

func main() {
	flag.Parse()
	// Listen for GRPC connections.
	lis, err := net.Listen("tcp", ":"+*grpcPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterDriverServer(s, &server{})

	// Open broadcast connection.
	c, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	dst, err := net.ResolveUDPAddr("udp", "255.255.255.255:8032")
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			log.Info("Broadcasting our port")
			if _, err := c.WriteTo([]byte(*grpcPort), dst); err != nil {
				log.Fatal(err)
			}
			time.Sleep(time.Second)
		}
	}()

	// Start serving GRPC.
	log.Fatal(s.Serve(lis))
}
