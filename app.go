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

	// 是否脱离到系统后台。
	// 在系统后台的时候不刷屏、不处理键盘事件。
	// 重新附加后，屏幕会刷新一次。
	detached bool
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
	app.cancel()
}

func (app *App) Context() context.Context {
	return app.ctx
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
	doc.app = app
	app.documents = append(app.documents, doc)
	return doc
}

func (app *App) _CloseDocument(doc *Document) {
	app.documents = slices.DeleteFunc(app.documents, func(d *Document) bool {
		return d == doc
	})
	app.sync()
}

// 同步标记为脏，异步等待下次刷新。
//
// 只起标记作用，文档是否需要重绘还要看文档本身。
func (app *App) Dirty() {
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

func (app *App) Show(doc *Document, show ...bool) {
	if len(show) > 0 {
		doc.display = show[0]
	} else {
		doc.display = true
	}
	doc.RequestPaint()
}

// 用于其它线程创建一个将来会在主线程中调用的回调函数。
//
// 方便用于在非主线程中安全更新UI操作。
//
// 调用会立即返回，不会阻塞。
//
// 每次回调都会额外触发 sync 以检测是否有内容更新。
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
			if app.detached {
				return
			}

			// 按“菜单”和“开始”可以退出。
			// 暂时固定给所有APP。
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

			// 发送给前台文档。
			for _, doc := range slices.Backward(app.documents) {
				if !doc.display {
					continue
				}
				doc.handleKeyboardEvent(event.Keyboard)
			}
		}
	})
}

func (app *App) AddFont(family string, bold, italic bool, path string) error {
	return app.fonts.AddFont(path, family, bold, italic)
}

// 脱离当前与操作系统的事件交互，比如屏幕、键盘。
// 需要在主线程中调用。
// 用于Linux系统独占，MacOS无效。
func (app *App) Detach() {
	app.detached = true
}

// 重新夺取操作系统事件交互，比如屏幕、键盘。
// 需要在主线程中调用。
// 用于Linux系统独占，MacOS无效。
func (app *App) Attach() {
	app.detached = false
	app.Dirty()
}

// 真正执行检测是否需要重新布局或重绘的地方。
func (app *App) sync() {
	if app.detached {
		return
	}
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

type Delegator interface {
	HandleKeyboardEvent(name KeyName, pressed bool)
}
