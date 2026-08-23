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

	// 系统覆盖层，始终覆盖在所有文档之上。
	// 可以为空。
	// 参考: [SetOverlay]。
	overlay        *Document
	safeInsets     _AppInset
	overlayChanged bool

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
	return app._New(fsys, name, true)
}

// 创建新的文档，但是不添加到当前显示桌面。
func (app *App) NewOverlay(fsys fs.FS, name string) *Document {
	return app._New(fsys, name, false)
}

func (app *App) _New(fsys fs.FS, name string, addToDesktop bool) *Document {
	doc := _NewDocument(
		app.canvas.width, app.canvas.height,
		fsys, app.fonts, app.images,
	)

	// safe-area要求的，暂时提前到这里。
	doc.app = app

	if err := doc.load(name); err != nil {
		panic(err)
	}

	doc.display = false
	// 默认把焦点设置给根元素。
	doc.root.Activate()

	if addToDesktop {
		// 追加到后面（最上层窗口）
		// 但是由于没有显示，不需要放触发事件。
		// 默认不显示还有意义吗？
		app.documents = append(app.documents, doc)
	}

	return doc
}

func (app *App) _CloseDocument(doc *Document) {
	app.documents = slices.DeleteFunc(app.documents, func(d *Document) bool {
		return d == doc
	})

	if app.overlay == doc {
		app.SetOverlay(nil)
	}

	app.Dirty()

	app.Dispatch(DocChange, DocChangeArgs{Doc: app.topDoc()})
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
	if doc.app != app {
		panic(`不属于此App的文档。`)
	}

	if len(show) > 0 {
		doc.display = show[0]
	} else {
		doc.display = true
	}
	if doc.display && app.topDoc() == doc {
		app.Dispatch(DocChange, DocChangeArgs{Doc: doc})
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

	overlayDirty := app.overlay != nil && app.overlay.dirty()
	forceLayout := app.overlayChanged || app.overlay != nil && app.overlay.layoutDirty

	if !app.dirty && !hasDirtyDocument && !overlayDirty {
		return
	}

	// 0. 预备阶段
	app.canvas.Clear()

	// 1. 布局覆盖层，以计算安全区域。
	app.layoutOverlay()

	// 2. 画普通文档。
	for _, doc := range app.documents {
		if !doc.display {
			continue
		}
		now := time.Now()
		doc.sync(app.canvas, forceLayout, true)
		log.Println(`帧绘制时长：`, time.Since(now).Round(time.Microsecond*100))
	}

	// 3. 画系统覆盖层。
	if overlay := app.overlay; overlay != nil {
		overlay.sync(app.canvas, false, true)
	}

	app.display.Sync()
	app.dirty = false
	app.overlayChanged = false
}

// 设置系统覆盖层（状态栏）。
//
// 文档规范：
//
//   - #top/right/bottom/left 盒子（的高度或宽度）分别表示上/右/下/左的占用区域。
//
// <safe-area> 元素会根据这些区域设置安全内边距。
//
//   - 首次设置 overlay：强制布局 overlay，并重排普通文档。
//   - overlay 尺寸变化：layoutDirty 会触发普通文档重排。
//   - 移除 overlay：overlayChanged 使普通文档重新布局，inset 归零。
//   - 重新挂载 clean overlay：SetOverlay() 主动设置 layoutDirty，尺寸会重新读取。
//   - overlay 只布局一次：layoutOverlay() 清理 layoutDirty，后续 sync() 不会重复布局。
//   - overlayChanged 在帧结束后正确清零，不会导致每帧重排。
//   - overlay.Close() 和跨 App 文档检查都正常。
//
// 剩余的是非阻塞项：
//
//   - SafeArea.SetProp() 仍然用 panic 拒绝 padding，而且 CSS padding 可以绕过检查后被覆盖。
//   - overlay 只要发生任何布局，即使四边尺寸没变，也会重排所有普通文档。状态栏规模下通常可以接受；需要优化时再比较前后 inset。
func (app *App) SetOverlay(doc *Document) {
	if doc != nil && doc.app != app {
		panic(`不属于此App的文档。`)
	}

	app.overlay = doc
	app.safeInsets = _AppInset{}

	if app.overlay != nil {
		app.safeInsets = _AppInset{
			topBox:    app.overlay.GetBoxByID[Box](`top`),
			rightBox:  app.overlay.GetBoxByID[Box](`right`),
			bottomBox: app.overlay.GetBoxByID[Box](`bottom`),
			leftBox:   app.overlay.GetBoxByID[Box](`left`),
		}
		app.overlay.layoutDirty = true
	}

	app.overlayChanged = true
	app.Dirty()
}

type _AppInset struct {
	topBox, rightBox, bottomBox, leftBox Box
	top, right, bottom, left             int
}

func (app *App) layoutOverlay() {
	if app.overlay == nil || !app.overlay.layoutDirty {
		return
	}

	app.overlay.layout()
	app.overlay.layoutDirty = false
	app.overlay.paintDirty = true

	if top := app.safeInsets.topBox; top != nil {
		app.safeInsets.top = top.GetLayoutBox().Height
	}
	if right := app.safeInsets.rightBox; right != nil {
		app.safeInsets.right = right.GetLayoutBox().Width
	}
	if bottom := app.safeInsets.bottomBox; bottom != nil {
		app.safeInsets.bottom = bottom.GetLayoutBox().Height
	}
	if left := app.safeInsets.leftBox; left != nil {
		app.safeInsets.left = left.GetLayoutBox().Width
	}
}

// 返回其它文档的安全可用区域。
//
// 注意：此为建议，不一定需要遵守。
// func (app *App) contentViewport() Rect {
// 	return Rect{
// 		X:      app.safeInsets.left,
// 		Y:      app.safeInsets.top,
// 		Width:  app.canvas.width - app.safeInsets.left - app.safeInsets.right,
// 		Height: app.canvas.height - app.safeInsets.top - app.safeInsets.bottom,
// 	}
// }

type SafeArea struct {
	BaseBox

	app *App
}

var _ Box = (*SafeArea)(nil)

func _NewSafeArea(doc *Document) *SafeArea {
	if doc.app == nil {
		panic(`使用SafeArea的文档必须已绑定App。`)
	}
	b := &SafeArea{
		BaseBox: NewBaseBox(doc, `safe-area`),
		app:     doc.app,
	}
	b.inlineStyles.Display = StringValue(`block`)
	return b
}

func init() {
	Define(`safe-area`, false, _NewSafeArea)
}

func (b *SafeArea) SetProp(key, value string) error {
	if key == `padding` {
		panic(`不能给SafeArea设置Padding。`)
	}
	return b.Base().SetProp(key, value)
}

func (b *SafeArea) Calc(availWidth, availHeight int, constraints Constraints) {
	insets := b.app.safeInsets
	b.computedStyles.Padding = PaddingValue(insets.top, insets.right, insets.bottom, insets.left)
	b.Base().Calc(availWidth, availHeight, constraints)
}

// func (b *SafeArea) Draw(canvas *Canvas) {
// 	b.Base().Draw(canvas)
// }
