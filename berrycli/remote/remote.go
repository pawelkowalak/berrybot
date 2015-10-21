// Package remote provides a service for gRPC clients that connects to the first
// available gRPC server broadcasting its services on UDP port 8032. Used by
// berrycli mobile app to control berrybot server.
package remote

import (
	"fmt"
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	log "github.com/Sirupsen/logrus"
	pb "github.com/viru/berrybot/proto"
)

// Service provides Stream object and connection status to the clients.
type Service struct {
	Stream    pb.Driver_DriveClient
	Connected bool
	conn      *grpc.ClientConn
}

// NewService initializes new remote service.
func NewService() *Service {
	return &Service{}
}

// Connect listens for UDP broadcasts on port 8032 and tries to connect to
// the first server it finds. This function blocks.
func (s *Service) Connect() {
	// Listen for bots on broadcast.
	c, err := net.ListenPacket("udp", ":8032")
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	port := make([]byte, 512)
	_, peer, err := c.ReadFrom(port)
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("Received port broadcast from %s", peer)
	host, _, err := net.SplitHostPort(peer.String())
	if err != nil {
		log.Fatalf("can't parse peer IP address %v", err)
	}

	// Connect to first discovered bot via GRPC.
	s.conn, err = grpc.Dial(fmt.Sprintf("%s:%s", host, string(port)), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	cli := pb.NewDriverClient(s.conn)
	s.Stream, err = cli.Drive(context.Background())
	if err != nil {
		log.Fatalf("%v.Drive(_) = _, %v", cli, err)
	}
	s.Connected = true
	log.Info("Connected")
}

// Close tries to close service connection if it exists.
func (s *Service) Close() {
	if s.Connected {
		s.conn.Close()
	}
}
