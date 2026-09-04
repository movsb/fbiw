package fbiw

import (
	"container/list"
	"context"
	"io/fs"
	"iter"
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

	fpsCalc   _FPSCounter
	animation *_AnimationClock

	images       *ImageManager
	fonts        *FontManager
	themeManager *ThemeManager

	// 桌面列表。
	// 桌面由文档构成。
	// 前台桌面是 Front() 元素。
	desktops list.List
	// // 桌面切换器。
	// switcher         func(app *App) *Document
	// switcherDocument *Document

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
		canvas:  NewCanvas(display.Width, display.Height),
		images:  NewImageManager(),
		fonts:   NewFontManager(),

		// 容量一定为1，见前面定义时的说明。
		unblock: make(chan struct{}, 1),
	}

	app.themeManager = newThemeManager(app)

	for _, opt := range options {
		opt(app)
	}

	if app.ctx == nil {
		app.ctx = context.Background()
	}

	ctx, cancel := context.WithCancel(app.ctx)
	app.ctx = ctx
	app.cancel = cancel
	app.animation = newAnimationClock(app.ctx, app.animationRunnable, app.wakeUp)

	// 未初始化任何内容，也不应该使用它。
	app._EventTarget.box = &BaseBox{}

	// 不要放前面，里面引用了 app.ctx。
	app.themeManager.start()

	return app
}

func (app *App) Close() {
	defer app.display.Close()
	defer app.images.Close()
	defer app.fonts.Close()
	defer app.animation.close()
	app.themeManager.close()
	app.cancel()
}

func (app *App) Context() context.Context {
	return app.ctx
}

// 创建新的文档、新的桌面。
// 创建后立即切换到此桌面。
func (app *App) NewDesktop(fsys fs.FS, name string) *Document {
	return app._New(fsys, name, _AppNewDocDesktopNew, nil)
}

// 创建新的文档、添加到docRef的桌面。
func (app *App) NewPopup(fsys fs.FS, name string, opener *Document) *Document {
	if opener == nil || opener.app != app || opener.desktop == nil {
		panic(`无效文档桌面。`)
	}
	return app._New(fsys, name, _AppNewDocDesktopRef, opener)
}

// 创建新的系统覆盖层。
func (app *App) NewOverlay(fsys fs.FS, name string) *Document {
	return app._New(fsys, name, _AppNewDocDesktopOverlay, nil)
}

type _AppNewDocDesktop uint8

const (
	_AppNewDocDesktopRef _AppNewDocDesktop = iota
	_AppNewDocDesktopNew
	_AppNewDocDesktopOverlay
)

func (app *App) _New(fsys fs.FS, name string, desktop _AppNewDocDesktop, docRef *Document) *Document {
	doc := _NewDocument(
		app.canvas.width, app.canvas.height,
		fsys, app.fonts, app.images,
	)

	// safe-area要求的，暂时提前到这里。
	doc.bindApp(app)

	if err := doc.load(name); err != nil {
		doc.unbindApp()
		panic(err)
	}

	// 默认把焦点设置给根元素。
	doc.root.Activate()

	switch desktop {
	case _AppNewDocDesktopRef:
		// 第一文档创建时还没有桌面。
		var cur *Desktop
		if app.desktops.Len() == 0 {
			cur = &Desktop{app: app}
			app.desktops.PushFront(cur)
		} else {
			cur = docRef.desktop
		}
		cur.add(doc)
		if app.isActiveDesktop(cur) {
			app.Dispatch(DocChange, DocChangeArgs{Doc: doc})
			app.Dirty()
		}
	case _AppNewDocDesktopNew:
		top := &Desktop{app: app}
		top.add(doc)
		app.desktops.PushFront(top)
		app.Dispatch(DocChange, DocChangeArgs{Doc: doc})
		app.Dirty()
	case _AppNewDocDesktopOverlay:
		// 不添加到任何桌面。
	}

	return doc
}

func (app *App) isActiveDesktop(d *Desktop) bool {
	if front := app.desktops.Front(); front != nil {
		return d == front.Value.(*Desktop)
	}
	return false
}

// 判断文档的动画是否应该执行，时钟本身不解释桌面和覆盖层状态。
func (app *App) animationRunnable(doc *Document) bool {
	return doc != nil && doc.app == app && app.detached == 0 &&
		(doc == app.overlay || doc.desktop != nil && app.isActiveDesktop(doc.desktop))
}

func (app *App) _CloseDocument(doc *Document) {
	app.animation.cancelDocument(doc)
	if app.overlay == doc {
		app.SetOverlay(nil)
		doc.unbindApp()
		return
	}
	// if app.switcherDocument == doc {
	// 	app.switcherDocument = nil
	// 	doc.app = nil
	// 	app.Dirty()
	// 	return
	// }

	// multiple close ? overlay & switcher?
	if doc.desktop == nil {
		doc.unbindApp()
		return
	}

	desktop := doc.desktop
	wasActive := app.isActiveDesktop(desktop)
	isTop := doc == desktop.top()
	doc.desktop.remove(doc)

	if desktop.count() <= 0 {
		for e := app.desktops.Front(); e != nil; e = e.Next() {
			if e.Value.(*Desktop) == desktop {
				desktop.app = nil
				app.desktops.Remove(e)
				break
			}
		}
	}

	// 只有前台桌面的顶层文档被移除时，当前文档才会变化。
	if wasActive {
		if isTop {
			var top *Document
			if front := app.desktops.Front(); front != nil {
				top = front.Value.(*Desktop).top()
			}
			app.Dispatch(DocChange, DocChangeArgs{Doc: top})
		}
		app.Dirty()
	}

	if app.desktops.Len() <= 0 {
		app.Quit()
	}
}

// 同步标记为脏，异步等待下次刷新。
//
// 只起标记作用，文档是否需要重绘还要看文档本身。
//
// 文档dirty不要调用这个，因为文档不一定属于前台桌面，不一定需要更新。
func (app *App) Dirty() {
	app.dirty = true
	app.animation.updateTimer()
	app.wakeUp()
}

func (app *App) docDirty(doc *Document) {
	if doc.app != app {
		panic(`非此App的文档。`)
	}
	// overlay?
	if doc.desktop == nil {
		app.Dirty()
		return
	}
	if app.desktops.Len() > 0 {
		front := app.desktops.Front().Value.(*Desktop)
		if doc.desktop == front {
			app.Dirty()
			return
		}
	}
}

// 把文档设置为显示状态。
//
// 显示后键盘事件发发送到这里。
// func (app *App) Show(doc *Document, show ...bool) {
// 	if doc.app != app {
// 		panic(`不属于此App的文档。`)
// 	}

// 	if len(show) > 0 {
// 		doc.display = show[0]
// 	} else {
// 		doc.display = true
// 	}
// 	if doc.display && app.topDoc() == doc {
// 		app.Dispatch(DocChange, DocChangeArgs{Doc: doc})
// 	}
// 	doc.RequestPaint()
// }

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
// 特别注意：回调只被保证在主线程中调用，但是如果Async是被文档调用，
// 回调被调用时文档不一定还“活着”（有可能已经被关闭了），如果此时操作
// UI界面，很有可能炸掉。所以：此函数应该非常小心地被调用。
//
// 文档（Document）那边提供了一个相同签名的方法，但是它会将回调绑定到
// 文档的生命周期上。即：回调时如果文档已关闭等，则回调函数不会被调用。
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

				// 桌面切换。
				// if event.Type == StickDownEvent && event.Stick.Name == Select && app.switcher != nil {
				// 	if app.switcherDocument != nil {
				// 		app.switcherDocument.Close()
				// 		app.switcherDocument = nil
				// 		return
				// 	}
				// 	app.switcherDocument = app.switcher(app)
				// 	return
				// }
				// if app.switcherDocument != nil {
				// 	// 关闭后会清空app，以此来判断切换器已关闭。
				// 	if app.switcherDocument.app != nil {
				// 		app.switcherDocument.handleEvent(event)
				// 		return
				// 	}
				// 	app.switcherDocument = nil
				// 	// fallthrough
				// }
				if event.Type == StickDownEvent && event.Stick.Name == Select {
					if app.desktops.Len() > 1 {
						app.desktops.MoveToBack(app.desktops.Front())
						top := app.desktops.Front().Value.(*Desktop)
						app.Dispatch(DocChange, DocChangeArgs{Doc: top.top()})
						app.Dirty()
						return
					}
				}

				// 只发送给前台文档。
				// TODO 除非有系统级事件监听器？
				// TODO 其实这两个地方都不应该判断，理论不可能为空。
				if e := app.desktops.Front(); e != nil {
					desktop := e.Value.(*Desktop)
					if top := desktop.top(); top != nil {
						top.handleEvent(event)
					}
				}
			}
		},
	)
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
	app.animation.updateTimer()
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

type _FPSCounter struct {
	start  time.Time
	frames int
}

func (f *_FPSCounter) Frame() {
	if f.start.IsZero() {
		f.start = time.Now()
	}

	f.frames++
	elapsed := time.Since(f.start)
	if elapsed >= time.Second {
		fps := float64(f.frames) / elapsed.Seconds()
		f.frames = 0
		f.start = time.Now()
		log.Printf("帧率: %.1f", fps)
	}
}

// 真正执行检测是否需要重新布局或重绘的地方。
func (app *App) sync() {
	defer app.animation.updateTimer()
	app.animation.tick()

	if app.ctx.Err() != nil {
		return
	}

	if app.detached > 0 {
		return
	}

	hasDirtyDocument := false

	// 有可能只创建了overlay就开始运行，此时还没有桌面。
	var desktop *Desktop
	if app.desktops.Len() > 0 {
		desktop = app.desktops.Front().Value.(*Desktop)
		for doc := range desktop.All() {
			if doc.dirty() {
				hasDirtyDocument = true
				break
			}
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
	if desktop != nil {
		for doc := range desktop.All() {
			now := time.Now()
			doc.sync(app.canvas, forceLayout, true)
			log.Println(`帧绘制时长：`, doc.name, time.Since(now).Round(time.Microsecond*100))
		}
	}

	// 3. 画桌面切换器
	// if switcher := app.switcherDocument; switcher != nil {
	// 	switcher.sync(app.canvas, false, true)
	// }

	// 4. 画系统覆盖层。
	if overlay := app.overlay; overlay != nil {
		overlay.sync(app.canvas, false, true)
	}

	app.display.Sync(app.canvas.buffer)
	app.dirty = false
	app.overlayChanged = false

	// 统计帧率
	// 意义不大，macOS WindowServer 会锁帧。
	// app.fpsCalc.Frame()
}

// 多桌面空间支持。
type Desktop struct {
	app *App
	// 层叠的窗口列表。
	// 上面的在后面。
	documents []*Document
}

// 取第一个可见、有标题的文档的为名字。
func (d *Desktop) Name() string {
	name := ``
	for _, doc := range slices.Backward(d.documents) {
		if doc.Title() == `` {
			continue
		}
		name = doc.title
		break
	}
	return name
}
func (d *Desktop) add(doc *Document) {
	d.documents = append(d.documents, doc)
	doc.desktop = d
}
func (d *Desktop) remove(doc *Document) {
	d.documents = slices.DeleteFunc(d.documents, func(d *Document) bool {
		return d == doc
	})
	doc.unbindApp()
	doc.desktop = nil
}
func (d *Desktop) top() *Document {
	if len(d.documents) > 0 {
		return d.documents[len(d.documents)-1]
	}
	return nil
}
func (d *Desktop) count() int {
	return len(d.documents)
}

// 从下往上遍历。
func (d *Desktop) All() iter.Seq[*Document] {
	documents := slices.Clone(d.documents)
	return func(yield func(*Document) bool) {
		for _, doc := range documents {
			if !yield(doc) {
				break
			}
		}
	}
}

// 返回本App关联所有的文档（含Overlays）。
func (app *App) allDocuments() iter.Seq[*Document] {
	return func(yield func(*Document) bool) {
		for desktop := range app.Desktops() {
			for doc := range desktop.All() {
				if !yield(doc) {
					return
				}
			}
		}
		if app.overlay != nil {
			if !yield(app.overlay) {
				return
			}
		}
	}
}

// 遍历所有的桌面。
func (app *App) Desktops() iter.Seq[*Desktop] {
	desktops := make([]*Desktop, 0, app.desktops.Len())
	for e := app.desktops.Front(); e != nil; e = e.Next() {
		desktops = append(desktops, e.Value.(*Desktop))
	}
	return func(yield func(*Desktop) bool) {
		for _, desktop := range desktops {
			if !yield(desktop) {
				break
			}
		}
	}
}

// 切换到指定的桌面。
func (app *App) SwitchTo(desktop *Desktop) {
	// 从 Desktops() 拿到的列表是快照。
	// 期间可能由于文档主动关闭后被删除了。
	// 再切换就会失败。
	if desktop == nil || desktop.app == nil || desktop.app != app {
		return
	}

	// 已经是前台。
	if desktop == app.desktops.Front().Value.(*Desktop) {
		return
	}

	// 移动到前台。
	for e := app.desktops.Front(); e != nil; e = e.Next() {
		if d := e.Value.(*Desktop); d == desktop {
			app.desktops.MoveToFront(e)
			break
		}
	}

	// 通知前台文档变化。
	top := app.desktops.Front().Value.(*Desktop).top()
	app.Dispatch(DocChange, DocChangeArgs{Doc: top})

	app.Dirty()
}

// 设置桌面切换器。
// func (app *App) SetSwitcher(callback func(app *App) *Document) {
// 	app.switcher = callback
// }

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
	if doc != nil && (doc.app != app || doc.desktop != nil) {
		panic(`无效Overlay文档。`)
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
	if !displaying(b) {
		return
	}
	insets := b.app.safeInsets
	b.computedStyles.SetPadding(PaddingValue(insets.top, insets.right, insets.bottom, insets.left))
	blockCalc(&b.BaseBox, availWidth, availHeight, constraints)
}
