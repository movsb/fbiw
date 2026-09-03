package fbiw

import (
	"context"
	"math"
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
	// 调用方提供生命周期、文档的可运行性判断和唤醒入口。
	ctx      context.Context
	runnable func(*Document) bool
	wake     func()

	requests []*_AnimationRequest
	now      func() time.Time
	after    func(time.Duration, func()) func()
	stop     func()
	deadline time.Time
	last     time.Time
	closed   bool
	running  bool
}

func newAnimationClock(ctx context.Context, runnable func(*Document) bool, wake func()) *_AnimationClock {
	return &_AnimationClock{
		ctx:      ctx,
		runnable: runnable,
		wake:     wake,
		now:      time.Now,
		after: func(d time.Duration, f func()) func() {
			t := time.AfterFunc(d, f)
			return func() { t.Stop() }
		},
	}
}

// 注册一次帧回调，返回可重复调用的取消函数。
func (c *_AnimationClock) request(doc *Document, callback func(time.Time)) func() {
	if c.closed {
		panic("动画时钟已停摆。")
	}

	r := &_AnimationRequest{doc: doc, callback: callback}
	c.requests = append(c.requests, r)
	c.updateTimer()
	return func() {
		r.doc, r.callback = nil, nil
		c.updateTimer()
	}
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
	// 清空这次唤醒的截止时间。
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

// 取消文档的所有请求，包括已进入当前帧快照但尚未执行的请求。
func (c *_AnimationClock) cancelDocument(doc *Document) {
	for _, r := range c.requests {
		if r.doc == doc {
			r.doc, r.callback = nil, nil
		}
	}
	c.updateTimer()
}

// 根据当前状态，决定动画定时器该启动、保留还是停止。
func (c *_AnimationClock) updateTimer() {
	// 生命周期已结束 → 关闭动画时钟。
	if c.ctx.Err() != nil {
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
			active = active || c.runnable(r.doc)
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
			c.wake()
		}
	})
	c.stop = func() {
		canceled.Store(true)
		stop()
	}
}

// 执行到期帧。下一次定时安排由绘制结束后的 updateTimer 负责，
// 避免把绘制耗时叠加到帧间隔，也不补发错过的历史帧。
func (c *_AnimationClock) tick() {
	c.updateTimer()
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
	// 执行期间仍将请求保留在时钟中，保证按文档取消请求时，
	// 已进入本帧快照但尚未执行的回调也能被取消。
	batch := append([]*_AnimationRequest(nil), c.requests...)
	for _, r := range batch {
		if c.ctx.Err() != nil {
			c.close()
			return
		}
		if r.callback == nil || !c.runnable(r.doc) {
			continue
		}
		callback := r.callback
		r.doc, r.callback = nil, nil
		callback(now)
	}
}

// Easing 决定补间动画的变化节奏。这里使用二次曲线，
// 不等同于 CSS 同名关键字对应的三次贝塞尔曲线。
type Easing uint8

const (
	EaseLinear Easing = iota // 匀速，也是默认值。
	EaseIn                   // 开始慢，随后加速。
	EaseOut                  // 开始快，随后减速。
	EaseInOut                // 先加速，再减速。
)

func (e Easing) apply(t float64) float64 {
	switch e {
	case EaseIn:
		return t * t
	case EaseOut:
		return t * (2 - t)
	case EaseInOut:
		if t < 0.5 {
			return 2 * t * t
		}
		return 1 - 2*(1-t)*(1-t)
	default:
		return t
	}
}

// TweenOptions 描述一次数值补间。From、To 必须是有限数值，
// Duration 不能为负；零时长在下一帧直接更新为 To。
type TweenOptions struct {
	From, To float64
	Duration time.Duration
	Easing   Easing

	// OnUpdate 接收当前值，不能为空。布局和重绘仍由调用者按需请求。
	OnUpdate func(value float64)
	// OnComplete 只在自然完成时调用一次，在最后一次 OnUpdate 之后执行。
	// 主动取消、关闭文档或退出 App 都不会触发它。
	OnComplete func()
}

// value 只计算当前时刻的值，不修改样式，也不参与帧调度。
func (o TweenOptions) value(elapsed time.Duration) (value float64, complete bool) {
	if elapsed >= o.Duration {
		return o.To, true
	}
	if elapsed <= 0 {
		return o.From, false
	}
	p := o.Easing.apply(float64(elapsed) / float64(o.Duration))
	// 使用加权和，避免有限但符号相反的端点相减后溢出。
	return (1-p)*o.From + p*o.To, false
}

// Tween 从调用时开始计时，通过统一帧时钟更新数值，返回可重复调用的取消函数。
// 不会同步调用 OnUpdate，也不保证首帧恰好为 From；需要立即显示起点时由调用者设置。
//
// 注册、取消和回调均在 UI 主线程执行。后台暂停回调但不暂停时间，
// 恢复时直接追上当前进度。完成时精确交付 To，不补发错过的中间帧。
// 同一属性的新动画不会自动替换旧动画；调用者应先取消旧动画，
// 再以当前显示值为 From 创建新动画。
func (doc *Document) Tween(options TweenOptions) (cancel func()) {
	if options.OnUpdate == nil || options.Duration < 0 || options.Easing > EaseInOut ||
		math.IsNaN(options.From) || math.IsInf(options.From, 0) ||
		math.IsNaN(options.To) || math.IsInf(options.To, 0) {
		panic("Tween: 无效的回调、时长、缓动或端点。")
	}
	if doc.app == nil || doc.app.ctx.Err() != nil || doc.app.animation.closed {
		panic("Tween: 文档未绑定到运行中的动画时钟。")
	}
	app := doc.app
	clock := app.animation
	start := clock.now()
	if clock.running {
		// 帧回调中创建的动画以本帧统一时间戳为起点。
		start = clock.last
	}
	stopped := false
	var cancelFrame func()
	cancel = func() {
		if stopped {
			return
		}
		stopped = true
		if cancelFrame != nil {
			cancelFrame()
			cancelFrame = nil
		}
		options.OnUpdate, options.OnComplete = nil, nil
	}
	live := func() bool {
		return !stopped && doc.app == app && app.ctx.Err() == nil && !clock.closed
	}
	var frame func(time.Time)
	frame = func(now time.Time) {
		cancelFrame = nil
		if !live() {
			cancel()
			return
		}
		value, complete := options.value(now.Sub(start))
		options.OnUpdate(value)
		// 用户回调可能取消动画、关闭文档或退出 App，不能再续订下一帧。
		if !live() {
			cancel()
			return
		}
		if complete {
			onComplete := options.OnComplete
			cancel()
			if onComplete != nil {
				onComplete()
			}
			return
		}
		cancelFrame = doc.RequestAnimationFrame(frame)
	}
	cancelFrame = doc.RequestAnimationFrame(frame)
	return cancel
}
