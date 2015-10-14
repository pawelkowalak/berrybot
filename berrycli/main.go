package main

import (
	"fmt"
	"image"
	_ "image/png"
	"math"
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
	touchPtX  float32
	touchPtY  float32

	// GRPC variables for driving.
	conn   *grpc.ClientConn
	cli    pb.DriverClient
	stream pb.Driver_DriveClient
)

// object is a generic 2D sprite with texture, position and size (in points).
type object struct {
	subTex        sprite.SubTex
	width, height float32
	posx, posy    float32
	midx, midy    float32
}

// controller is an UI element that let's you drive Berry Bot! Duh.
type controller struct {
	object
	stick object
}

// UpdateMiddle updates middle point of the stick based on current position and width.
func (c *controller) UpdateMiddle() {
	c.stick.midx = c.stick.posx + c.stick.width/2
	c.stick.midy = c.stick.posy + c.stick.height/2
}

// UpdatePosition tries to move stick inside controller based on current touch event's (x, y).
func (c *controller) UpdatePosition() {
	inside := ctrl.IsInside(ctrl.stick.midx, ctrl.stick.midy)
	if touchPtX-1 > ctrl.stick.midx && (inside || ctrl.stick.midx <= ctrl.midx) {
		ctrl.stick.posx++
	} else if touchPtX+1 < ctrl.stick.midx && (inside || ctrl.stick.midx >= ctrl.midx) {
		ctrl.stick.posx--
	}
	if touchPtY-1 > ctrl.stick.midy && (inside || ctrl.stick.midy <= ctrl.midy) {
		ctrl.stick.posy++
	} else if touchPtY+1 < ctrl.stick.midy && (inside || ctrl.stick.midy >= ctrl.midy) {
		ctrl.stick.posy--
	}
}

// IsInside checks if given (x, y) point is inside controller circle.
func (c *controller) IsInside(x, y float32) bool {
	r2 := math.Pow(float64(c.width/2-c.stick.width/2), 2)
	d2 := math.Pow(float64(x-c.midx), 2) + math.Pow(float64(y-c.midy), 2)
	return d2 < r2
}

// Size of controller and stick inside in points.
const (
	ctrlSize      = 80
	ctrlStickSize = 35
)

// newController creates new controller and sets its sizes.
func newController() *controller {
	c := new(controller)
	c.width = ctrlSize
	c.height = ctrlSize
	c.stick.width = ctrlStickSize
	c.stick.height = ctrlStickSize
	return c
}

// There can be only one controller.
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
				ctrl.stick.posx = ctrl.midx - ctrl.stick.width/2
				ctrl.stick.posy = ctrl.midy - ctrl.stick.height/2
				touchPtX = ctrl.midx
				touchPtY = ctrl.midy
				log.WithFields(log.Fields{
					"posx": ctrl.posx,
					"posy": ctrl.posy,
				}).Info("controller position")
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
				if sz.PixelsPerPt == 0 {
					break
				}
				ptx := e.X / sz.PixelsPerPt
				pty := e.Y / sz.PixelsPerPt
				d := new(pb.Direction)
				switch e.Type {
				case touch.TypeEnd:
					log.Info("Resetting back to middle")
					touchPtX = ctrl.midx
					touchPtY = ctrl.midy
					d.Dx = 0
					d.Dy = 0
				default:
					touchPtX = ptx
					touchPtY = pty
					d.Dx = int32(ctrl.stick.midx - ctrl.midx)
					d.Dy = int32(ctrl.midy - ctrl.stick.midy)
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
	ctrlNode := newNode()
	ctrlNode.Arranger = arrangerFunc(func(eng sprite.Engine, ctrlNode *sprite.Node, t clock.Time) {
		eng.SetSubTex(ctrlNode, ctrl.subTex)
		eng.SetTransform(ctrlNode, f32.Affine{
			{ctrl.width, 0, ctrl.posx},
			{0, ctrl.height, ctrl.posy},
		})
	})

	// Stick position
	stickNode := newNode()
	stickNode.Arranger = arrangerFunc(func(eng sprite.Engine, stickNode *sprite.Node, t clock.Time) {
		eng.SetSubTex(stickNode, ctrl.stick.subTex)
		ctrl.UpdateMiddle()
		ctrl.UpdatePosition()
		eng.SetTransform(stickNode, f32.Affine{
			{ctrl.stick.width, 0, ctrl.stick.posx},
			{0, ctrl.stick.height, ctrl.stick.posy},
		})
	})

}

func loadTexture(name string) sprite.Texture {
	a, err := asset.Open(name)
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
	return t
}

func loadController() {
	ctrl.subTex = sprite.SubTex{
		T: loadTexture("circle.png"),
		R: image.Rect(0, 0, 320, 320),
	}
	ctrl.stick.subTex = sprite.SubTex{
		T: loadTexture("stick.png"),
		R: image.Rect(0, 0, 120, 120),
	}
}

type arrangerFunc func(e sprite.Engine, n *sprite.Node, t clock.Time)

func (a arrangerFunc) Arrange(e sprite.Engine, n *sprite.Node, t clock.Time) { a(e, n, t) }

func onPaint(glctx gl.Context, sz size.Event) {
	// Fill background with white.
	glctx.ClearColor(0, 0, 0, 1)
	glctx.Clear(gl.COLOR_BUFFER_BIT)
	now := clock.Time(time.Since(startTime) * 60 / time.Second)
	eng.Render(scene, now, sz)
	fps.Draw(sz)
}
