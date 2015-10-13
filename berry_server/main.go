package main

import (
	"fmt"
	"io"
	"net"

	"google.golang.org/grpc"

	log "github.com/Sirupsen/logrus"
	pb "github.com/viru/berrybot/proto"
)

// server is used to implement hellowrld.GreeterServer.
type server struct{}

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
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 31337))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterDriverServer(s, &server{})
	s.Serve(lis)
}
