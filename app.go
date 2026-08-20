package fbiw

import (
	"context"
	"io/fs"
	"log"
	"slices"
	"sync"
	"time"
)

type Option func(app *App)

func WithContext(ctx context.Context) Option {
	return func(app *App) {
		app.ctx = ctx
	}
}

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

	// 事件合并机制。
	// 来自主线程和其它线程的消息只管往这里面塞，
	// 塞完了只是尝试往容量只有1的队列里面写。
	// 能写进去则说明主消息循环还未被唤醒，写不进去
	// 则说明队列里面已经有阻塞的消息等待处理了，不需要
	// 多次唤醒，直接drop即可。
	lock    sync.Mutex
	pending []func()
	unblock chan struct{}

	display *Display
	canvas  *Canvas
	images  *ImageManager
	fonts   *FontManager

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

	// 假装是一个事件目标？比如文档切换？
	// 但是由于其内部有一个box（且不能为空），
	// 我只能填一个空的base了，暂时。
	// 其实按照web的定义，这个box应该是any，
	// 但是由于我这里非box的场景极少，所以暂时定义成box了。
	_EventTarget
}

func NewApp(options ...Option) *App {
	display := openDisplay()

	app := &App{
		display: display,
		canvas:  NewCanvas(display),
		images:  NewImageManager(),
		fonts:   NewFontManager(),

		// 容量一定为1，见前面定义时的说明。
		unblock: make(chan struct{}, 1),
	}

	for _, opt := range options {
		opt(app)
	}

	if app.ctx == nil {
		app.ctx = context.Background()
	}

	ctx, cancel := context.WithCancel(app.ctx)
	app.ctx = ctx
	app.cancel = cancel

	// 未初始化任何内容，也不应该使用它。
	app._EventTarget.box = &BaseBox{}

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
func (app *App) New(fsys fs.FS, name string) *Document {
	doc := _NewDocument(
		app.canvas.width, app.canvas.height,
		fsys, app.fonts, app.images,
	)
	if err := doc.load(name); err != nil {
		panic(err)
	}
	doc.display = false
	doc.app = app
	// 默认把焦点设置给根元素。
	doc.root.Activate()
	// 追加到后面（最上层窗口）
	// 但是由于没有显示，不需要放触发事件。
	// 默认不显示还有意义吗？
	app.documents = append(app.documents, doc)
	return doc
}

func (app *App) _CloseDocument(doc *Document) {
	app.documents = slices.DeleteFunc(app.documents, func(d *Document) bool {
		return d == doc
	})
	app.Dirty()

	app.Dispatch(&Event{
		Type:      DocChange,
		DocChange: DocChangeArgs{Doc: app.topDoc()},
	})
}

// 同步标记为脏，异步等待下次刷新。
//
// 只起标记作用，文档是否需要重绘还要看文档本身。
func (app *App) Dirty() {
	app.dirty = true
	app.wakeUp()
}

func (app *App) topDoc() *Document {
	for _, doc := range slices.Backward(app.documents) {
		if !doc.display {
			continue
		}
		return doc
	}
	return nil
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
	if doc.display && app.topDoc() == doc {
		app.Dispatch(&Event{
			Type: DocChange,
			DocChange: DocChangeArgs{
				Doc: doc,
			},
		})
	}
	doc.RequestPaint()
}

// 唤醒消息循环以处理挂起的异步调用和脏处理过程。
// 写不进去说明有积压的事件等待处理，可以安全丢弃事件。
func (app *App) wakeUp() {
	select {
	case app.unblock <- struct{}{}:
	default:
	}
}

// 用于其它线程创建一个将来会在主线程中调用的回调函数。
//
// 方便用于在非主线程中安全更新UI操作。
// 调用会立即返回，不会阻塞。
// 每次回调都会额外触发检测是否有绘制更新。
func (app *App) Async(callback func()) {
	app.lock.Lock()
	app.pending = append(app.pending, callback)
	app.lock.Unlock()
	app.wakeUp()
}

// 使 app.Run() 结束运行。
func (app *App) Quit() {
	app.cancel()
}

func (app *App) Run() {
	menuPressed := false
	startPressed := false
	pollEvents(
		app.ctx, app.cancel,
		app.unblock,
		func() {
			app.lock.Lock()
			pending := app.pending
			app.pending = nil
			app.lock.Unlock()
			for _, callback := range pending {
				callback()
			}
		},
		app.sync,
		func(event *Event) {
			switch event.Type {
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
