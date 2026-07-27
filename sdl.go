//go:build darwin

package main

import (
	"github.com/veandco/go-sdl2/sdl"
)

func loop(render func(buffer []byte)) {
	if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
		panic(err)
	}
	defer sdl.Quit()

	window, err := sdl.CreateWindow("gofb",
		sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED,
		windowWidth, windowHeight, sdl.WINDOW_SHOWN,
	)
	if err != nil {
		panic(err)
	}
	defer window.Destroy()

	wid, _ := window.GetID()

	surface, err := window.GetSurface()
	if err != nil {
		panic(err)
	}

	buffer, err := sdl.CreateRGBSurface(0, windowWidth, windowHeight, 32, 0, 0, 0, 0)
	if err != nil {
		panic(err)
	}

	bufPixels := buffer.Pixels()

	var originRect = &sdl.Rect{0, 0, windowWidth, windowHeight}
	var scaledRect = &sdl.Rect{0, 0, windowWidth, windowHeight}

	for run := true; run; {
		switch evt := sdl.PollEvent().(type) {
		case *sdl.KeyboardEvent:
			if evt.WindowID == wid {
				switch evt.Keysym.Sym {
				}
			}
		case *sdl.QuitEvent:
			run = false
		}

		render(bufPixels)

		buffer.Blit(originRect, surface, scaledRect)
		window.UpdateSurface()
	}
}
