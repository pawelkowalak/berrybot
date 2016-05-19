package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
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

const (
	safeStraightDist = 10
	safeTurningDist  = 5
)

func (s *server) drive(dir *pb.Direction) {
	switch {
	case dir.Dy > -5 && dir.Dy < 5 && dir.Dx > -5 && dir.Dx < 5:
		// Full stop.
		s.driver.left.pwr.Write(embd.Low)
		s.driver.right.pwr.Write(embd.Low)
		log.Info("driver STOP")
	case dir.Dy > 5 && dir.Dx > -5 && dir.Dx < 5 && s.front.dist > safeStraightDist:
		// Forward.
		s.driver.left.pwr.Write(embd.High)
		s.driver.left.fwd.Write(embd.High)
		s.driver.right.pwr.Write(embd.High)
		s.driver.right.fwd.Write(embd.High)
		log.Info("driver FWD")
	case dir.Dy < -5 && dir.Dx > -5 && dir.Dx < 5 && s.rear.dist > safeStraightDist:
		// Backward.
		s.driver.left.pwr.Write(embd.High)
		s.driver.left.fwd.Write(embd.Low)
		s.driver.right.pwr.Write(embd.High)
		s.driver.right.fwd.Write(embd.Low)
		log.Info("driver BACK")
	case dir.Dx > 5 && dir.Dy > -5 && dir.Dy < 5:
		// Sharp right.
		s.driver.left.pwr.Write(embd.High)
		s.driver.left.fwd.Write(embd.High)
		s.driver.right.pwr.Write(embd.High)
		s.driver.right.fwd.Write(embd.Low)
		log.Info("driver TURN RIGHT")
	case dir.Dx < -5 && dir.Dy > -5 && dir.Dy < 5:
		// Sharp left.
		s.driver.left.pwr.Write(embd.High)
		s.driver.left.fwd.Write(embd.Low)
		s.driver.right.pwr.Write(embd.High)
		s.driver.right.fwd.Write(embd.High)
		log.Info("driver TURN LEFT")
	case dir.Dx > 5 && dir.Dy > 5 && s.front.dist > safeTurningDist:
		// Forward + right.
		s.driver.left.pwr.Write(embd.High)
		s.driver.left.fwd.Write(embd.High)
		s.driver.right.pwr.Write(embd.Low)
		s.driver.right.fwd.Write(embd.High)
		log.Info("driver FWD RIGHT")
	case dir.Dx < -5 && dir.Dy > 5 && s.front.dist > safeTurningDist:
		// Forward + left.
		s.driver.left.pwr.Write(embd.Low)
		s.driver.left.fwd.Write(embd.High)
		s.driver.right.pwr.Write(embd.High)
		s.driver.right.fwd.Write(embd.High)
		log.Info("driver FWD LEFT")
	case dir.Dx > 5 && dir.Dy < -5 && s.rear.dist > safeTurningDist:
		// Backward + right.
		s.driver.left.pwr.Write(embd.High)
		s.driver.left.fwd.Write(embd.Low)
		s.driver.right.pwr.Write(embd.Low)
		s.driver.right.fwd.Write(embd.Low)
		log.Info("driver BACK RIGHT")
	case dir.Dx < -5 && dir.Dy < -5 && s.rear.dist > safeTurningDist:
		// Backward + left.
		s.driver.left.pwr.Write(embd.Low)
		s.driver.left.fwd.Write(embd.Low)
		s.driver.right.pwr.Write(embd.High)
		s.driver.right.fwd.Write(embd.Low)
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
			s.drive(d)
		}
	}()

	for {
		select {
		case <-time.After(time.Second):
			if err := stream.Send(&pb.Telemetry{Speed: 1, DistFront: int32(s.front.dist), DistRear: int32(s.rear.dist)}); err != nil {
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

	// Open broadcast connection.
	bcast, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Fatal(err)
	}
	defer bcast.Close()

	broadcastAddr := "255.255.255.255:8032"
	dst, err := net.ResolveUDPAddr("udp", broadcastAddr)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Infof("Starting to broadcast our port %s on %s", *grpcPort, broadcastAddr)
		for {
			if _, err := bcast.WriteTo([]byte(*grpcPort), dst); err != nil {
				log.Warn(err)
			}
			time.Sleep(time.Second)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-c
		log.Infof("Got %s, trying to shutdown gracefully", sig.String())
		front.close()
		rear.close()
		left.close()
		right.close()
		embd.CloseGPIO()
		lis.Close()
		bcast.Close()
		os.Exit(0)
	}()

	// Start serving GRPC.
	log.Fatal(s.Serve(lis))
}
