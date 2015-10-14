package main

import (
	"fmt"
	"image"
	_ "image/png"
	"net"
	"time"

	"golang.org/x/mobile/app"
	"golang.org/x/mobile/asset"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/exp/app/debug"
	"golang.org/x/mobile/exp/f32"
	"golang.org/x/mobile/exp/gl/glutil"
	"golang.org/x/mobile/exp/sprite"
	"golang.org/x/mobile/exp/sprite/clock"
	"golang.org/x/mobile/exp/sprite/glsprite"
	"golang.org/x/mobile/gl"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	log "github.com/Sirupsen/logrus"
	pb "github.com/viru/berrybot/proto"
)

var (
	// UI variables for scene drawing.
	startTime = time.Now()
	images    *glutil.Images
	eng       sprite.Engine
	scene     *sprite.Node
	fps       *debug.FPS

	// GRPC variables for driving.
	conn   *grpc.ClientConn
	cli    pb.DriverClient
	stream pb.Driver_DriveClient
)

type controller struct {
	subTex        sprite.SubTex
	width, height float32
	posx, posy    float32
	midx, midy    float32
}

func newController() *controller {
	c := new(controller)
	c.width = 80
	c.height = 80
	return c
}

var ctrl = newController()

func main() {
	app.Main(func(a app.App) {
		var glctx gl.Context
		var sz size.Event
		for e := range a.Events() {
			switch e := a.Filter(e).(type) {
			case lifecycle.Event:
				switch e.Crosses(lifecycle.StageVisible) {
				case lifecycle.CrossOn:
					glctx, _ = e.DrawContext.(gl.Context)
					onStart(glctx)
					a.Send(paint.Event{})
				case lifecycle.CrossOff:
					onStop(glctx)
					glctx = nil
				}
			case size.Event:
				sz = e
				switch sz.Orientation {
				case size.OrientationPortrait:
					ctrl.posx = (float32(sz.WidthPt) - ctrl.width) / 2
					ctrl.posy = (float32(sz.HeightPt) - ctrl.height*1.5)
				case size.OrientationLandscape:
					ctrl.posx = (float32(sz.WidthPt) - ctrl.width*1.5)
					ctrl.posy = (float32(sz.HeightPt) - ctrl.height) / 2
				default:
					ctrl.posx = (float32(sz.WidthPt) - ctrl.width) / 2
					ctrl.posy = (float32(sz.HeightPt) - ctrl.height*1.5)
				}
				ctrl.midx = ctrl.posx + ctrl.width/2
				ctrl.midy = ctrl.posy + ctrl.height/2
			case paint.Event:
				if glctx == nil || e.External {
					// As we are actively painting as fast as
					// we can (usually 60 FPS), skip any paint
					// events sent by the system.
					continue
				}

				onPaint(glctx, sz)
				a.Publish()
				// Drive the animation by preparing to paint the next frame
				// after this one is shown.
				a.Send(paint.Event{})
			case touch.Event:
				d := new(pb.Direction)
				// Normalize touch input based on controller position
				if sz.PixelsPerPt > 0 && ctrl.midx > 0 && ctrl.midy > 0 {
					d.Dx = int32((e.X/sz.PixelsPerPt - ctrl.midx) * 100 / ctrl.midx)
					d.Dy = int32((ctrl.midy - e.Y/sz.PixelsPerPt) * 100 / ctrl.midy)
				}
				log.WithFields(log.Fields{
					"dx": d.Dx,
					"dy": d.Dy,
				}).Info("Normalized inputs")
				if err := stream.Send(d); err != nil {
					log.Fatalf("%v.Send(%v) = %v", stream, d, err)
				}
			}
		}
	})
}

func onStart(glctx gl.Context) {
	// Enable blending for alpha channel in PNG files.
	glctx.Enable(gl.BLEND)
	glctx.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	images = glutil.NewImages(glctx)
	eng = glsprite.Engine(images)
	loadScene()
	fps = debug.NewFPS(images)

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
	log.Printf("Received port broadcast from %s", peer)
	host, _, err := net.SplitHostPort(peer.String())
	if err != nil {
		log.Fatalf("can't parse peer IP address %v", err)
	}

	// Connect to first discovered bot via GRPC.
	conn, err = grpc.Dial(fmt.Sprintf("%s:%s", host, string(port)), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	cli = pb.NewDriverClient(conn)
	stream, err = cli.Drive(context.Background())
	if err != nil {
		log.Fatalf("%v.Drive(_) = _, %v", cli, err)
	}
}

func onStop(glctx gl.Context) {
	eng.Release()
	fps.Release()
	images.Release()
	conn.Close()
}

func newNode() *sprite.Node {
	n := &sprite.Node{}
	eng.Register(n)
	scene.AppendChild(n)
	return n
}

func loadScene() {
	loadController()
	scene = &sprite.Node{}
	eng.Register(scene)
	eng.SetTransform(scene, f32.Affine{
		{1, 0, 0},
		{0, 1, 0},
	})

	// Controller boundaries.
	circle := newNode()
	eng.SetSubTex(circle, ctrl.subTex)
	eng.SetTransform(circle, f32.Affine{
		{ctrl.width, 0, ctrl.posx},
		{0, ctrl.height, ctrl.posy},
	})
}

func loadController() {
	a, err := asset.Open("circle.png")
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()

	img, _, err := image.Decode(a)
	if err != nil {
		log.Fatal(err)
	}
	t, err := eng.LoadTexture(img)
	if err != nil {
		log.Fatal(err)
	}
	ctrl.subTex = sprite.SubTex{T: t, R: image.Rect(0, 0, 300, 300)}
}

type arrangerFunc func(e sprite.Engine, n *sprite.Node, t clock.Time)

func (a arrangerFunc) Arrange(e sprite.Engine, n *sprite.Node, t clock.Time) { a(e, n, t) }

func onPaint(glctx gl.Context, sz size.Event) {
	// Fill background with white.
	glctx.ClearColor(1, 1, 1, 1)
	glctx.Clear(gl.COLOR_BUFFER_BIT)
	now := clock.Time(time.Since(startTime) * 60 / time.Second)
	eng.Render(scene, now, sz)
	fps.Draw(sz)
}
