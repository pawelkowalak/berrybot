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

	pb "github.com/pawelkowalak/berrybot/proto"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

// Server is used to implement steering.DriverServer.
type server struct {
	pb.UnimplementedDriverServer
	front, rear *echo
	driver      *driver
}

// Proximity sensor.
type echo struct {
	name    string
	echo    gpio.PinIO
	trig    gpio.PinIO
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

	trig := gpioreg.ByName(fmt.Sprintf("GPIO%d", trigPin))
	if trig == nil {
		return nil, fmt.Errorf("can't find trigger pin GPIO%d", trigPin)
	}
	echo := gpioreg.ByName(fmt.Sprintf("GPIO%d", echoPin))
	if echo == nil {
		return nil, fmt.Errorf("can't find echo pin GPIO%d", echoPin)
	}
	e.trig = trig
	e.echo = echo

	if err := e.trig.Out(gpio.Low); err != nil {
		return nil, fmt.Errorf("can't set trigger pin low: %v", err)
	}
	if err := e.echo.In(gpio.PullDown, gpio.NoEdge); err != nil {
		return nil, fmt.Errorf("can't configure echo pin: %v", err)
	}

	return &e, nil
}

// Try to measure proximity sensor pulse to calculate distance.
func (e *echo) measure() error {
	// Trigger a 10µs pulse.
	if err := e.trig.Out(gpio.High); err != nil {
		return fmt.Errorf("can't set trigger to high: %v", err)
	}
	time.Sleep(10 * time.Microsecond)
	if err := e.trig.Out(gpio.Low); err != nil {
		return fmt.Errorf("can't set trigger to low: %v", err)
	}

	// Wait for the echo pin to go high, then low, measuring the high duration.
	if err := e.echo.In(gpio.PullDown, gpio.RisingEdge); err != nil {
		return fmt.Errorf("can't configure echo pin for rising edge: %v", err)
	}
	if !e.echo.WaitForEdge(50 * time.Millisecond) {
		return fmt.Errorf("timeout waiting for echo rising edge")
	}
	start := time.Now()

	if err := e.echo.In(gpio.PullDown, gpio.FallingEdge); err != nil {
		return fmt.Errorf("can't configure echo pin for falling edge: %v", err)
	}
	if !e.echo.WaitForEdge(50 * time.Millisecond) {
		return fmt.Errorf("timeout waiting for echo falling edge")
	}
	dur := time.Since(start)

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
	if err := e.trig.Out(gpio.Low); err != nil {
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
	_ = d.left.fwdPin.Out(gpio.High)
	d.right.pwr = pwr
	_ = d.right.fwdPin.Out(gpio.High)
	d.setMoving(true)
}

func (d *driver) backward(pwr int32) {
	d.left.pwr = pwr
	_ = d.left.fwdPin.Out(gpio.Low)
	d.right.pwr = pwr
	_ = d.right.fwdPin.Out(gpio.Low)
	d.setMoving(true)
}

func (d *driver) sharpRight(pwr int32) {
	d.left.pwr = pwr
	_ = d.left.fwdPin.Out(gpio.High)
	d.right.pwr = pwr
	_ = d.right.fwdPin.Out(gpio.Low)
	d.setMoving(true)
}

func (d *driver) sharpLeft(pwr int32) {
	d.left.pwr = pwr
	_ = d.left.fwdPin.Out(gpio.Low)
	d.right.pwr = pwr
	_ = d.right.fwdPin.Out(gpio.High)
	d.setMoving(true)
}

func (d *driver) fwdRight() {
	d.left.pwr = 100
	_ = d.left.fwdPin.Out(gpio.High)
	d.right.pwr = 0
	_ = d.right.fwdPin.Out(gpio.High)
	d.setMoving(true)
}

func (d *driver) fwdLeft() {
	d.left.pwr = 0
	_ = d.left.fwdPin.Out(gpio.High)
	d.right.pwr = 100
	_ = d.right.fwdPin.Out(gpio.High)
	d.setMoving(true)
}

func (d *driver) backRight() {
	d.left.pwr = 100
	_ = d.left.fwdPin.Out(gpio.Low)
	d.right.pwr = 0
	_ = d.right.fwdPin.Out(gpio.Low)
	d.setMoving(true)
}

func (d *driver) backLeft() {
	d.left.pwr = 0
	_ = d.left.fwdPin.Out(gpio.Low)
	d.right.pwr = 100
	_ = d.right.fwdPin.Out(gpio.Low)
	d.setMoving(true)
}

const driveDeadZone = 15

// driveCmd represents a single driving intent derived from joystick (Dx, Dy).
type driveCmd int

const (
	cmdStop driveCmd = iota
	cmdForward
	cmdBackward
	cmdSharpRight
	cmdSharpLeft
	cmdFwdRight
	cmdFwdLeft
	cmdBackRight
	cmdBackLeft
)

// driveRule maps a condition (dir) to a drive command. First match wins.
type driveRule struct {
	pred func(*pb.Direction) bool
	cmd  driveCmd
}

var driveTable = []driveRule{
	{func(d *pb.Direction) bool {
		return d.Dy > driveDeadZone && d.Dx > -driveDeadZone && d.Dx < driveDeadZone
	}, cmdForward},
	{func(d *pb.Direction) bool {
		return d.Dy < -driveDeadZone && d.Dx > -driveDeadZone && d.Dx < driveDeadZone
	}, cmdBackward},
	{func(d *pb.Direction) bool {
		return d.Dx > driveDeadZone && d.Dy > -driveDeadZone && d.Dy < driveDeadZone
	}, cmdSharpRight},
	{func(d *pb.Direction) bool {
		return d.Dx < -driveDeadZone && d.Dy > -driveDeadZone && d.Dy < driveDeadZone
	}, cmdSharpLeft},
	{func(d *pb.Direction) bool { return d.Dx > driveDeadZone && d.Dy > driveDeadZone }, cmdFwdRight},
	{func(d *pb.Direction) bool { return d.Dx < -driveDeadZone && d.Dy > driveDeadZone }, cmdFwdLeft},
	{func(d *pb.Direction) bool { return d.Dx > driveDeadZone && d.Dy < -driveDeadZone }, cmdBackRight},
	{func(d *pb.Direction) bool { return d.Dx < -driveDeadZone && d.Dy < -driveDeadZone }, cmdBackLeft},
}

func classifyDirection(dir *pb.Direction) driveCmd {
	for _, r := range driveTable {
		if r.pred(dir) {
			return r.cmd
		}
	}
	return cmdStop
}

func (s *server) drive(dir *pb.Direction) {
	switch classifyDirection(dir) {
	case cmdForward:
		s.front.enabled = true
		s.driver.forward(dir.Dy)
	case cmdBackward:
		s.rear.enabled = true
		s.driver.backward(-dir.Dy)
	case cmdSharpRight:
		s.driver.sharpRight(dir.Dx)
	case cmdSharpLeft:
		s.driver.sharpLeft(-dir.Dx)
	case cmdFwdRight:
		s.driver.fwdRight()
	case cmdFwdLeft:
		s.driver.fwdLeft()
	case cmdBackRight:
		s.driver.backRight()
	case cmdBackLeft:
		s.driver.backLeft()
	default:
		s.front.enabled = false
		s.rear.enabled = false
		s.driver.stop()
	}
}

type engine struct {
	fwdPin, pwrPin gpio.PinIO
	pwr            int32
}

func newEngine(pwrPin, fwdPin int) (*engine, error) {
	var e engine

	pwr := gpioreg.ByName(fmt.Sprintf("GPIO%d", pwrPin))
	if pwr == nil {
		return nil, fmt.Errorf("can't find power pin GPIO%d", pwrPin)
	}
	fwd := gpioreg.ByName(fmt.Sprintf("GPIO%d", fwdPin))
	if fwd == nil {
		return nil, fmt.Errorf("can't find forward pin GPIO%d", fwdPin)
	}

	e.pwrPin = pwr
	e.fwdPin = fwd

	if err := e.pwrPin.Out(gpio.Low); err != nil {
		return nil, fmt.Errorf("can't configure power pin: %v", err)
	}
	if err := e.fwdPin.Out(gpio.Low); err != nil {
		return nil, fmt.Errorf("can't configure forward pin: %v", err)
	}

	go e.startPWM()
	return &e, nil
}

func (e *engine) close() {
	// No-op for periph.io; pins are released on process exit.
}

func (e *engine) startPWM() {
	ticker := time.NewTicker(time.Millisecond * 25)
	flap := gpio.Low
	for range ticker.C {
		switch {
		case e.pwr < 15:
			_ = e.pwrPin.Out(gpio.Low)
		case e.pwr < 50:
			_ = e.pwrPin.Out(flap)
			if flap == gpio.Low {
				flap = gpio.High
			} else {
				flap = gpio.Low
			}
		default:
			_ = e.pwrPin.Out(gpio.High)
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

	// Initialize periph.io host drivers.
	var err error
	if _, err = host.Init(); err != nil {
		log.Fatalf("Can't init periph.io: %v", err)
	}
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
		lis.Close()
		bcast.Close()
		os.Exit(0)
	}()

	// Start serving GRPC.
	log.Fatal(s.Serve(lis))
}
