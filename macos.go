//go:build darwin

package fbiw

import (
	"context"

	"github.com/veandco/go-sdl2/sdl"
)

func openDisplay() *Display {
	const (
		windowWidth  = 1024
		windowHeight = 768
	)

	d := &Display{
		Width:  windowWidth,
		Height: windowHeight,
		Bpp:    32,
		Stride: windowWidth * 32 / 8,
	}

	if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
		panic(err)
	}
	// defer sdl.Quit()

	// 启动即关闭输入法。
	sdl.StopTextInput()

	window, err := sdl.CreateWindow("fbiw",
		sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED,
		windowWidth, windowHeight, sdl.WINDOW_SHOWN,
	)
	if err != nil {
		panic(err)
	}
	// defer window.Destroy()

	// wid, _ := window.GetID()

	surface, err := window.GetSurface()
	if err != nil {
		panic(err)
	}

	buffer, err := sdl.CreateRGBSurface(0, windowWidth, windowHeight, 32, 0, 0, 0, 0)
	if err != nil {
		panic(err)
	}
	// d.Data = buffer.Pixels()
	d.Data = make([]byte, len(buffer.Pixels()))

	originRect := sdl.Rect{X: 0, Y: 0, W: windowWidth, H: windowHeight}
	scaledRect := sdl.Rect{X: 0, Y: 0, W: windowWidth, H: windowHeight}

	d.sync = func() {
		copy(buffer.Pixels(), d.Data)
		buffer.Blit(&originRect, surface, &scaledRect)
		window.UpdateSurface()
	}

	d.close = func() {
		window.Destroy()
		sdl.Quit()
	}

	return d
}

func pollEvents(ctx context.Context, system chan Event, sync func(), handler func(Event)) {
	sendKey := func(name KeyName, pressed bool) {
		handler(Event{
			Type: KeyboardEvent,
			Keyboard: KeyboardEventArgs{
				Name:    name,
				Pressed: pressed,
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
		polled := false
		select {
		case <-ctx.Done():
			return
		case event := <-system:
			switch event.Type {
			case asyncCallback:
				event.asyncCallback()
				polled = true
			case appDirty:
				polled = true
			default:
				handler(event)
				polled = true
			}
		default:
		}
		switch event := sdl.PollEvent().(type) {
		case *sdl.QuitEvent:
			handler(Event{Type: QuitEvent})
			return
		case *sdl.KeyboardEvent:
			pressed := event.Type == sdl.KEYDOWN
			key := event.Keysym.Sym
			if mapped, ok := keyMaps[key]; ok {
				sendKey(mapped, pressed)
			}
			polled = true
		}
		if polled {
			sync()
		}
	}
}
