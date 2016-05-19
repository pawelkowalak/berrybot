package main

import (
	"flag"
	"fmt"
	"net"
	// "os/exec"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/kidoman/embd"
	_ "github.com/kidoman/embd/host/rpi" // This loads the RPi driver
	"google.golang.org/grpc"

	pb "github.com/viru/berrybot/proto"
)

// server is used to implement hellowrld.GreeterServer.
type server struct {
	front, rear *echo
	driver      driver
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

type driver struct {
	left, right *engine
}

func (d *driver) drive(dir *pb.Direction) {
	switch {
	// Full stop.
	case dir.Dy > -5 && dir.Dy < 5 && dir.Dx > -5 && dir.Dx < 5:
		d.left.pwr.Write(embd.Low)
		d.right.pwr.Write(embd.Low)
		log.Info("driver STOP")
	// Forward.
	case dir.Dy > 5 && dir.Dx > -5 && dir.Dx < 5:
		d.left.pwr.Write(embd.High)
		d.left.fwd.Write(embd.High)
		d.right.pwr.Write(embd.High)
		d.right.fwd.Write(embd.High)
		log.Info("driver FWD")
	// Backward.
	case dir.Dy < -5 && dir.Dx > -5 && dir.Dx < 5:
		d.left.pwr.Write(embd.High)
		d.left.fwd.Write(embd.Low)
		d.right.pwr.Write(embd.High)
		d.right.fwd.Write(embd.Low)
		log.Info("driver BACK")
	// Sharp right.
	case dir.Dx > 5 && dir.Dy > -5 && dir.Dy < 5:
		d.left.pwr.Write(embd.High)
		d.left.fwd.Write(embd.High)
		d.right.pwr.Write(embd.High)
		d.right.fwd.Write(embd.Low)
		log.Info("driver TURN RIGHT")
	// Sharp left.
	case dir.Dx < -5 && dir.Dy > -5 && dir.Dy < 5:
		d.left.pwr.Write(embd.High)
		d.left.fwd.Write(embd.Low)
		d.right.pwr.Write(embd.High)
		d.right.fwd.Write(embd.High)
		log.Info("driver TURN LEFT")
	// Forward + right.
	case dir.Dx > 5 && dir.Dy > 5:
		d.left.pwr.Write(embd.High)
		d.left.fwd.Write(embd.High)
		d.right.pwr.Write(embd.Low)
		d.right.fwd.Write(embd.High)
		log.Info("driver FWD RIGHT")
	// Forward + left.
	case dir.Dx < -5 && dir.Dy > 5:
		d.left.pwr.Write(embd.Low)
		d.left.fwd.Write(embd.High)
		d.right.pwr.Write(embd.High)
		d.right.fwd.Write(embd.High)
		log.Info("driver FWD LEFT")
	// Backward + right.
	case dir.Dx > 5 && dir.Dy < -5:
		d.left.pwr.Write(embd.High)
		d.left.fwd.Write(embd.Low)
		d.right.pwr.Write(embd.Low)
		d.right.fwd.Write(embd.Low)
		log.Info("driver BACK RIGHT")
	// Backward + left.
	case dir.Dx < -5 && dir.Dy < -5:
		d.left.pwr.Write(embd.Low)
		d.left.fwd.Write(embd.Low)
		d.right.pwr.Write(embd.High)
		d.right.fwd.Write(embd.Low)
		log.Info("driver BACK LEFT")
	}
}

type engine struct {
	fwd, pwr embd.DigitalPin
}

func newEngine(pwrPin, fwdPin int) (*engine, error) {
	var e engine
	var err error
	e.pwr, err = embd.NewDigitalPin(pwrPin)
	if err != nil {
		return nil, fmt.Errorf("can't init power pin: %v", err)
	}
	e.fwd, err = embd.NewDigitalPin(fwdPin)
	if err != nil {
		return nil, fmt.Errorf("can't init forward pin: %v", err)
	}

	// Set direction.
	if err := e.pwr.SetDirection(embd.Out); err != nil {
		return nil, fmt.Errorf("can't set power direction: %v", err)
	}
	if err := e.fwd.SetDirection(embd.Out); err != nil {
		return nil, fmt.Errorf("can't set forward direction: %v", err)
	}
	return &e, nil
}

func (e *engine) close() {
	e.pwr.Close()
	e.fwd.Close()
}

const (
	sensorUnknown = iota
	sensorFront
	sensorRear
)

func (s *server) Drive(stream pb.Driver_DriveServer) error {
	waitc := make(chan struct{})
	go func() {
		for {
			d, err := stream.Recv()
			if err != nil {
				log.Warnf("ERR from client: %v", err)
				close(waitc)
				return
			}
			log.WithFields(log.Fields{
				"dx": d.Dx,
				"dy": d.Dy,
			}).Info("Direction")
			s.driver.drive(d)
		}
	}()

	for {
		select {
		case <-time.After(time.Second):
			if err := stream.Send(&pb.Telemetry{Speed: 1, DistFront: 10, DistRear: 20}); err != nil {
				log.Errorf("can't send telemetry: %v", err)
				return err
			}
			log.Info("Sending telemetry!")
		case <-waitc:
			log.Info("got ERR from client, closing sending loop")
			return nil
		}
	}

}

// func (s *server) GetImage(image *pb.Image, stream pb.Driver_GetImageServer) error {
// 	if image.Live {
// 		for {
// 			out, err := exec.Command("/bin/cat", "/home/pi/space.jpg").Output() // FIXME: needs memprofiling
// 			//			out, err := exec.Command("/usr/bin/raspistill", "-n", "-t", "100", "-o", "-").Output() // FIXME: needs memprofiling
// 			if err != nil {
// 				log.Fatal(err)
// 			}
// 			b := pb.ImageBytes{}
// 			b.Image = out
// 			log.Infof("sending %d bytes", len(b.Image))
// 			if err := stream.Send(&b); err != nil {
// 				e := fmt.Errorf("can't send image: %+v", err)
// 				log.Warning(e)
// 				return e
// 			}
// 			time.Sleep(time.Second)
// 		}
// 	}
// 	return nil
// }

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

	left, err := newEngine(23, 4)
	if err != nil {
		log.Fatalf("Can't init left engine: %v", err)
	}
	defer left.close()
	right, err := newEngine(24, 17)
	if err != nil {
		log.Fatalf("Can't init right engine: %v", err)
	}
	defer right.close()

	// Listen for GRPC connections.
	lis, err := net.Listen("tcp", ":"+*grpcPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	srv := server{front: front, rear: rear, driver: driver{left: left, right: right}}
	s := grpc.NewServer()
	pb.RegisterDriverServer(s, &srv)
	// pb.RegisterTelemetryServer(s, &srv)

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
