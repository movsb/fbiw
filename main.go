package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
)

func init() {
	// pprof 性能测试用。
	go http.ListenAndServe(`0.0.0.0:8888`, nil)
}

func main() {
	display := openDisplay()
	defer display.Close()

	canvas := &Canvas{
		buffer:        display.Data,
		bytesPerPixel: display.Bpp / 8,
		x:             0,
		y:             0,
		width:         display.Width,
		height:        display.Height,
	}

	box := loadBox(_main)
	box.Base().Dirty = true
	box.Draw(canvas, display.Width, display.Height)
	display.Sync()

	// 没有缓冲，满了（或处理不过来）即丢。
	ctx, cancel := context.WithCancel(context.Background())

	menuPressed := false
	startPressed := false

	pollEvents(ctx, func(event Event) {
		if event.Type == QuitEvent {
			cancel()
			return
		}
		if event.Type == KeyboardEvent {
			switch event.Keyboard.Name {
			case Menu:
				menuPressed = event.Keyboard.Pressed
			case Start:
				startPressed = event.Keyboard.Pressed
			}
			if menuPressed && startPressed {
				cancel()
				return
			}
		}
		if event.Type == KeyboardEvent {
			text := box.Base().Children[0].(*Button).Base().Children[0].(*Text)
			pressed := iif(event.Keyboard.Pressed, "按下", "松开")
			text.Data = fmt.Sprintf(`按键：%s (%s)`, event.Keyboard.Name, pressed)
			text.Base().Dirty = true
		}
		if box.Base().IsDirty() {
			box.Draw(canvas, display.Width, display.Height)
			display.Sync()
		}
	})
}
