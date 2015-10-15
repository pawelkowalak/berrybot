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

type gpioMock struct {
	name string
}

func (g *gpioMock) High() {
	// log.Infof("engine %s: high", g.name)
}

func (g *gpioMock) Low() {
	// log.Infof("engine %s: low", g.name)
}

func (s *server) Drive(stream pb.Driver_DriveServer) error {
	// err := rpio.Open()
	// if err != nil {
	// 	log.Fatalf("can't open rpio: %v", err)
	// }
	// defer rpio.Close()
	// leftOn := rpio.Pin(23)
	// leftFwd := rpio.Pin(4)
	// rightOn := rpio.Pin(24)
	// rightFwd := rpio.Pin(17)
	// leftOn.Output()
	// leftFwd.Output()
	// rightOn.Output()
	// rightFwd.Output()
	leftOn := gpioMock{"leftOn"}
	rightOn := gpioMock{"rightOn"}
	leftFwd := gpioMock{"leftFwd"}
	rightFwd := gpioMock{"rightFwd"}
	for {
		d, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.DirectionReply{
				Ok: true,
			})
		}
		if err != nil {
			return err
		}
		// log.WithFields(log.Fields{
		// 	"dx": d.Dx,
		// 	"dy": d.Dy,
		// }).Info("Direction")
		switch {
		// Full stop.
		case d.Dy > -5 && d.Dy < 5 && d.Dx > -5 && d.Dx < 5:
			leftOn.Low()
			rightOn.Low()
			log.Info("full stop")
		// Forward.
		case d.Dy > 5 && d.Dx > -5 && d.Dx < 5:
			leftOn.High()
			leftFwd.High()
			rightOn.High()
			rightFwd.High()
			log.Info("fwd")
		// Backward.
		case d.Dy < -5 && d.Dx > -5 && d.Dx < 5:
			leftOn.High()
			leftFwd.Low()
			rightOn.High()
			rightFwd.Low()
			log.Info("back")
		// Sharp right.
		case d.Dx > 5 && d.Dy > -5 && d.Dy < 5:
			leftOn.High()
			leftFwd.High()
			rightOn.High()
			rightFwd.Low()
			log.Info("sharp right")
		// Sharp left.
		case d.Dx < -5 && d.Dy > -5 && d.Dy < 5:
			leftOn.High()
			leftFwd.High()
			rightOn.High()
			rightFwd.Low()
			log.Info("sharp left")
		// Forward + right.
		case d.Dx > 5 && d.Dy > 5:
			leftOn.High()
			leftFwd.High()
			rightOn.Low()
			rightFwd.High()
			log.Info("forward right")
		// Forward + left.
		case d.Dx < -5 && d.Dy > 5:
			leftOn.Low()
			leftFwd.High()
			rightOn.High()
			rightFwd.High()
			log.Info("forward left")
		// Backward + right.
		case d.Dx > 5 && d.Dy < -5:
			leftOn.High()
			leftFwd.Low()
			rightOn.Low()
			rightFwd.Low()
			log.Info("back right")
		// Backward + left.
		case d.Dx < -5 && d.Dy < -5:
			leftOn.Low()
			leftFwd.Low()
			rightOn.High()
			rightFwd.Low()
			log.Info("back left")
		}
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

	broadcastAddr := "255.255.255.255:8032"
	dst, err := net.ResolveUDPAddr("udp", broadcastAddr)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Infof("Starting to broadcast our port %s on %s", grpcPort, broadcastAddr)
		for {
			if _, err := c.WriteTo([]byte(*grpcPort), dst); err != nil {
				log.Fatal(err)
			}
			time.Sleep(time.Second)
		}
	}()

	// Start serving GRPC.
	log.Fatal(s.Serve(lis))
}
