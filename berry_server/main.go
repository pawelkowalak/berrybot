package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os/exec"
	"time"

	"google.golang.org/grpc"

	log "github.com/Sirupsen/logrus"
	"github.com/stianeikeland/go-rpio"
	pb "github.com/viru/berrybot/proto"
)

// server is used to implement hellowrld.GreeterServer.
type server struct{}

func (s *server) Drive(stream pb.Driver_DriveServer) error {
	err := rpio.Open()
	if err != nil {
		log.Warnf("can't open rpio: %v", err) // FIXME: is it ok to send message instead of returning error?
		return stream.SendMsg(&pb.DriveReply{
			Ok: false,
		})
	}
	defer rpio.Close()
	leftOn := rpio.Pin(23)
	leftFwd := rpio.Pin(4)
	rightOn := rpio.Pin(24)
	rightFwd := rpio.Pin(17)
	leftOn.Output()
	leftFwd.Output()
	rightOn.Output()
	rightFwd.Output()
	for {
		d, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.DriveReply{
				Ok: true,
			})
		}
		if err != nil {
			return err
		}
		log.WithFields(log.Fields{
			"dx": d.Dx,
			"dy": d.Dy,
		}).Info("Direction")
		switch {
		// Full stop.
		case d.Dy > -5 && d.Dy < 5 && d.Dx > -5 && d.Dx < 5:
			leftOn.Low()
			rightOn.Low()
		// Forward.
		case d.Dy > 5 && d.Dx > -5 && d.Dx < 5:
			leftOn.High()
			leftFwd.High()
			rightOn.High()
			rightFwd.High()
		// Backward.
		case d.Dy < -5 && d.Dx > -5 && d.Dx < 5:
			leftOn.High()
			leftFwd.Low()
			rightOn.High()
			rightFwd.Low()
		// Sharp right.
		case d.Dx > 5 && d.Dy > -5 && d.Dy < 5:
			leftOn.High()
			leftFwd.High()
			rightOn.High()
			rightFwd.Low()
		// Sharp left.
		case d.Dx < -5 && d.Dy > -5 && d.Dy < 5:
			leftOn.High()
			leftFwd.High()
			rightOn.High()
			rightFwd.Low()
		// Forward + right.
		case d.Dx > 5 && d.Dy > 5:
			leftOn.High()
			leftFwd.High()
			rightOn.Low()
			rightFwd.High()
		// Forward + left.
		case d.Dx < -5 && d.Dy > 5:
			leftOn.Low()
			leftFwd.High()
			rightOn.High()
			rightFwd.High()
		// Backward + right.
		case d.Dx > 5 && d.Dy < -5:
			leftOn.High()
			leftFwd.Low()
			rightOn.Low()
			rightFwd.Low()
		// Backward + left.
		case d.Dx < -5 && d.Dy < -5:
			leftOn.Low()
			leftFwd.Low()
			rightOn.High()
			rightFwd.Low()
		}
	}
}

func (s *server) GetImage(image *pb.Image, stream pb.Driver_GetImageServer) error {
	if image.Live {
		for {
			out, err := exec.Command("/bin/cat", "/Users/viru/Desktop/space.jpg").Output() // FIXME: needs memprofiling
			if err != nil {
				log.Fatal(err)
			}
			b := pb.ImageBytes{}
			b.Image = out
			log.Infof("sending %d bytes", len(b.Image))
			if err := stream.Send(&b); err != nil {
				e := fmt.Errorf("can't send image: %+v", err)
				log.Warning(e)
				return e
			}
			time.Sleep(time.Second)
		}
	}
	return nil
}

var grpcPort = flag.String("grpc-port", "31337", "gRPC listen port")

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

	broadcastAddr := "255.255.255.255:8032"
	dst, err := net.ResolveUDPAddr("udp", broadcastAddr)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Infof("Starting to broadcast our port %s on %s", *grpcPort, broadcastAddr)
		for {
			if _, err := c.WriteTo([]byte(*grpcPort), dst); err != nil {
				log.Warn(err)
			}
			time.Sleep(time.Second)
		}
	}()

	// Start serving GRPC.
	log.Fatal(s.Serve(lis))
}
