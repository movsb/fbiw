//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/veandco/go-sdl2/sdl"
)

var fileSystem = os.DirFS(`.`)

const defaultFontFileRegular = `./fonts/MapleMonoNormalNL-NF-CN-Regular.ttf`
const defaultFontFileBold = `./fonts/MapleMonoNormalNL-NF-CN-Bold.ttf`

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

	originRect := sdl.Rect{X: 0, Y: 0, W: windowWidth, H: windowHeight}
	scaledRect := sdl.Rect{X: 0, Y: 0, W: windowWidth, H: windowHeight}

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
		default:
			handler(Event{})
		}
	}
}

func loadFonts(fontManager *FontManager) error {
	for dir, faces := range map[string][]struct {
		FileName string
		Family   string
		Bold     bool
		Italic   bool
	}{
		`fonts/`: {
			{
				FileName: `MapleMonoNormalNL-NF-CN-Regular.ttf`,
				Family:   `MapleMono`,
				Bold:     false,
				Italic:   false,
			},
			{
				FileName: `MapleMonoNormalNL-NF-CN-Italic.ttf`,
				Family:   `MapleMono`,
				Bold:     false,
				Italic:   true,
			},
			{
				FileName: `MapleMonoNormalNL-NF-CN-Bold.ttf`,
				Family:   `MapleMono`,
				Bold:     true,
				Italic:   false,
			},
			{
				FileName: `MapleMonoNormalNL-NF-CN-BoldItalic.ttf`,
				Family:   `MapleMono`,
				Bold:     true,
				Italic:   true,
			},
		},
	} {
		for _, face := range faces {
			if err := fontManager.AddFont(
				filepath.Join(dir, face.FileName),
				face.Family, face.Bold, face.Italic,
			); err != nil {
				return fmt.Errorf(`加载字体失败：%w`, err)
			}
		}
	}
	return nil
}
