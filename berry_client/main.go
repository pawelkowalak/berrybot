package main

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	pb "github.com/viru/berrybot/proto"
)

func main() {
	conn, err := grpc.Dial("localhost:31337", grpc.WithInsecure())
	if err != nil {
		grpclog.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewDriverClient(conn)

	r, err := c.Forward(context.Background(), &pb.ForwardRequest{Ok: true})
	if err != nil {
		grpclog.Fatalf("could not forward: %v", err)
	}
	grpclog.Printf("Forwarding!: %t", r.Ok)
}
