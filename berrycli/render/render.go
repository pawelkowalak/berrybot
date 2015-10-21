// Package render provides a service for berrycli that creates GL context,
// rendering engine, empty scene and allows to create new nodes attached
// to it. Also allows to load textures from files. Import proper image codecs
// in your main package.
package render

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"time"

	"github.com/golang/freetype/truetype"
	"golang.org/x/mobile/asset"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/exp/app/debug"
	"golang.org/x/mobile/exp/f32"
	"golang.org/x/mobile/exp/gl/glutil"
	"golang.org/x/mobile/exp/sprite"
	"golang.org/x/mobile/exp/sprite/clock"
	"golang.org/x/mobile/exp/sprite/glsprite"
	"golang.org/x/mobile/gl"
)

// Service provides its exported methods to init, create nodes, load textures
// and finally teardown.
type Service struct {
	glctx     gl.Context
	font      *truetype.Font
	startTime time.Time
	images    *glutil.Images
	engine    sprite.Engine
	scene     *sprite.Node
	fps       *debug.FPS
	loaded    bool
}

// NewService initializes new render service.
func NewService(start time.Time) *Service {
	return &Service{startTime: start}
}

// Init initializes GL context, creates engine and new scene. It should be
// called on CrossOn event.
func (s *Service) Init(glctx gl.Context) {
	// Enable blending for alpha channel in PNG files.
	s.glctx = glctx
	s.glctx.Enable(gl.BLEND)
	s.glctx.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	s.images = glutil.NewImages(s.glctx)
	s.engine = glsprite.Engine(s.images)
	s.fps = debug.NewFPS(s.images)
	s.scene = new(sprite.Node)
	s.engine.Register(s.scene)
	s.engine.SetTransform(s.scene, f32.Affine{
		{1, 0, 0},
		{0, 1, 0},
	})
	s.loaded = true
}

// Teardown releases engine and clears GL context. Called at CrossOff event.
func (s *Service) Teardown() {
	s.loaded = false
	s.engine.Release()
	s.fps.Release()
	s.images.Release()
	s.glctx = nil
}

// NewNode creates new node, registers in the engine and appends to current scene.
func (s *Service) NewNode() (*sprite.Node, error) {
	if s.scene == nil {
		return nil, errors.New("scene doesn't exists, is render service initialized?")
	}
	n := new(sprite.Node)
	s.engine.Register(n)
	s.scene.AppendChild(n)
	return n, nil
}

// OpenTexture opens given filename and loads it as a texture in the engine.
func (s *Service) OpenTexture(name string) (sprite.Texture, error) {
	a, err := asset.Open(name)
	if err != nil {
		return nil, fmt.Errorf("can't open texture file: %v", err)
	}
	defer a.Close()

	img, _, err := image.Decode(a)
	if err != nil {
		return nil, fmt.Errorf("can't decode texture file: %v", err)
	}
	t, err := s.engine.LoadTexture(img)
	if err != nil {
		return nil, fmt.Errorf("can't load texture file: %v", err)
	}
	return t, nil
}

func (s *Service) LoadTexture(data []byte) (sprite.Texture, error) {
	if !s.loaded {
		return nil, errors.New("engine not loaded yet")
	}
	r := bytes.NewReader(data)
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("can't decode texture file: %v", err)
	}
	t, err := s.engine.LoadTexture(img)
	if err != nil {
		return nil, fmt.Errorf("can't load texture file: %v", err)
	}
	return t, nil
}

// Render fills screen with background color, clears buffer and renders scene.
func (s *Service) Render(sz size.Event) error {
	if !s.loaded {
		return errors.New("engine not loaded")
	}
	// Fill background with white.
	s.glctx.ClearColor(0, 0, 0, 1)
	s.glctx.Clear(gl.COLOR_BUFFER_BIT)
	now := clock.Time(time.Since(s.startTime) * 60 / time.Second)
	s.engine.Render(s.scene, now, sz)
	s.fps.Draw(sz)
	return nil
}
