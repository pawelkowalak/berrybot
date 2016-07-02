package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/viru/berrybot/proto"

	log "github.com/Sirupsen/logrus"
	"github.com/kidoman/embd"
	_ "github.com/kidoman/embd/host/rpi" // This loads the RPi driver
	"google.golang.org/grpc"
)

// server is used to implement hellowrld.GreeterServer.
type server struct {
	front, rear *echo
	driver      driver
	shutdown    bool
}

type echo struct {
	name       string
	echo       embd.DigitalPin
	trig       embd.DigitalPin
	quit, done chan bool
	dist       int64
	last       time.Time
}

func newEcho(name string, trigPin, echoPin int) (*echo, error) {
	var e echo
	e.name = name
	e.quit = make(chan bool)
	e.done = make(chan bool)
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
	if err := e.trig.Write(embd.Low); err != nil {
		log.Warnf("can't set trigger to low: %v", err)
	}
	time.Sleep(time.Second * 1)
	tick := time.NewTicker(time.Millisecond * 500)
	defer tick.Stop()
	for {
		select {
		case <-e.quit:
			e.done <- true
			return
		case <-tick.C:
			log.Infof("%s: measuring...", e.name)
			if err := e.trig.Write(embd.High); err != nil {
				log.Warnf("can't set trigger to high: %v", err)
			}
			time.Sleep(time.Microsecond * 10)
			if err := e.trig.Write(embd.Low); err != nil {
				log.Warnf("can't set trigger to low: %v", err)
			}
			dur, err := e.echo.TimePulse(embd.High)
			if err != nil {
				log.Warnf("can't time pulse: %v", err)
			}
			log.Infof("%s: distance: %dcm", e.name, dur.Nanoseconds()/1000*34/1000/2)
			e.dist = dur.Nanoseconds() / 1000 * 34 / 1000 / 2
		}
	}
}

func (e *echo) close() {
	e.quit <- true
	<-e.done
	e.echo.Close()
	e.trig.Close()
}

type driver struct {
	left, right *engine
	last        time.Time
}

func (d *driver) safetyStop() {
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		if d.last.Add(time.Second).Before(time.Now()) {
			log.Warn("Stopping driver!")
			d.stop()
		}
	}
}

func (d *driver) stop() {
	d.left.pwr.Write(embd.Low)
	d.right.pwr.Write(embd.Low)
	log.Info("driver STOP")
}

func (d *driver) forward() {
	d.left.pwr.Write(embd.High)
	d.left.fwd.Write(embd.High)
	d.right.pwr.Write(embd.High)
	d.right.fwd.Write(embd.High)
	d.last = time.Now()
	log.Info("driver FWD")
}

func (d *driver) backward() {
	d.left.pwr.Write(embd.High)
	d.left.fwd.Write(embd.Low)
	d.right.pwr.Write(embd.High)
	d.right.fwd.Write(embd.Low)
	d.last = time.Now()
	// log.Info("driver BACK")
}

const (
	safeStraightDist = 20
	safeTurningDist  = 10
)

func (s *server) drive(dir *pb.Direction) {
	switch {
	case dir.Dy > -5 && dir.Dy < 5 && dir.Dx > -5 && dir.Dx < 5:
		// Full stop.
		s.driver.stop()
	case dir.Dy > 5 && dir.Dx > -5 && dir.Dx < 5:
		// Forward.
		s.front.measure()
		if s.front.dist < safeStraightDist {
			s.driver.stop()
			return
		}
		s.driver.forward()
	case dir.Dy < -5 && dir.Dx > -5 && dir.Dx < 5:
		// Backward.
		s.rear.measure()
		if s.rear.dist < safeStraightDist {
			s.driver.stop()
			return
		}
		s.driver.backward()

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
	case dir.Dx > 5 && dir.Dy > 5:
		// Forward + right.
		s.driver.left.pwr.Write(embd.High)
		s.driver.left.fwd.Write(embd.High)
		s.driver.right.pwr.Write(embd.Low)
		s.driver.right.fwd.Write(embd.High)
		log.Info("driver FWD RIGHT")
	case dir.Dx < -5 && dir.Dy > 5:
		// Forward + left.
		s.driver.left.pwr.Write(embd.Low)
		s.driver.left.fwd.Write(embd.High)
		s.driver.right.pwr.Write(embd.High)
		s.driver.right.fwd.Write(embd.High)
		log.Info("driver FWD LEFT")
	case dir.Dx > 5 && dir.Dy < -5:
		// Backward + right.
		s.driver.left.pwr.Write(embd.High)
		s.driver.left.fwd.Write(embd.Low)
		s.driver.right.pwr.Write(embd.Low)
		s.driver.right.fwd.Write(embd.Low)
		log.Info("driver BACK RIGHT")
	case dir.Dx < -5 && dir.Dy < -5:
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
			// log.WithFields(log.Fields{
			// 	"dx": d.Dx,
			// 	"dy": d.Dy,
			// }).Info("Direction")
			s.drive(d)
		}
	}()

	for {
		select {
		case <-time.After(time.Minute):
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

	go http.ListenAndServe(":9191", nil)

	// Initialize GPIO.
	var err error
	if err = embd.InitGPIO(); err != nil {
		log.Fatalf("Can't init GPIO: %v", err)
	}
	defer embd.CloseGPIO()
	front, err := newEcho("front", 9, 10)
	if err != nil {
		log.Fatalf("Can't init front echo: %v", err)
	}
	defer front.close()
	rear, err := newEcho("rear", 19, 20)
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

	drv := driver{left: left, right: right}
	go drv.safetyStop()

	srv := server{front: front, rear: rear, driver: drv}
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
		srv.shutdown = true
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
