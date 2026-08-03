package fbiw

import (
	"context"
	"io/fs"
	"log"
	"slices"
	"time"
)

type Option func(app *App)

func WithSystemFont(path string) Option {
	return func(app *App) {
		if err := app.AddFont(`system`, false, false, path); err != nil {
			log.Fatal(`添加系统字体时错误:`, err)
		}
	}
}

// 应用程序实例。
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	system chan Event

	display *Display
	canvas  *Canvas
	images  *ImageManager
	fonts   *FontManager
	root    fs.FS

	// 层叠的窗口列表。
	// 上面的在后面。
	documents []*Document
}

func NewApp(
	ctx context.Context,
	fileSystem fs.FS,
	options ...Option,
) *App {
	ctx, cancel := context.WithCancel(ctx)

	display := openDisplay()

	app := &App{
		ctx:    ctx,
		cancel: cancel,

		root:    fileSystem,
		display: display,
		canvas:  NewCanvas(display),
		images:  NewImageManager(),
		fonts:   NewFontManager(),

		system: make(chan Event),
	}

	for _, opt := range options {
		opt(app)
	}

	return app
}

func (app *App) Close() {
	defer app.display.Close()
	defer app.images.Close()
	defer app.fonts.Close()
}

func (app *App) New(name string, skinDir string) *Document {
	doc := _NewDocument(
		app.canvas.width, app.canvas.height,
		app.root, app.fonts, app.images,
	)
	if err := doc.load(name, skinDir); err != nil {
		panic(err)
	}
	doc.display = false
	app.documents = append(app.documents, doc)
	return doc
}

func (app *App) _CloseDocument(doc *Document) {
	app.documents = slices.DeleteFunc(app.documents, func(d *Document) bool {
		return d == doc
	})
	app.sync()
}

func (app *App) Show(doc *Document) {
	doc.display = true
	app.sync()
}

// 用于在主线程中调用此回调函数。
func (app *App) Async(callback func()) {
	app.system <- Event{
		Type:          asyncCallback,
		asyncCallback: callback,
	}
}

func (app *App) Run() {
	menuPressed := false
	startPressed := false
	pollEvents(app.ctx, app.system, app.sync, func(event Event) {
		switch event.Type {
		case QuitEvent:
			app.cancel()
			return
		case KeyboardEvent:
			switch event.Keyboard.Name {
			case Menu:
				menuPressed = event.Keyboard.Pressed
			case Start:
				startPressed = event.Keyboard.Pressed
			}
			if menuPressed && startPressed {
				app.cancel()
				return
			}
		}
	})
}

func (app *App) AddFont(family string, bold, italic bool, path string) error {
	return app.fonts.AddFont(path, family, bold, italic)
}

func (app *App) sync() {
	needSync := false
	for _, doc := range app.documents {
		if doc.display && doc.dirty() {
			now := time.Now()
			doc.sync(app.canvas)
			log.Println(`帧绘制时长：`, time.Since(now).Round(time.Microsecond*100))
			needSync = true
		}
	}
	if needSync {
		app.display.Sync()
	}
}
