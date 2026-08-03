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

type App struct {
	ctx    context.Context
	cancel context.CancelFunc
	system chan Event

	display      *Display
	imageManager *ImageManager
	fontManager  *FontManager

	txtCat  *Text
	txtTime *Text

	menuPressed  bool
	startPressed bool

	windows []*Document
}

func NewApp(
	display *Display,
	imageManager *ImageManager,
	fontManager *FontManager,
) *App {
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		ctx:    ctx,
		cancel: cancel,
		system: make(chan Event),

		display:      display,
		fontManager:  fontManager,
		imageManager: imageManager,
	}
	app.Init()
	return app
}

func (app *App) Init() {
	mainDoc := NewDocument(fileSystem, app.fontManager, app.imageManager)
	if err := mainDoc.Load(`main.html`, `skin`); err != nil {
		log.Fatalln(err)
	}

	canvas := NewCanvas(app.display)
	mainDoc.SetCanvas(canvas)

	app.txtTime = mainDoc.QuerySelector(`#time`).(*Text)

	app.windows = append(app.windows, mainDoc)

	go func() {
		for range time.Tick(time.Second * 1) {
			app.Async(func() {
				now := time.Now().Format(`15:04:05`)
				app.txtTime.SetText(now)
			})
		}
	}()
}

func (app *App) Async(fn func()) {
	app.system <- Event{
		Type:          AsyncCallback,
		AsyncCallback: fn,
	}
}

func (app *App) sync() {
	needSync := false
	for _, window := range app.windows {
		if window.Dirty() {
			now := time.Now()
			window.Sync()
			log.Println(`帧绘制时长：`, time.Since(now).Round(time.Microsecond*100))
			needSync = true
		}
	}
	if needSync {
		app.display.Sync()
	}
}

func (app *App) Run() {
	pollEvents(app.ctx, app.system, app.sync, func(event Event) {
		switch event.Type {
		case QuitEvent:
			app.cancel()
			return
		case KeyboardEvent:
			switch event.Keyboard.Name {
			case Menu:
				app.menuPressed = event.Keyboard.Pressed
			case Start:
				app.startPressed = event.Keyboard.Pressed
			}
			if app.menuPressed && app.startPressed {
				app.cancel()
				return
			}
		}
	})
}

func main() {
	display := openDisplay()
	defer display.Close()

	fontManager := initFonts()
	defer fontManager.Close()

	imageManager := NewImageManager()
	defer imageManager.Close()

	app := NewApp(display, imageManager, fontManager)
	app.Run()
}
