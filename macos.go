//go:build darwin

package main

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

	window, err := sdl.CreateWindow("gofb",
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

	originRect := sdl.Rect{0, 0, windowWidth, windowHeight}
	scaledRect := sdl.Rect{0, 0, windowWidth, windowHeight}

	d.Sync = func() {
		copy(buffer.Pixels(), d.Data)
		buffer.Blit(&originRect, surface, &scaledRect)
		window.UpdateSurface()
	}

	d.Close = func() {
		window.Destroy()
		sdl.Quit()
	}

	return d
}

func pollEvents(ctx context.Context, handler func(Event)) {
	sendKey := func(name KeyName, pressed bool) {
		handler(Event{
			Type: KeyboardEvent,
			Keyboard: KeyboardEventArgs{
				Name:    name,
				Pressed: pressed,
			},
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		switch event := sdl.PollEvent().(type) {
		case *sdl.QuitEvent:
			handler(Event{Type: QuitEvent})
			return
		case *sdl.KeyboardEvent:
			keyDown := event.Type == sdl.KEYDOWN
			switch event.Keysym.Sym {
			case sdl.K_w:
				sendKey(Up, keyDown)
			}
		}
	}
}
