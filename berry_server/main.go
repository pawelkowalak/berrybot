package main

import (
	"fmt"
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	pb "github.com/viru/berrybot/proto"
)

// server is used to implement hellowrld.GreeterServer.
type server struct{}

func (s *server) Forward(ctx context.Context, in *pb.ForwardRequest) (*pb.ForwardReply, error) {
	grpclog.Printf("got message! %t", in.Ok)
	return &pb.ForwardReply{Ok: true}, nil
}

func main() {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 31337))
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterDriverServer(s, &server{})
	s.Serve(lis)
}
