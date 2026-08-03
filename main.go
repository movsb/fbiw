package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"time"
)

// pprof 性能测试用。
//
// go tool pprof -web  http://localhost:8888/debug/pprof/profile?seconds=30
func init() {
	go http.ListenAndServe(`0.0.0.0:8888`, nil)
}

func initFonts() *FontManager {
	fontManager := NewFontManager()
	if err := fontManager.AddFont(defaultFontFileRegular, `system`, false, false); err != nil {
		log.Panic(`加载默认字体失败：`, err)
	}
	if err := loadFonts(fontManager); err != nil {
		panic(`字体加载失败：` + err.Error())
	}
	return fontManager
}

func main() {
	display := openDisplay()
	defer display.Close()

	fontManager := initFonts()
	defer fontManager.Close()

	imageManager := NewImageManager()
	defer imageManager.Close()

	mainDoc := NewDocument(fileSystem, fontManager, imageManager)
	if err := mainDoc.Load(`main.html`, `skin`); err != nil {
		log.Fatalln(err)
	}

	canvas := NewCanvas(display)
	mainDoc.SetCanvas(canvas)
	mainDoc.Sync()
	display.Sync()

	menuPressed := false
	startPressed := false

	ctx, cancel := context.WithCancel(context.Background())
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
		if event.Type == KeyboardEvent && event.Keyboard.Name == Up && event.Keyboard.Pressed {
			buttons := QuerySelector[*Inline](mainDoc, `#buttons`)
			first := buttons.Children[0].(*Block)
			first.Class.Toggle(`last`)
		}
		if mainDoc.Dirty() {
			now := time.Now()
			mainDoc.Sync()
			log.Println(`帧绘制时长：`, time.Since(now).Round(time.Microsecond*100))
			display.Sync()
		}
	})
}
