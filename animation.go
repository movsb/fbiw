package fbiw

import (
	"sync/atomic"
	"time"
)

const animationFrameInterval = time.Second / 60

type _AnimationRequest struct {
	doc      *Document
	callback func(time.Time)
}

// 所有状态都由 UI 主线程管理。定时器回调只检查原子的取消标记，
// 并发送可合并的唤醒通知，不访问 UI 状态。
type _AnimationClock struct {
	requests []*_AnimationRequest
	now      func() time.Time
	after    func(time.Duration, func()) func()
	stop     func()
	deadline time.Time
	last     time.Time
	closed   bool
	running  bool
}

// RequestAnimationFrame 请求在下一次布局和绘制前执行一次回调。
// 持续动画需要在回调中再次申请。同一帧的所有回调收到相同的时间戳。
//
// 注册和取消必须在主线程执行；其他线程应通过 App.Async 投递。
// 取消可重复调用，关闭文档会取消其全部请求。文档切到后台或 App Detach 时
// 暂停回调，但时间继续流逝。回调改变显示状态后，需要显式请求布局或重绘。
func (doc *Document) RequestAnimationFrame(callback func(now time.Time)) (cancel func()) {
	if callback == nil || doc.app == nil || doc.app.ctx.Err() != nil {
		panic("RequestAnimationFrame: 无效回调、文档。")
	}

	app := doc.app
	c := &app.animation
	if c.closed {
		panic("RequestAnimationFrame: 时钟已停摆。")
	}

	if c.now == nil {
		c.now = time.Now
	}
	if c.after == nil {
		c.after = func(d time.Duration, f func()) func() {
			t := time.AfterFunc(d, f)
			return func() { t.Stop() }
		}
	}
	r := &_AnimationRequest{doc: doc, callback: callback}
	c.requests = append(c.requests, r)
	app.reconcileAnimation()
	return func() {
		r.doc, r.callback = nil, nil
		app.reconcileAnimation()
	}
}

// 判断文档的动画是否应该执行。
func (app *App) animationRunnable(doc *Document) bool {
	return doc != nil && doc.app == app && app.detached == 0 &&
		(doc == app.overlay || doc.desktop != nil && app.isActiveDesktop(doc.desktop))
}

// 把动画时钟当前安排的定时唤醒撤掉。
//
// 它不取消动画请求，也不关闭整个时钟。
//
//   - 开始执行一帧时，撤掉本次唤醒安排。
//   - 没有可运行请求时，例如切到后台，停止定时唤醒。
//   - 关闭时钟时，清理定时器。
func (c *_AnimationClock) disarm() {
	if c.stop != nil {
		// 标记旧回调失效，并停止定时器
		c.stop()
		c.stop = nil
	}
	// // 清空这次唤醒的截止时间
	c.deadline = time.Time{}
}

// 彻底停摆，准备退出。
func (c *_AnimationClock) close() {
	c.closed = true
	c.disarm()
	for _, r := range c.requests {
		r.doc, r.callback = nil, nil
	}
	c.requests = nil
}

func (app *App) cancelDocumentAnimation(doc *Document) {
	for _, r := range app.animation.requests {
		if r.doc == doc {
			r.doc, r.callback = nil, nil
		}
	}
	app.reconcileAnimation()
}

// 根据当前状态，决定动画定时器该启动、保留还是停止。
// updateAnimationTimer
func (app *App) reconcileAnimation() {
	c := &app.animation

	// App 已退出 → 关闭动画时钟。
	if app.ctx.Err() != nil {
		c.close()
		return
	}

	// 时钟已关闭，或正在执行本帧回调 → 不处理。
	if c.closed || c.running {
		return
	}

	// 清理已执行、已取消的请求，同时检查有没有当前可运行的请求。
	active := false
	kept := c.requests[:0]
	for _, r := range c.requests {
		if r.callback != nil {
			kept = append(kept, r)
			active = active || app.animationRunnable(r.doc)
		}
	}
	clear(c.requests[len(kept):])
	c.requests = kept

	// 没有可运行请求 → disarm()，停止唤醒，保留后台请求。
	if !active {
		c.disarm()
		return
	}

	// 已有定时器 → 保留，不重复安排。
	if c.stop != nil {
		return
	}

	// 按上一帧开始时间加帧间隔，安排一次定时唤醒。
	now := c.now()
	c.deadline = c.last.Add(animationFrameInterval)
	if c.last.IsZero() {
		c.deadline = now.Add(animationFrameInterval)
	}
	var canceled atomic.Bool
	stop := c.after(max(0, c.deadline.Sub(now)), func() {
		if !canceled.Load() {
			app.wakeUp()
		}
	})
	c.stop = func() {
		canceled.Store(true)
		stop()
	}
}

func (app *App) runAnimationFrame() {
	app.reconcileAnimation()
	c := &app.animation
	if c.closed || c.stop == nil || c.running {
		return
	}
	now := c.now()
	if now.Before(c.deadline) {
		return
	}
	c.disarm()
	c.last = now
	c.running = true
	defer func() { c.running = false }()
	// 执行期间仍将请求保留在时钟中，保证关闭文档时，
	// 已进入本帧快照但尚未执行的回调也能被取消。
	batch := append([]*_AnimationRequest(nil), c.requests...)
	for _, r := range batch {
		if app.ctx.Err() != nil {
			c.close()
			return
		}
		if r.callback == nil || !app.animationRunnable(r.doc) {
			continue
		}
		callback := r.callback
		r.doc, r.callback = nil, nil
		callback(now)
	}
}
