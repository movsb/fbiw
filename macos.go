//go:build darwin

package fbiw

import (
	"context"

	"github.com/veandco/go-sdl2/sdl"
)

type _SdlDisplay struct {
	width, height int

	window  *sdl.Window
	surface *sdl.Surface
	buffer  *sdl.Surface
}

func (d *_SdlDisplay) GetSize() (int, int, int) {
	return d.width, d.height, d.width * 4
}

func (d *_SdlDisplay) Sync(pixels []byte) {
	copy(d.buffer.Pixels(), pixels)

	originRect := sdl.Rect{X: 0, Y: 0, W: int32(d.width), H: int32(d.height)}
	scaledRect := sdl.Rect{X: 0, Y: 0, W: int32(d.width), H: int32(d.height)}

	d.buffer.Blit(&originRect, d.surface, &scaledRect)
	d.window.UpdateSurface()
}

func (d *_SdlDisplay) Close() {
	d.window.Destroy()
	sdl.Quit()
}

// 创建一个固定大小的基于SDL2的显示层。
func OpenDisplay() Display {
	const (
		windowWidth  = 1024
		windowHeight = 768
	)

	if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
		panic(err)
	}

	// 启动即关闭输入法。
	sdl.StopTextInput()

	window, err := sdl.CreateWindow("fbiw",
		sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED,
		windowWidth, windowHeight, sdl.WINDOW_SHOWN,
	)
	if err != nil {
		panic(err)
	}

	// wid, _ := window.GetID()

	surface, err := window.GetSurface()
	if err != nil {
		panic(err)
	}

	buffer, err := sdl.CreateRGBSurface(0, windowWidth, windowHeight, 32, 0, 0, 0, 0)
	if err != nil {
		panic(err)
	}

	return &_SdlDisplay{
		width:   windowWidth,
		height:  windowHeight,
		window:  window,
		surface: surface,
		buffer:  buffer,
	}
}

func pollEvents(
	ctx context.Context, cancel context.CancelFunc,
	unblock chan struct{}, unblockHandler func(),
	sync func(), eventHandler func(*Event),
) {
	sendKey := func(name KeyName, pressed, repeat bool) {
		eventHandler(&Event{
			Type: Iif(pressed, StickDownEvent, StickUpEvent),
			Stick: KeyEventArgs{
				Name:   name,
				Repeat: repeat,
			},
		})
	}

	keyMaps := map[sdl.Keycode]KeyName{
		sdl.K_w: Up,
		sdl.K_s: Down,
		sdl.K_a: Left,
		sdl.K_d: Right,
		sdl.K_k: A,
		sdl.K_j: B,
		sdl.K_i: X,
		sdl.K_u: Y,
		sdl.K_r: Menu,
		sdl.K_t: Select,
		sdl.K_y: Start,
		sdl.K_q: L1,
		sdl.K_o: R1,
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-unblock:
			unblockHandler()
		default:
		}
		switch event := sdl.PollEvent().(type) {
		case *sdl.QuitEvent:
			cancel()
			return
		case *sdl.KeyboardEvent:
			pressed := event.Type == sdl.KEYDOWN
			key := event.Keysym.Sym
			if mapped, ok := keyMaps[key]; ok {
				sendKey(mapped, pressed, event.Repeat != 0)
			}
		}
		sync()
	}
}
