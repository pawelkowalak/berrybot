package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	pb "github.com/viru/berrybot/proto"

	log "github.com/Sirupsen/logrus"
	"github.com/kidoman/embd"
	_ "github.com/kidoman/embd/host/rpi" // RaspberryPI driver
	"google.golang.org/grpc"
)

// Server is used to implement steering.DriverServer.
type server struct {
	front, rear *echo
	driver      *driver
}

// Proximity sensor.
type echo struct {
	name    string
	echo    embd.DigitalPin
	trig    embd.DigitalPin
	waitc   chan struct{}
	dist    int64
	last    time.Time
	enabled bool
	send    chan bool
}

func newEcho(name string, trigPin, echoPin int) (*echo, error) {
	var e echo
	e.name = name
	e.waitc = make(chan struct{})
	e.send = make(chan bool)
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

// Try to TimePulse proximity sensor to calculate distance.
func (e *echo) measure() error {
	if err := e.trig.Write(embd.High); err != nil {
		return fmt.Errorf("can't set trigger to high: %v", err)
	}
	time.Sleep(time.Microsecond * 10)
	if err := e.trig.Write(embd.Low); err != nil {
		return fmt.Errorf("can't set trigger to low: %v", err)
	}
	dur, err := e.echo.TimePulse(embd.High)
	if err != nil {
		return fmt.Errorf("can't time pulse: %v", err)
	}
	log.Infof("%s: distance: %dcm", e.name, dur.Nanoseconds()/1000*34/1000/2)
	e.dist = dur.Nanoseconds() / 1000 * 34 / 1000 / 2
	e.send <- true
	return nil
}

const (
	defaultFastDur = time.Millisecond * 250
	defaultSlowDur = time.Second
)

// Goroutine measuring distance in an infinite loop. If distancer is enabled (bot is driving)
// then measuring on fast timer, otherwise only once per second to save CPU cycles.
func (e *echo) runDistancer() {
	if err := e.trig.Write(embd.Low); err != nil {
		log.Warnf("can't set trigger to low: %v", err)
	}
	time.Sleep(time.Second * 1) // Settle time needed after initial activation.
	fast := time.NewTicker(defaultFastDur)
	defer fast.Stop()
	slow := time.NewTicker(defaultSlowDur)
	defer slow.Stop()
	for {
		select {
		case <-e.waitc:
			return
		case <-slow.C:
			if err := e.measure(); err != nil {
				log.Warn(err)
			}
		case <-fast.C:
			if e.enabled {
				if err := e.measure(); err != nil {
					log.Warn(err)
				}
			}
		}
	}
}

func (e *echo) close() {
	close(e.waitc)
	e.echo.Close()
	e.trig.Close()
}

type driver struct {
	left, right *engine
	mu          sync.Mutex
	moving      bool
	last        time.Time
}

func (d *driver) safetyStop() {
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		d.mu.Lock()
		if d.moving && d.last.Add(time.Second).Before(time.Now()) {
			d.mu.Unlock()
			d.stop()
			log.Warn("Emergency stop!")
			continue
		}
		d.mu.Unlock()
	}
}

func (d *driver) setMoving(moving bool) {
	d.mu.Lock()
	d.last = time.Now()
	d.moving = moving
	d.mu.Unlock()
}

func (d *driver) stop() {
	d.left.pwr = 0
	d.right.pwr = 0
	d.setMoving(false)
}

func (d *driver) forward(pwr int32) {
	d.left.pwr = pwr
	d.left.fwdPin.Write(embd.High)
	d.right.pwr = pwr
	d.right.fwdPin.Write(embd.High)
	d.setMoving(true)
}

func (d *driver) backward(pwr int32) {
	d.left.pwr = pwr
	d.left.fwdPin.Write(embd.Low)
	d.right.pwr = pwr
	d.right.fwdPin.Write(embd.Low)
	d.setMoving(true)
}

func (d *driver) sharpRight(pwr int32) {
	d.left.pwr = pwr
	d.left.fwdPin.Write(embd.High)
	d.right.pwr = pwr
	d.right.fwdPin.Write(embd.Low)
	d.setMoving(true)
}

func (d *driver) sharpLeft(pwr int32) {
	d.left.pwr = pwr
	d.left.fwdPin.Write(embd.Low)
	d.right.pwr = pwr
	d.right.fwdPin.Write(embd.High)
	d.setMoving(true)
}

func (d *driver) fwdRight() {
	d.left.pwr = 100
	d.left.fwdPin.Write(embd.High)
	d.right.pwr = 50
	d.right.fwdPin.Write(embd.High)
	d.setMoving(true)
}

func (d *driver) fwdLeft() {
	d.left.pwr = 50
	d.left.fwdPin.Write(embd.High)
	d.right.pwr = 100
	d.right.fwdPin.Write(embd.High)
	d.setMoving(true)
}

func (d *driver) backRight() {
	d.left.pwr = 100
	d.left.fwdPin.Write(embd.Low)
	d.right.pwr = 50
	d.right.fwdPin.Write(embd.Low)
	d.setMoving(true)
}

func (d *driver) backLeft() {
	d.left.pwr = 50
	d.left.fwdPin.Write(embd.Low)
	d.right.pwr = 100
	d.right.fwdPin.Write(embd.Low)
	d.setMoving(true)
}

func (s *server) drive(dir *pb.Direction) {
	switch {
	case dir.Dy > 15 && dir.Dx > -15 && dir.Dx < 15:
		s.front.enabled = true
		s.driver.forward(dir.Dy)
	case dir.Dy < -15 && dir.Dx > -15 && dir.Dx < 15:
		s.rear.enabled = true
		s.driver.backward(-dir.Dy)
	case dir.Dx > 15 && dir.Dy > -15 && dir.Dy < 15:
		s.driver.sharpRight(dir.Dx)
	case dir.Dx < -15 && dir.Dy > -15 && dir.Dy < 15:
		s.driver.sharpLeft(-dir.Dx)
	case dir.Dx > 15 && dir.Dy > 15:
		s.driver.fwdRight()
	case dir.Dx < -15 && dir.Dy > 15:
		s.driver.fwdLeft()
	case dir.Dx > 15 && dir.Dy < -15:
		s.driver.backRight()
	case dir.Dx < -15 && dir.Dy < -15:
		s.driver.backLeft()
	default:
		s.front.enabled = false
		s.rear.enabled = false
		s.driver.stop()
	}
}

type engine struct {
	fwdPin, pwrPin embd.DigitalPin
	pwr            int32
}

func newEngine(pwrPin, fwdPin int) (*engine, error) {
	var e engine
	var err error
	e.pwrPin, err = embd.NewDigitalPin(pwrPin)
	if err != nil {
		return nil, fmt.Errorf("can't init power pin: %v", err)
	}
	e.fwdPin, err = embd.NewDigitalPin(fwdPin)
	if err != nil {
		return nil, fmt.Errorf("can't init forward pin: %v", err)
	}

	// Set direction.
	if err := e.pwrPin.SetDirection(embd.Out); err != nil {
		return nil, fmt.Errorf("can't set power direction: %v", err)
	}
	if err := e.fwdPin.SetDirection(embd.Out); err != nil {
		return nil, fmt.Errorf("can't set forward direction: %v", err)
	}
	go e.startPWM()
	return &e, nil
}

func (e *engine) close() {
	e.pwrPin.Close()
	e.fwdPin.Close()
}

func (e *engine) startPWM() {
	ticker := time.NewTicker(time.Millisecond * 25)
	flap := embd.Low
	for range ticker.C {
		switch {
		case e.pwr < 15:
			e.pwrPin.Write(embd.Low)
		case e.pwr < 50:
			e.pwrPin.Write(flap)
			if flap == embd.Low {
				flap = embd.High
			} else {
				flap = embd.Low
			}
		default:
			e.pwrPin.Write(embd.High)
		}
	}
}

const (
	sensorUnknown = iota
	sensorFront
	sensorRear
)

func (s *server) sendTelemetry(stream pb.Driver_DriveServer) error {
	var speed int32
	if s.driver.moving {
		speed = 100
	}
	log.Info("Sending telemetry!")
	return stream.Send(&pb.Telemetry{Speed: speed, DistFront: int32(s.front.dist), DistRear: int32(s.rear.dist)})

}

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
			s.drive(d)
		}
	}()

	for {
		select {
		case <-s.front.send:
			if err := s.sendTelemetry(stream); err != nil {
				log.Errorf("can't send telemetry: %v", err)
				return err
			}
		case <-s.rear.send:
			if err := s.sendTelemetry(stream); err != nil {
				log.Errorf("can't send telemetry: %v", err)
				return err
			}
		case <-waitc:
			log.Info("got ERR from client, closing sending loop")
			return nil
		}
	}
}

var (
	grpcPort  = flag.String("grpc-port", "31337", "gRPC listen port")
	bcastPort = flag.String("bcast-port", "8032", "UDP broadcast port used by clients for discovery")
)

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

	// Listen for gRPC connections.
	lis, err := net.Listen("tcp", ":"+*grpcPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	drv := driver{left: left, right: right}
	go drv.safetyStop()

	srv := server{front: front, rear: rear, driver: &drv}
	s := grpc.NewServer()
	pb.RegisterDriverServer(s, &srv)

	// Open broadcast connection.
	bcast, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Fatal(err)
	}
	defer bcast.Close()

	broadcastAddr := "255.255.255.255:" + *bcastPort
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
