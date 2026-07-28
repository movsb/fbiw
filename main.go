package main

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

const (
	windowWidth  = 1024
	windowHeight = 768
)

func main() {
	box := loadBox(_main)
	dialog := loadBox(_dialog)

	canvas := Canvas{
		bytesPerPixel: 4,
		x:             0,
		y:             0,
		width:         windowWidth,
		height:        windowHeight,
	}

	var frameCounter atomic.Uint64

	go func() {
		var last = uint64(0)
		for {
			time.Sleep(time.Second)
			curr := frameCounter.Load()
			fmt.Println(`FPS:`, curr-last)
			last = curr
		}
	}()

	now := time.Now()

	dialogWidth, dialogHeight := 600, 400

	loop(func(buffer []byte) {
		canvas.buffer = buffer

		box.Draw(&canvas, windowWidth, windowHeight)
		dialog.Draw(canvas.Offset((windowWidth-dialogWidth)/2, (windowHeight-dialogHeight)/2), dialogWidth, dialogHeight)

		frameCounter.Add(1)

		if runtime.GOOS == `linux` {
			if time.Since(now) > time.Second*5 {
				os.Exit(0)
			}
		}
	})
}
