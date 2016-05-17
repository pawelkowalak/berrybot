package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os/exec"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/kidoman/embd"
	_ "github.com/kidoman/embd/host/rpi" // This loads the RPi driver
	"google.golang.org/grpc"

	pb "github.com/viru/berrybot/proto"
)

// server is used to implement hellowrld.GreeterServer.
type server struct {
	echoFront, echoRear *echo
}

type echo struct {
	echo embd.DigitalPin
	trig embd.DigitalPin
	dist int64
}

func newEcho(trigPin, echoPin int) (*echo, error) {
	var e echo
	var err error
	e.trig, err = embd.NewDigitalPin(trigPin)
	if err != nil {
		return nil, fmt.Errorf("can't init trigger pin: %v", err)
	}
	e.echo, err = embd.NewDigitalPin(echoPin)
	if err != nil {
		return nil, fmt.Errorf("can't init echo pin: %v", err)
	}

	// Set direction.
	if err := e.trig.SetDirection(embd.Out); err != nil {
		return nil, fmt.Errorf("can't set trigger direction: %v", err)
	}
	if err := e.echo.SetDirection(embd.In); err != nil {
		return nil, fmt.Errorf("can't set echo direction: %v", err)
	}
	return &e, nil
}

func (e *echo) runDistancer() {
	for {
		if err := e.trig.Write(embd.Low); err != nil {
			log.Warnf("can't set trigger to low: %v", err)
			continue
		}
		time.Sleep(time.Second * 1)
		log.Info("measuring")
		if err := e.trig.Write(embd.High); err != nil {
			log.Warnf("can't set trigger to high: %v", err)
			continue
		}
		time.Sleep(time.Microsecond * 10)
		if err := e.trig.Write(embd.Low); err != nil {
			log.Warnf("can't set trigger to low: %v", err)
			continue
		}

		dur, err := e.echo.TimePulse(embd.High)
		if err != nil {
			log.Warnf("can't time pulse: %v", err)
			continue
		}
		log.Infof("distance: %dcm", dur.Nanoseconds()/1000*34/1000/2)
		e.dist = dur.Nanoseconds() / 1000 * 34 / 1000 / 2
	}
}

func (e *echo) close() {
	e.echo.Close()
	e.trig.Close()
}

const (
	sensorUnknown = iota
	sensorFront
	sensorRear
)

func (s *server) Measure(sensor *pb.Sensor, stream pb.Telemetry_MeasureServer) error {
	if sensor.Id == sensorFront {
		stream.Send(*steering.Distance)
	}
	return nil
}

func (s *server) Drive(stream pb.Driver_DriveServer) error {
	leftOn, err := embd.NewDigitalPin(23)
	if err != nil {
		log.Fatalf("Can't init trigger pin: %v", err)
	}
	defer leftOn.Close()
	leftFwd, err := embd.NewDigitalPin(4)
	if err != nil {
		log.Fatalf("Can't init trigger pin: %v", err)
	}
	defer leftFwd.Close()
	rightOn, err := embd.NewDigitalPin(24)
	if err != nil {
		log.Fatalf("Can't init trigger pin: %v", err)
	}
	defer rightOn.Close()
	rightFwd, err := embd.NewDigitalPin(17)
	if err != nil {
		log.Fatalf("Can't init trigger pin: %v", err)
	}
	defer rightFwd.Close()
	if err := leftOn.SetDirection(embd.Out); err != nil {
		log.Fatalf("Can't init trigger pin: %v", err)
	}
	if err := leftFwd.SetDirection(embd.Out); err != nil {
		log.Fatalf("Can't init trigger pin: %v", err)
	}
	if err := rightOn.SetDirection(embd.Out); err != nil {
		log.Fatalf("Can't init trigger pin: %v", err)
	}
	if err := rightFwd.SetDirection(embd.Out); err != nil {
		log.Fatalf("Can't init trigger pin: %v", err)
	}

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
		//		switch {
		//		// Full stop.
		//		case d.Dy > -5 && d.Dy < 5 && d.Dx > -5 && d.Dx < 5:
		//			leftOn.Write(embd.Low)
		//			rightOn.Write(embd.Low)
		//		// Forward.
		//		case d.Dy > 5 && d.Dx > -5 && d.Dx < 5:
		//			leftOn.Write(embd.High)
		//			leftFwd.Write(embd.High)
		//			rightOn.Write(embd.High)
		//			rightFwd.Write(embd.High)
		//		// Backward.
		//		case d.Dy < -5 && d.Dx > -5 && d.Dx < 5:
		//			leftOn.Write(embd.High)
		//			leftFwd.Write(embd.Low)
		//			rightOn.Write(embd.High)
		//			rightFwd.Write(embd.Low)
		//		// Sharp right.
		//		case d.Dx > 5 && d.Dy > -5 && d.Dy < 5:
		//			leftOn.Write(embd.High)
		//			leftFwd.Write(embd.High)
		//			rightOn.Write(embd.High)
		//			rightFwd.Write(embd.Low)
		//		// Sharp left.
		//		case d.Dx < -5 && d.Dy > -5 && d.Dy < 5:
		//			leftOn.Write(embd.High)
		//			leftFwd.Write(embd.Low)
		//			rightOn.Write(embd.High)
		//			rightFwd.Write(embd.High)
		//		// Forward + right.
		//		case d.Dx > 5 && d.Dy > 5:
		//			leftOn.Write(embd.High)
		//			leftFwd.Write(embd.High)
		//			rightOn.Write(embd.Low)
		//			rightFwd.Write(embd.High)
		//		// Forward + left.
		//		case d.Dx < -5 && d.Dy > 5:
		//			leftOn.Write(embd.Low)
		//			leftFwd.Write(embd.High)
		//			rightOn.Write(embd.High)
		//			rightFwd.Write(embd.High)
		//		// Backward + right.
		//		case d.Dx > 5 && d.Dy < -5:
		//			leftOn.Write(embd.High)
		//			leftFwd.Write(embd.Low)
		//			rightOn.Write(embd.Low)
		//			rightFwd.Write(embd.Low)
		//		// Backward + left.
		//		case d.Dx < -5 && d.Dy < -5:
		//			leftOn.Write(embd.Low)
		//			leftFwd.Write(embd.Low)
		//			rightOn.Write(embd.High)
		//			rightFwd.Write(embd.Low)
		//		}
	}
}

func (s *server) GetImage(image *pb.Image, stream pb.Driver_GetImageServer) error {
	if image.Live {
		for {
			out, err := exec.Command("/bin/cat", "/home/pi/space.jpg").Output() // FIXME: needs memprofiling
			//			out, err := exec.Command("/usr/bin/raspistill", "-n", "-t", "100", "-o", "-").Output() // FIXME: needs memprofiling
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

	// Initialize GPIO.
	var err error
	if err = embd.InitGPIO(); err != nil {
		log.Fatalf("Can't init GPIO: %v", err)
	}
	defer embd.CloseGPIO()
	front, err := newEcho(9, 10)
	if err != nil {
		log.Fatalf("Can't init front echo: %v", err)
	}
	defer front.close()
	rear, err := newEcho(19, 20)
	if err != nil {
		log.Fatalf("Can't init rear echo: %v", err)
	}
	defer rear.close()
	go front.runDistancer()
	go rear.runDistancer()

	// Listen for GRPC connections.
	lis, err := net.Listen("tcp", ":"+*grpcPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	srv := server{echoFront: front, echoRear: rear}
	s := grpc.NewServer()
	pb.RegisterDriverServer(s, &srv)
	pb.RegisterTelemetryServer(s, &srv)

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
