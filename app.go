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

	// 显示或隐藏文档会影响所有，而不能仅仅在 sync
	// 的时候判断如果隐藏就不绘制、如果显示就绘制。
	dirty bool
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

		// 像是Show之类的改动绘制的函数不应该主动调用sync方法，
		// 如果调用，可能阻塞？因为正在处理事件，主循环没执行。
		system: make(chan Event, 8),
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

func (app *App) Show(doc *Document, show ...bool) {
	if len(show) > 0 {
		doc.display = show[0]
	} else {
		doc.display = true
	}

	doc.paintDirty = true
	app.dirty = true

	// 现在有可能正处于事件处理过程中，主循环没循环，
	// 系统事件也没及时处理，所以开个线程去写，否则
	// 可能就死锁了。
	go func() {
		select {
		case app.system <- Event{Type: appDirty}:
		case <-app.ctx.Done():
		}
	}()
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
		if app.dirty || doc.dirty() {
			if doc.display {
				now := time.Now()
				doc.sync(app.canvas)
				log.Println(`帧绘制时长：`, time.Since(now).Round(time.Microsecond*100))
			}
			needSync = true
		}
	}
	if needSync {
		app.display.Sync()
		app.dirty = false
	}
}
