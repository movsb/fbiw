package fbiw

import (
	"slices"
	"testing"
	"time"
)

type fakeAnimationTimer struct {
	due     time.Time
	fire    func()
	stopped bool
}

type fakeAnimationTime struct {
	now    time.Time
	timers []*fakeAnimationTimer
}

func newAnimationTestApp(t *testing.T) (*App, *Document, *fakeAnimationTime) {
	t.Helper()
	app := newDesktopTestApp()
	t.Cleanup(func() { app.cancel(); app.animation.close() })
	desktop := &Desktop{app: app}
	app.desktops.PushFront(desktop)
	doc := addDesktopTestDocument(app, desktop)
	f := &fakeAnimationTime{now: time.Now()}
	app.animation.now = func() time.Time { return f.now }
	app.animation.after = func(d time.Duration, callback func()) func() {
		timer := &fakeAnimationTimer{due: f.now.Add(d), fire: callback}
		f.timers = append(f.timers, timer)
		return func() { timer.stopped = true }
	}
	return app, doc, f
}

func animationStep(app *App) {
	app.runAnimationFrame()
	app.reconcileAnimation()
}

func TestAnimationFrameBatch(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	other := addDesktopTestDocument(app, doc.desktop)
	var order []int
	var times []time.Time
	doc.RequestAnimationFrame(func(now time.Time) {
		order = append(order, 1)
		times = append(times, now)
		doc.RequestAnimationFrame(func(time.Time) { order = append(order, 3) })
	})
	other.RequestAnimationFrame(func(now time.Time) { order = append(order, 2); times = append(times, now) })
	if len(f.timers) != 1 {
		t.Fatal("requests did not share one timer")
	}
	animationStep(app)
	if len(order) != 0 {
		t.Fatal("input wake-up bypassed deadline")
	}
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if !slices.Equal(order, []int{1, 2}) || times[0] != times[1] || times[0] != f.now {
		t.Fatalf("batch = %v, timestamps = %v", order, times)
	}
	animationStep(app)
	if len(order) != 2 {
		t.Fatal("renewal ran in same frame")
	}
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if !slices.Equal(order, []int{1, 2, 3}) || app.animation.stop != nil || len(app.animation.requests) != 0 {
		t.Fatal("one-shot requests did not drain")
	}
}

func TestAnimationCancellationAndClose(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	other := addDesktopTestDocument(app, doc.desktop)
	var cancel func()
	doc.RequestAnimationFrame(func(time.Time) { cancel(); cancel(); other.Close() })
	cancel = doc.RequestAnimationFrame(func(time.Time) { t.Fatal("canceled snapshot callback ran") })
	other.RequestAnimationFrame(func(time.Time) { t.Fatal("closed document callback ran") })
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if len(app.animation.requests) != 0 {
		t.Fatal("requests retained")
	}
}

func TestAnimationDropsMissedFrames(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	calls := 0
	var tick func(time.Time)
	tick = func(now time.Time) {
		calls++
		if now != f.now {
			t.Fatal("stale timestamp")
		}
		doc.RequestAnimationFrame(tick)
	}
	cancel := doc.RequestAnimationFrame(tick)
	f.now = f.now.Add(10 * time.Second)
	animationStep(app)
	cancel() // 原请求已执行，取消它不能影响新请求。
	animationStep(app)
	if calls != 1 {
		t.Fatal("missed frames replayed")
	}
	if app.animation.deadline != f.now.Add(animationFrameInterval) {
		t.Fatal("deadline not based on frame start")
	}
	f.now = f.now.Add(5 * time.Second) // 模拟绘制耗时过长或 UI 阻塞。
	animationStep(app)
	if calls != 2 {
		t.Fatal("slow frame did not produce exactly one new batch")
	}
}

func TestAnimationBackgroundAndDetach(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	background := &Desktop{app: app}
	app.desktops.PushBack(background)
	other := addDesktopTestDocument(app, background)
	var got time.Time
	other.RequestAnimationFrame(func(now time.Time) { got = now })
	if len(f.timers) != 0 {
		t.Fatal("background request scheduled a wake-up")
	}
	app.SwitchTo(background)
	if app.animation.stop == nil {
		t.Fatal("switch did not start clock")
	}
	app.Detach()
	app.Detach()
	if app.animation.stop != nil {
		t.Fatal("detach did not stop clock")
	}
	f.now = f.now.Add(time.Hour)
	app.Attach()
	animationStep(app)
	if !got.IsZero() {
		t.Fatal("nested detach ignored")
	}
	app.Attach()
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if got != f.now {
		t.Fatal("background time did not advance")
	}
	app.SwitchTo(doc.desktop)
}

func TestAnimationOverlayAndMidFrameSwitch(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	// SetOverlay 的安全区域查询允许找不到对应元素，这里提供空根节点即可。
	overlay := &Document{app: app}
	overlay.root = NewBlock(overlay)
	replacement := &Document{app: app}
	replacement.root = NewBlock(replacement)
	calls := 0
	overlay.RequestAnimationFrame(func(time.Time) { calls++ })
	if app.animation.stop != nil {
		t.Fatal("unmounted overlay scheduled")
	}
	app.SetOverlay(overlay)
	doc.RequestAnimationFrame(func(time.Time) { app.SetOverlay(replacement) })
	// 将覆盖层的请求重新注册到切换回调之后，验证执行前会重新检查是否可运行。
	app.cancelDocumentAnimation(overlay)
	overlay.RequestAnimationFrame(func(time.Time) { calls++ })
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if calls != 0 || app.animation.stop != nil {
		t.Fatal("unmounted overlay callback ran")
	}
	app.SetOverlay(overlay)
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if calls != 1 {
		t.Fatal("overlay pending request not resumed")
	}
}

func TestAnimationShutdownAndStaleWake(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	doc.RequestAnimationFrame(func(time.Time) { t.Fatal("shutdown callback ran") })
	old := f.timers[0]
	app.Quit()
	animationStep(app)
	old.fire() // 模拟与 timer.Stop 并发到期的旧回调。
	select {
	case <-app.unblock:
		t.Fatal("stale timer woke closed app")
	default:
	}
	if !app.animation.closed || len(app.animation.requests) != 0 {
		t.Fatal("shutdown retained requests")
	}
}

func TestAnimationTimerOnlyWakesUI(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	calls := 0
	cancel := doc.RequestAnimationFrame(func(time.Time) { calls++ })
	timer := f.timers[0]
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			timer.fire()
		}
	}()
	<-done
	if calls != 0 || len(app.pending) != 0 || len(app.unblock) != 1 {
		t.Fatal("timer executed UI work or failed to coalesce wake-ups")
	}
	<-app.unblock
	// 让取消操作与迟到的定时器回调并发执行。定时器 goroutine
	// 只能访问原子取消标记和唤醒通道。
	done = make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			timer.fire()
		}
	}()
	cancel()
	<-done
	if app.animation.stop != nil || !timer.stopped || len(app.animation.requests) != 0 {
		t.Fatal("cancellation retained timer or request")
	}
	select {
	case <-app.unblock:
	default:
	}
	timer.fire()
	if len(app.unblock) != 0 {
		t.Fatal("canceled timer still wakes UI")
	}
}

func TestAnimationSwitchDuringBatch(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	background := &Desktop{app: app}
	app.desktops.PushBack(background)
	addDesktopTestDocument(app, background)
	calls := 0
	doc.RequestAnimationFrame(func(time.Time) { app.SwitchTo(background) })
	doc.RequestAnimationFrame(func(time.Time) { calls++ })
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if calls != 0 || app.animation.stop != nil {
		t.Fatal("background callback executed or scheduled")
	}
	app.SwitchTo(doc.desktop)
	f.now = f.now.Add(time.Second)
	animationStep(app)
	if calls != 1 {
		t.Fatal("pending callback lost on desktop switch")
	}
}

func TestAnimationInvalidRequest(t *testing.T) {
	app, doc, _ := newAnimationTestApp(t)
	for _, fn := range []func(){
		func() { doc.RequestAnimationFrame(nil) },
		func() { (&Document{}).RequestAnimationFrame(func(time.Time) {}) },
		func() { doc.Close(); doc.RequestAnimationFrame(func(time.Time) {}) },
		func() { app.animation.close(); (&Document{app: app}).RequestAnimationFrame(func(time.Time) {}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			fn()
		}()
	}
}

type animationPaintBox struct {
	BaseBox
	paint  func()
	layout func()
}

func (b *animationPaintBox) Draw(*Canvas)               { b.paint() }
func (b *animationPaintBox) Calc(int, int, Constraints) { b.layout() }

func TestAnimationSyncPaintBatch(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	paints, syncs, state, layouts := 0, 0, 0, 0
	b := &animationPaintBox{BaseBox: NewBaseBox(doc, "block")}
	b.computedStyles.SetDisplay(true)
	b.paint = func() {
		paints++
		if state != 2 {
			t.Fatal("paint before all frame callbacks")
		}
	}
	b.layout = func() {
		layouts++
		if state != 2 {
			t.Fatal("layout before all frame callbacks")
		}
	}
	doc.root = b
	app.canvas = &Canvas{width: 1, height: 1, buffer: make([]byte, 4)}
	app.display = &Display{sync: func([]byte) { syncs++ }}
	doc.RequestAnimationFrame(func(time.Time) { state++; doc.RequestLayout() })
	doc.RequestAnimationFrame(func(time.Time) { state++; doc.RequestLayout() })
	f.now = f.now.Add(animationFrameInterval)
	app.sync()
	if paints != 1 || syncs != 1 || layouts != 1 {
		t.Fatalf("paint/sync = %d/%d", paints, syncs)
	}
	doc.RequestAnimationFrame(func(time.Time) {})
	f.now = f.now.Add(animationFrameInterval)
	app.sync()
	if paints != 1 || syncs != 1 {
		t.Fatal("clean callback caused paint")
	}
	doc.RequestAnimationFrame(func(time.Time) { doc.RequestPaint() })
	f.now = f.now.Add(animationFrameInterval)
	app.sync()
	if paints != 2 || syncs != 2 || layouts != 1 {
		t.Fatal("paint-only callback caused layout")
	}
}
