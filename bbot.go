package main

import (
	"fmt"
	"image"
	_ "image/png"
	"io"
	"math"
	"net"

	pb "github.com/viru/berrybot/proto"

	"golang.org/x/mobile/asset"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/exp/f32"
	"golang.org/x/mobile/exp/sprite"
	"golang.org/x/mobile/exp/sprite/clock"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
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
	bot struct {
		x, y        float32
		front, rear *echo
	}
}

type echo struct {
	sm, md, lg int
}

func (e *echo) clear() {
	e.sm = 0
	e.md = 0
	e.lg = 0
}

func (e *echo) far() {
	e.sm = 1
	e.md = 0
	e.lg = 0
}

func (e *echo) mid() {
	e.sm = 2
	e.md = 1
	e.lg = 0
}

func (e *echo) close() {
	e.sm = 3
	e.md = 2
	e.lg = 1
}

func (e *echo) SetDist(d int32) {
	switch {
	case d >= 100:
		e.far()
	case d < 100 && d >= 50:
		e.mid()
	case d < 50:
		e.close()
	}
}

// NewApp creates new app.
func NewApp() *App {
	a := App{}
	a.bot.front = &echo{}
	a.bot.rear = &echo{}

	go func() {
		for {
			if !a.connected {
				a.discoverBot()
				continue
			}
			t, err := a.DriveStream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			a.bot.front.SetDist(t.DistFront)
			a.bot.rear.SetDist(t.DistRear)
		}
	}()
	return &a
}

const defaultBcastPort = "8032"

// discoverBot listens for UDP broadcasts on port 8032 and tries to connect to
// the first server it finds. This function blocks.
func (a *App) discoverBot() {
	// Listen for bots on broadcast.
	log.Printf("Listening on UDP/%s...", defaultBcastPort)
	c, err := net.ListenPacket("udp", ":"+defaultBcastPort)
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
	log.Print("Connected")
}

// Size of controller and stick inside in points.
const (
	ctrlSize      = 80
	ctrlStickSize = 35
	botSize       = 60
)

// Reset sets default positions, should be used after orientation change, etc.
func (a *App) Reset(sz size.Event) {
	if sz.PixelsPerPt == 0 || sz.HeightPt == 0 || sz.WidthPt == 0 {
		return
	}
	a.ctrl.x = float32(sz.WidthPt)/2 - ctrlSize/2
	a.ctrl.y = 3*float32(sz.HeightPt)/4 - ctrlSize/2
	a.ctrl.midx = a.ctrl.x + ctrlSize/2
	a.ctrl.midy = a.ctrl.y + ctrlSize/2
	a.stick.x = float32(sz.WidthPt)/2 - ctrlStickSize/2
	a.stick.y = 3*float32(sz.HeightPt)/4 - ctrlStickSize/2
	a.calcStickMids()
	a.bot.x = float32(sz.WidthPt)/2 - botSize/2
	a.bot.y = float32(sz.HeightPt)/3 - botSize/2
}

func (a *App) calcStickMids() {
	a.stick.midx = a.stick.x + ctrlStickSize/2
	a.stick.midy = a.stick.y + ctrlStickSize/2
}

const ctrlRadius = 21

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
	if d2 < float64(ctrlRadius*ctrlRadius) {
		a.stick.x = xp - ctrlStickSize/2
		a.stick.y = yp - ctrlStickSize/2
	} else {
		a.stick.x = xc + ctrlRadius*((xp-xc)/float32(math.Sqrt(d2))) - ctrlStickSize/2
		a.stick.y = yc + ctrlRadius*((yp-yc)/float32(math.Sqrt(d2))) - ctrlStickSize/2
	}
	a.calcStickMids()
	a.SendDrive()
}

// ResetStick sets stick to rest position.
func (a *App) ResetStick(sz size.Event) {
	if sz.PixelsPerPt == 0 || sz.HeightPt == 0 || sz.WidthPt == 0 {
		return
	}
	a.stick.x = float32(sz.WidthPt)/2 - ctrlStickSize/2
	a.stick.y = 3*float32(sz.HeightPt)/4 - ctrlStickSize/2
	a.calcStickMids()
	a.SendDrive()
}

// SendDrive sends drive message over gRPC.
func (a *App) SendDrive() {
	if !a.connected {
		return
	}
	d := new(pb.Direction)
	d.Dx = int32((a.stick.midx - a.ctrl.midx) * 100 / ctrlRadius)
	d.Dy = int32((a.ctrl.midy - a.stick.midy) * 100 / ctrlRadius)
	if err := a.DriveStream.Send(d); err != nil {
		log.Fatalf("%v.Send(%v) = %v", a.DriveStream, d, err)
	}
}

// Scene creates and returns a new app scene.
func (a *App) Scene(eng sprite.Engine, sz size.Event) *sprite.Node {
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

	// Bot.
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texBot])
		eng.SetTransform(n, f32.Affine{
			{botSize, 0, a.bot.x},
			{0, botSize, a.bot.y},
		})
	})

	// Proximity small.
	const (
		smSizeW = 15
		smSizeH = 5
	)
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texProxSmGrey+a.bot.front.sm])
		eng.SetTransform(n, f32.Affine{
			{smSizeW, 0, a.bot.x + botSize/2 - smSizeW/2},
			{0, smSizeH, a.bot.y - 2*smSizeH},
		})
	})
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texProxSmGrey+a.bot.rear.sm])
		eng.SetTransform(n, f32.Affine{
			{smSizeW, 0, a.bot.x + botSize/2 - smSizeW/2},
			{0, -smSizeH, a.bot.y + botSize + 2*smSizeH},
		})
	})

	// Proximity medium.
	const (
		mdSizeW = 25
		mdSizeH = 7
	)
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texProxMdGrey+a.bot.front.md])
		eng.SetTransform(n, f32.Affine{
			{mdSizeW, 0, a.bot.x + botSize/2 - mdSizeW/2},
			{0, mdSizeH, a.bot.y - 3*mdSizeH},
		})
	})
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texProxMdGrey+a.bot.rear.md])
		eng.SetTransform(n, f32.Affine{
			{mdSizeW, 0, a.bot.x + botSize/2 - mdSizeW/2},
			{0, -mdSizeH, a.bot.y + botSize + 3*mdSizeH},
		})
	})

	// Proximity large
	const (
		lgSizeW = 35
		lgSizeH = 10
	)
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texProxLgGrey+a.bot.front.lg])
		eng.SetTransform(n, f32.Affine{
			{lgSizeW, 0, a.bot.x + botSize/2 - lgSizeW/2},
			{0, lgSizeH, a.bot.y - 3.2*lgSizeH},
		})
	})
	newNode(func(eng sprite.Engine, n *sprite.Node, t clock.Time) {
		eng.SetSubTex(n, texs[texProxLgGrey+a.bot.rear.lg])
		eng.SetTransform(n, f32.Affine{
			{lgSizeW, 0, a.bot.x + botSize/2 - lgSizeW/2},
			{0, -lgSizeH, a.bot.y + botSize + 3.2*lgSizeH},
		})
	})

	return scene
}

type arrangerFunc func(e sprite.Engine, n *sprite.Node, t clock.Time)

func (a arrangerFunc) Arrange(e sprite.Engine, n *sprite.Node, t clock.Time) { a(e, n, t) }

const (
	texCtrl = iota
	texStick
	texBot
	texProxSmGrey
	texProxSmGreen
	texProxSmOrange
	texProxSmRed
	texProxMdGrey
	texProxMdOrange
	texProxMdRed
	texProxLgGrey
	texProxLgRed
)

func loadTextures(eng sprite.Engine) []sprite.SubTex {
	a, err := asset.Open("assets.png")
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
		texCtrl:         sprite.SubTex{T: t, R: image.Rect(0, 0, 320, 320)},
		texStick:        sprite.SubTex{T: t, R: image.Rect(320, 0, 440, 120)},
		texBot:          sprite.SubTex{T: t, R: image.Rect(0, 320, 300, 610)},
		texProxSmGrey:   sprite.SubTex{T: t, R: image.Rect(300, 320, 300+50, 320+15)},
		texProxSmGreen:  sprite.SubTex{T: t, R: image.Rect(300, 320+15, 300+50, 320+30)},
		texProxSmOrange: sprite.SubTex{T: t, R: image.Rect(300, 320+30, 300+50, 320+45)},
		texProxSmRed:    sprite.SubTex{T: t, R: image.Rect(300, 320+45, 300+50, 320+60)},
		texProxMdGrey:   sprite.SubTex{T: t, R: image.Rect(300, 320+60, 300+78, 320+80)},
		texProxMdOrange: sprite.SubTex{T: t, R: image.Rect(300, 320+80, 300+78, 320+100)},
		texProxMdRed:    sprite.SubTex{T: t, R: image.Rect(300, 320+100, 300+78, 320+120)},
		texProxLgGrey:   sprite.SubTex{T: t, R: image.Rect(300, 320+120, 300+110, 320+147)},
		texProxLgRed:    sprite.SubTex{T: t, R: image.Rect(300, 320+147, 300+110, 320+174)},
	}
}
