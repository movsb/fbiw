package main

import (
	"context"
	"gofb/style"
	"log"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"
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
	styles := loadStyles()
	applyStyles(box, []*style.Sheet{styles})
	box.Base().Dirty = true

	box.Calc(display.Width, display.Height)
	box.Draw(canvas)
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
			// 在linux上测试总是跟随按键更新重绘。
			box.Base().Dirty = true
		}
		// 在MacOS上更方便观察帧率。
		if box.Base().IsDirty() || runtime.GOOS == `darwin` {
			now := time.Now()
			box.Calc(display.Width, display.Height)
			box.Draw(canvas)
			log.Println(`帧绘制时长：`, time.Since(now).Round(time.Microsecond*100))
			display.Sync()
		}
	})
}
