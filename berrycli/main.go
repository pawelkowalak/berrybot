package main

import (
	"image"
	_ "image/png"
	"math"
	"time"

	log "github.com/Sirupsen/logrus"
	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/exp/f32"
	"golang.org/x/mobile/exp/sprite"
	"golang.org/x/mobile/exp/sprite/clock"
	"golang.org/x/mobile/gl"

	"github.com/viru/berrybot/berrycli/remote"
	"github.com/viru/berrybot/berrycli/render"
	pb "github.com/viru/berrybot/proto"
)

var (
	touchPtX float32
	touchPtY float32
)

// obj2d is a generic 2D sprite with texture, position and size (in points).
type obj2d struct {
	subTex        sprite.SubTex
	width, height float32
	posx, posy    float32
	midx, midy    float32
}

// controller is an UI element that let's you drive Berry Bot! Duh.
type controller struct {
	obj2d
	stick obj2d
}

// UpdateMiddle updates middle point of the stick based on current position and width.
func (c *controller) updateMiddle() {
	c.stick.midx = c.stick.posx + c.stick.width/2
	c.stick.midy = c.stick.posy + c.stick.height/2
}

// UpdatePosition tries to move stick inside controller based on current touch event's (x, y).
func (c *controller) updatePosition() {
	inside := c.isInside(c.stick.midx, c.stick.midy)
	if touchPtX-1 > c.stick.midx && (inside || c.stick.midx <= c.midx) {
		c.stick.posx++
	} else if touchPtX+1 < c.stick.midx && (inside || c.stick.midx >= c.midx) {
		c.stick.posx--
	}
	if touchPtY-1 > c.stick.midy && (inside || c.stick.midy <= c.midy) {
		c.stick.posy++
	} else if touchPtY+1 < c.stick.midy && (inside || c.stick.midy >= c.midy) {
		c.stick.posy--
	}
}

// IsInside checks if given (x, y) point is inside controller circle.
func (c *controller) isInside(x, y float32) bool {
	r2 := math.Pow(float64(c.width/2-c.stick.width/2), 2)
	d2 := math.Pow(float64(x-c.midx), 2) + math.Pow(float64(y-c.midy), 2)
	return d2 < r2
}

func (c *controller) loadTextures(rs *render.Service) {
	circle, err := rs.OpenTexture("circle.png")
	if err != nil {
		log.Fatalf("Can't load textures: %v", err)
	}
	c.subTex = sprite.SubTex{
		T: circle,
		R: image.Rect(0, 0, 320, 320),
	}
	stick, err := rs.OpenTexture("stick.png")
	if err != nil {
		log.Fatalf("Can't load textures: %v", err)
	}
	c.stick.subTex = sprite.SubTex{
		T: stick,
		R: image.Rect(0, 0, 120, 120),
	}
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

// client combines render, remote and controller services.
type client struct {
	render *render.Service
	remote *remote.Service
	ctrl   *controller
}

// newClient creates new client and its sub-services.
func newClient() *client {
	c := new(client)
	c.render = render.NewService(time.Now())
	c.remote = remote.NewService()
	c.ctrl = newController()
	go c.remote.Connect()
	return c
}

func (c *client) main(a app.App) {
	var sz size.Event
	for e := range a.Events() {
		switch e := a.Filter(e).(type) {
		case lifecycle.Event:
			switch e.Crosses(lifecycle.StageVisible) {
			case lifecycle.CrossOn:
				glctx, _ := e.DrawContext.(gl.Context)
				c.render.Init(glctx)
				c.loadScene()
				a.Send(paint.Event{})
			case lifecycle.CrossOff:
				c.render.Teardown()
				c.remote.Close()
			}
		case size.Event:
			sz = e
			switch sz.Orientation {
			case size.OrientationPortrait:
				c.ctrl.posx = (float32(sz.WidthPt) - c.ctrl.width) / 2
				c.ctrl.posy = (float32(sz.HeightPt) - c.ctrl.height*1.5)
			case size.OrientationLandscape:
				c.ctrl.posx = (float32(sz.WidthPt) - c.ctrl.width*1.5)
				c.ctrl.posy = (float32(sz.HeightPt) - c.ctrl.height) / 2
			default:
				c.ctrl.posx = (float32(sz.WidthPt) - c.ctrl.width) / 2
				c.ctrl.posy = (float32(sz.HeightPt) - c.ctrl.height*1.5)
			}
			c.ctrl.midx = c.ctrl.posx + c.ctrl.width/2
			c.ctrl.midy = c.ctrl.posy + c.ctrl.height/2
			c.ctrl.stick.posx = c.ctrl.midx - c.ctrl.stick.width/2
			c.ctrl.stick.posy = c.ctrl.midy - c.ctrl.stick.height/2
			touchPtX = c.ctrl.midx
			touchPtY = c.ctrl.midy
			log.WithFields(log.Fields{
				"posx": c.ctrl.posx,
				"posy": c.ctrl.posy,
			}).Info("controller position")
		case paint.Event:
			c.render.Render(sz)
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
				touchPtX = c.ctrl.midx
				touchPtY = c.ctrl.midy
				d.Dx = 0
				d.Dy = 0
			default:
				touchPtX = ptx
				touchPtY = pty
				d.Dx = int32(c.ctrl.stick.midx - c.ctrl.midx)
				d.Dy = int32(c.ctrl.midy - c.ctrl.stick.midy)
			}

			log.WithFields(log.Fields{
				"dx": d.Dx,
				"dy": d.Dy,
			}).Info("Normalized inputs")
			if c.remote.Connected {
				if err := c.remote.Stream.Send(d); err != nil {
					log.Fatalf("%v.Send(%v) = %v", c.remote.Stream, d, err)
				}
			}
		}
	}
}

func main() {
	c := newClient()
	app.Main(c.main)
}

func (c *client) loadScene() {
	c.ctrl.loadTextures(c.render)

	// Controller boundaries.
	ctrlNode, err := c.render.NewNode()
	if err != nil {
		log.Fatalf("Can't load scene: %v", err)
	}
	ctrlNode.Arranger = arrangerFunc(func(eng sprite.Engine, ctrlNode *sprite.Node, t clock.Time) {
		eng.SetSubTex(ctrlNode, c.ctrl.subTex)
		eng.SetTransform(ctrlNode, f32.Affine{
			{c.ctrl.width, 0, c.ctrl.posx},
			{0, c.ctrl.height, c.ctrl.posy},
		})
	})

	// Stick position
	stickNode, err := c.render.NewNode()
	if err != nil {
		log.Fatalf("Can't load scene: %v", err)
	}
	stickNode.Arranger = arrangerFunc(func(eng sprite.Engine, stickNode *sprite.Node, t clock.Time) {
		eng.SetSubTex(stickNode, c.ctrl.stick.subTex)
		c.ctrl.updateMiddle()
		c.ctrl.updatePosition()
		eng.SetTransform(stickNode, f32.Affine{
			{c.ctrl.stick.width, 0, c.ctrl.stick.posx},
			{0, c.ctrl.stick.height, c.ctrl.stick.posy},
		})
	})

}

type arrangerFunc func(e sprite.Engine, n *sprite.Node, t clock.Time)

func (a arrangerFunc) Arrange(e sprite.Engine, n *sprite.Node, t clock.Time) { a(e, n, t) }
