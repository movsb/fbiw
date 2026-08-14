package fbiw

import (
	"context"
	"io/fs"
	"log"
	"slices"
	"time"
)

type Option func(app *App)

// 添加系统字体。失败并不会退出。
func WithSystemFont(fsys fs.FS, path string) Option {
	return func(app *App) {
		if err := app.AddFont(`system`, false, false, fsys, path); err != nil {
			log.Println(`添加系统字体时错误:`, err)
		}
	}
}

func WithFont(family string, bold, italic bool, fsys fs.FS, path string) Option {
	return func(app *App) {
		if err := app.AddFont(family, bold, italic, fsys, path); err != nil {
			log.Println(`添加系统字体时错误:`, err)
		}
	}
}

// 应用程序实例。
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	system chan *Event

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
	//
	// 类型为整数，记录detach次数。
	// 比如：进入游戏时detach一次，此时还未attach；
	// 但是又出现了osd屏幕，会再次detach。
	// detached为0时表示未detach。
	detached int
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
		//
		// 数值是随意设置的，并不是一定要这么大。最理想的（或者说
		// 最应该）的做法是不要缓冲，因为这个机制的实现就是为了给
		// 其它线程往主线程投递消息用的，本来就不应该有任何阻塞的
		// 可能。除非是在主线程中调用了只给其它线程调用的Async方法，
		// 那确实有可能死锁。（Async已经改了，连在其它线程给主线程
		// 投递消息也新开了一个线程。创建goroutine跟不要钱一样。）
		system: make(chan *Event, 8),
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

// 创建新的文档，并绑定到此App上作为前台窗口。
//
// 创建的文档默认不显示，需要 Show()。
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
	// 默认把焦点设置给根元素。
	doc.root.Base().Activate()
	// 追加到后面（最上层窗口）
	app.documents = append(app.documents, doc)
	return doc
}

func (app *App) _CloseDocument(doc *Document) {
	app.documents = slices.DeleteFunc(app.documents, func(d *Document) bool {
		return d == doc
	})
	app.Dirty()
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
		case app.system <- &Event{Type: appDirty}:
		case <-app.ctx.Done():
		}
	}()
}

// 把文档设置为显示状态。
//
// 显示后键盘事件发发送到这里。
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
// 调用会立即返回，不会阻塞。除非你敢在主线程中调用此函数，那就死给你看。
// 每次回调都会额外触发 sync 以检测是否有内容更新。
//
// 我认怂了：某些用于创建服务的函数，比如启动WebDAV。如果启动，回调才会在线程中被调用；
// 但是如果启动失败（比如很早期的检查阶段），根本就不会走到线程中去。而是在主线程就
// 直接回调回去了，这时候回调函数仍然以为自己处理其它线程中（函数肯定明确说了是在其它线程中调用的回调），
// 然后就会调用这个Async，但是此时确实是在主线程，并且尝试往app.system队列塞异步回调事件，就会直接死锁。
//
// 为了缓和这个现象（或者说错误的编写方式），我直接在线程中投递消息了，
// 这下就永远也不会死锁了。
func (app *App) Async(callback func()) {
	go func() {
		app.system <- &Event{
			Type:          asyncCallback,
			asyncCallback: callback,
		}
	}()
}

// 投递退出事件，app.Run() 结束运行。
func (app *App) Quit() {
	go func() {
		app.system <- &Event{
			Type: QuitEvent,
		}
	}()
}

func (app *App) Run() {
	menuPressed := false
	startPressed := false
	pollEvents(app.ctx, app.system, app.sync, func(event *Event) {
		switch event.Type {
		case QuitEvent:
			app.cancel()
			return
		case StickDownEvent, StickUpEvent:
			if app.detached > 0 {
				return
			}

			// 按“菜单”和“开始”可以退出。
			// 暂时固定给所有APP。
			switch event.Stick.Name {
			case Menu:
				menuPressed = event.Type == StickDownEvent
			case Start:
				startPressed = event.Type == StickDownEvent
			}
			if menuPressed && startPressed {
				app.cancel()
				return
			}

			// 只发送给前台文档。
			// TODO 除非有系统级事件监听器？
			for _, doc := range slices.Backward(app.documents) {
				if !doc.display {
					continue
				}
				doc.handleEvent(event)
				break
			}
		}
	})
}

func (app *App) AddFont(family string, bold, italic bool, fsys fs.FS, path string) error {
	if err := app.fonts.AddFont(fsys, path, family, bold, italic); err != nil {
		log.Println(`字体添加失败:`, err)
		return err
	}
	return nil
}

// 脱离当前与操作系统的事件交互，比如屏幕、键盘。
// 需要在主线程中调用。
// 用于Linux系统独占，MacOS无效。
//
// Attach和Detach必须成对调用。
func (app *App) Detach() {
	app.detached++
}

func (app *App) DetachAsync() {
	app.Async(func() {
		app.Detach()
	})
}

// 重新夺取操作系统事件交互，比如屏幕、键盘。
// 需要在主线程中调用。
// 用于Linux系统独占，MacOS无效。
//
// Attach(Async)和Detach(Async)必须成对调用。
func (app *App) Attach() {
	app.detached--
	if app.detached < 0 {
		panic(`Attach后为负数`)
	}
	if app.detached == 0 {
		app.Dirty()
	}
}

func (app *App) AttachAsync() {
	app.Async(func() {
		app.Attach()
	})
}

// 真正执行检测是否需要重新布局或重绘的地方。
func (app *App) sync() {
	if app.detached > 0 {
		return
	}

	hasDirtyDocument := false
	for _, doc := range app.documents {
		if doc.display && doc.dirty() {
			hasDirtyDocument = true
			break
		}
	}
	if !app.dirty && !hasDirtyDocument {
		return
	}

	app.canvas.Clear()

	for _, doc := range app.documents {
		if !doc.display {
			continue
		}
		now := time.Now()
		doc.sync(app.canvas, true)
		log.Println(`帧绘制时长：`, time.Since(now).Round(time.Microsecond*100))
	}

	app.display.Sync()
	app.dirty = false
}
