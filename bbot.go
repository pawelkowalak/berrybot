package main

import (
	"fmt"
	"image"
	_ "image/png"
	"io"
	"math"
	"net"
	"time"

	log "github.com/Sirupsen/logrus"
	"golang.org/x/mobile/asset"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/exp/f32"
	"golang.org/x/mobile/exp/sprite"
	"golang.org/x/mobile/exp/sprite/clock"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "github.com/viru/berrybot/proto"
)

// App holds an app context.
type App struct {
	connected   bool
	conn        *grpc.ClientConn
	DriveStream pb.Driver_DriveClient
	ctrl        struct {
		x, midx float32
		y, midy float32
	}
	stick struct {
		x, midx float32
		y, midy float32
	}
}

// NewApp creates new app.
func NewApp() *App {
	a := App{}
	a.discoverBot()

	go func() {
		for {
			if !a.connected {
				time.Sleep(1)
			} else {
				t, err := a.DriveStream.Recv()
				if err == io.EOF {
					return
				}
				if err != nil {
					return
				}
				log.WithFields(log.Fields{
					"speed": t.Speed,
					"front": t.DistFront,
					"rear":  t.DistRear,
				}).Info("Telemetry")
			}
		}
	}()
	return &a
}

// discoverBot listens for UDP broadcasts on port 8032 and tries to connect to
// the first server it finds. This function blocks.
func (a *App) discoverBot() {
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
	a.conn, err = grpc.Dial(fmt.Sprintf("%s:%s", host, string(port)), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	cli := pb.NewDriverClient(a.conn)

	a.DriveStream, err = cli.Drive(context.Background())
	if err != nil {
		log.Fatalf("%v.Drive(_) = _, %v", cli, err)
	}

	// a.VideoStream, err = cli.GetImage(context.Background(), &pb.Image{Live: true})
	// if err != nil {
	// 	log.Fatalf("%v.GetImage(_) = _, %v", cli, err)
	// }

	a.connected = true
	log.Info("Connected")
}

// Size of controller and stick inside in points.
const (
	ctrlSize      = 80
	ctrlStickSize = 35
)

// Reset sets default positions, should be used after orientation change, etc.
func (a *App) Reset(sz size.Event) {
	if sz.PixelsPerPt == 0 || sz.HeightPt == 0 || sz.WidthPt == 0 {
		return
	}
	a.ctrl.x = float32(sz.WidthPt)/2 - ctrlSize/2
	a.ctrl.y = float32(sz.HeightPt)/2 - ctrlSize/2
	a.ctrl.midx = a.ctrl.x + ctrlSize/2
	a.ctrl.midy = a.ctrl.y + ctrlSize/2
	a.stick.x = float32(sz.WidthPt)/2 - ctrlStickSize/2
	a.stick.y = float32(sz.HeightPt)/2 - ctrlStickSize/2
	a.calcStickMids()
}

func (a *App) calcStickMids() {
	a.stick.midx = a.stick.x + ctrlStickSize/2
	a.stick.midy = a.stick.y + ctrlStickSize/2
}

// SetStick sets new position of controller stick.
func (a *App) SetStick(sz size.Event, x, y float32) {
	if sz.PixelsPerPt == 0 {
		return
	}
	xp := x / sz.PixelsPerPt
	yp := y / sz.PixelsPerPt
	xc := a.ctrl.x + ctrlSize/2
	yc := a.ctrl.y + ctrlSize/2
	d2 := math.Pow(float64(xp-xc), 2) + math.Pow(float64(yp-yc), 2)
	r := float32(21)
	if d2 < float64(r*r) {
		a.stick.x = xp - ctrlStickSize/2
		a.stick.y = yp - ctrlStickSize/2
	} else {
		a.stick.x = xc + r*((xp-xc)/float32(math.Sqrt(d2))) - ctrlStickSize/2
		a.stick.y = yc + r*((yp-yc)/float32(math.Sqrt(d2))) - ctrlStickSize/2
	}
	a.calcStickMids()
	log.Infof("new stick position: %v, %v", a.ctrl.x, a.ctrl.y)
	a.SendDrive()
}

// ResetStick sets stick to rest position.
func (a *App) ResetStick(sz size.Event) {
	if sz.PixelsPerPt == 0 || sz.HeightPt == 0 || sz.WidthPt == 0 {
		return
	}
	a.stick.x = float32(sz.WidthPt)/2 - ctrlStickSize/2
	a.stick.y = float32(sz.HeightPt)/2 - ctrlStickSize/2
	a.calcStickMids()
	log.Infof("new stick rest position: %v, %v", a.stick.x, a.stick.y)
	a.SendDrive()
}

// SendDrive sends drive message over gRPC.
func (a *App) SendDrive() {
	if a.connected {
		d := new(pb.Direction)
		d.Dx = int32(a.stick.midx - a.ctrl.midx)
		d.Dy = int32(a.ctrl.midy - a.stick.midy)
		if err := a.DriveStream.Send(d); err != nil {
			log.Fatalf("%v.Send(%v) = %v", a.DriveStream, d, err)
		}
	}
}

// Scene creates and returns a new app scene.
func (a *App) Scene(eng sprite.Engine) *sprite.Node {
	texs := loadTextures(eng)
	scene := &sprite.Node{}
	eng.Register(scene)
	eng.SetTransform(scene, f32.Affine{
		{1, 0, 0},
		{0, 1, 0},
	})

	newNode := func(fn arrangerFunc) {
		n := &sprite.Node{Arranger: arrangerFunc(fn)}
		eng.Register(n)
		scene.AppendChild(n)
	}

	// Controller boundaries.
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texCtrl])
		eng.SetTransform(n, f32.Affine{
			{ctrlSize, 0, a.ctrl.x},
			{0, ctrlSize, a.ctrl.y},
		})
	})

	// Controller stick.
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texStick])
		eng.SetTransform(n, f32.Affine{
			{ctrlStickSize, 0, a.stick.x},
			{0, ctrlStickSize, a.stick.y},
		})
	})

	return scene
}

type arrangerFunc func(e sprite.Engine, n *sprite.Node, t clock.Time)

func (a arrangerFunc) Arrange(e sprite.Engine, n *sprite.Node, t clock.Time) { a(e, n, t) }

const (
	texCtrl = iota
	texStick
)

func loadTextures(eng sprite.Engine) []sprite.SubTex {
	a, err := asset.Open("ctrl.png")
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()

	m, _, err := image.Decode(a)
	if err != nil {
		log.Fatal(err)
	}
	t, err := eng.LoadTexture(m)
	if err != nil {
		log.Fatal(err)
	}

	const n = 128
	// The +1's and -1's in the rectangles below are to prevent colors from
	// adjacent textures leaking into a given texture.
	// See: http://stackoverflow.com/questions/19611745/opengl-black-lines-in-between-tiles
	return []sprite.SubTex{
		texCtrl:  sprite.SubTex{T: t, R: image.Rect(0, 0, 320, 320)},
		texStick: sprite.SubTex{T: t, R: image.Rect(321, 0, 439, 120)},
	}
}
