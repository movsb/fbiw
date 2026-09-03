package fbiw

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"
)

// 不创建 App，直接验证时钟通过注入的入口完成调度和生命周期管理。
func TestAnimationClockWithoutApp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doc := &Document{}
	visible, wakes, calls := false, 0, 0
	c := newAnimationClock(ctx, func(owner *Document) bool {
		if owner != doc {
			t.Fatal("时钟传递了错误的文档")
		}
		return visible
	}, func() { wakes++ })
	defer c.close()
	now := time.Now()
	c.now = func() time.Time { return now }
	var fire func()
	c.after = func(_ time.Duration, callback func()) func() {
		fire = callback
		return func() {}
	}
	c.request(doc, func(stamp time.Time) {
		if stamp != now {
			t.Fatal("帧时间戳不一致")
		}
		calls++
	})
	if fire != nil {
		t.Fatal("不可运行的请求启动了定时器")
	}
	visible = true
	c.updateTimer()
	if fire == nil {
		t.Fatal("可运行的请求未启动定时器")
	}
	now = now.Add(animationFrameInterval)
	fire()
	if wakes != 1 || calls != 0 {
		t.Fatal("定时器应只调用唤醒入口")
	}
	c.tick()
	c.updateTimer()
	if calls != 1 || len(c.requests) != 0 || c.stop != nil {
		t.Fatal("一次性请求未正确执行并清理")
	}
	c.request(doc, func(time.Time) { t.Fatal("已取消的生命周期仍执行回调") })
	cancel()
	c.tick()
	if !c.closed || len(c.requests) != 0 || c.stop != nil {
		t.Fatal("生命周期结束后未关闭时钟")
	}
}

type fakeAnimationTimer struct {
	due     time.Time
	fire    func()
	stopped bool
}

func TestAnimationClockDocumentOwnership(t *testing.T) {
	for _, doc := range []*Document{{}, nil} {
		c := newAnimationClock(context.Background(), func(*Document) bool { return true }, func() {})
		now := time.Now()
		c.now = func() time.Time { return now }
		c.after = func(time.Duration, func()) func() { return func() {} }
		other := &Document{}
		calls := 0
		c.request(other, func(time.Time) { c.cancelDocument(doc); calls++ })
		c.request(doc, func(time.Time) { t.Fatal("按文档取消未阻止快照中的回调") })
		c.request(other, func(time.Time) { calls++ })
		now = now.Add(animationFrameInterval)
		c.tick()
		c.updateTimer()
		if calls != 2 || len(c.requests) != 0 {
			t.Fatal("按文档取消影响了其他文档，或未释放请求")
		}
		c.close()
	}
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
	app.animation.tick()
	app.animation.updateTimer()
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
	app.animation.cancelDocument(overlay)
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

func TestTweenValues(t *testing.T) {
	for _, tc := range []struct {
		easing Easing
		want   float64
	}{
		{EaseLinear, 0.25}, {EaseIn, 0.0625}, {EaseOut, 0.4375}, {EaseInOut, 0.125},
	} {
		o := AnimationOptions{Duration: time.Second, Easing: tc.easing}
		if got, done := o.progress(250 * time.Millisecond); got != tc.want || done {
			t.Fatalf("缓动 %d: 得到 %v/%v，期望 %v/false", tc.easing, got, done, tc.want)
		}
		previous := 0.0
		for i := 0; i <= 100; i++ {
			v, _ := o.progress(time.Duration(i) * time.Second / 100)
			if v < previous || v < 0 || v > 1 {
				t.Fatal("缓动不单调或越界")
			}
			previous = v
		}
		if v, done := o.progress(-time.Second); v != 0 || done {
			t.Fatal("起点不正确")
		}
		if v, done := o.progress(2 * time.Second); v != 1 || !done {
			t.Fatal("终点不正确")
		}
	}
	for _, ends := range [][2]float64{{20, -20}, {4, 4}, {-math.MaxFloat64, math.MaxFloat64}} {
		o := TweenOptions{From: ends[0], To: ends[1]}
		v := o.value(0.5)
		if v != ends[0]/2+ends[1]/2 {
			t.Fatal("反向、相同或极大端点插值错误")
		}
	}
}

func TestAnimateProgress(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	var progress []float64
	completed := 0
	doc.Animate(AnimationOptions{
		Duration:   time.Second,
		Easing:     EaseIn,
		OnUpdate:   func(value float64) { progress = append(progress, value) },
		OnComplete: func() { completed++ },
	})
	f.now = f.now.Add(500 * time.Millisecond)
	animationStep(app)
	f.now = f.now.Add(500 * time.Millisecond)
	animationStep(app)
	if !slices.Equal(progress, []float64{0.25, 1}) || completed != 1 {
		t.Fatalf("进度或完成回调不正确：%v, %d", progress, completed)
	}
}

func TestAnimateInvalidOptions(t *testing.T) {
	_, doc, _ := newAnimationTestApp(t)
	valid := AnimationOptions{Duration: time.Second, OnUpdate: func(float64) {}}
	for _, edit := range []func(*AnimationOptions){
		func(o *AnimationOptions) { o.OnUpdate = nil },
		func(o *AnimationOptions) { o.Duration = -1 },
		func(o *AnimationOptions) { o.Easing = Easing(255) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("无效参数应被拒绝")
				}
			}()
			o := valid
			edit(&o)
			doc.Animate(o)
		}()
	}
}

func TestTweenFramesAndCompletion(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	var values []float64
	var events []string
	cancel := doc.Tween(TweenOptions{
		From: 10, To: 30, Duration: time.Second,
		OnUpdate:   func(v float64) { values = append(values, v); events = append(events, "更新") },
		OnComplete: func() { events = append(events, "完成") },
	})
	if len(values) != 0 {
		t.Fatal("注册时不应同步回调")
	}
	f.now = f.now.Add(250 * time.Millisecond)
	animationStep(app)
	f.now = f.now.Add(250 * time.Millisecond)
	animationStep(app)
	f.now = f.now.Add(5 * time.Second)
	animationStep(app)
	cancel()
	cancel()
	animationStep(app)
	if !slices.Equal(values, []float64{15, 20, 30}) || !slices.Equal(events, []string{"更新", "更新", "更新", "完成"}) {
		t.Fatalf("值和完成顺序不正确：%v, %v", values, events)
	}
	if app.animation.stop != nil || len(app.animation.requests) != 0 {
		t.Fatal("完成后仍有活动请求")
	}
	if doc.dirty() || app.dirty {
		t.Fatal("Tween 不应自动请求重绘")
	}
}

func TestTweenZeroDuration(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	calls, completions := 0, 0
	doc.Tween(TweenOptions{From: 2, To: 7,
		OnUpdate: func(v float64) {
			calls++
			if v != 7 {
				t.Fatal("零时长未交付终点")
			}
		},
		OnComplete: func() { completions++ },
	})
	if calls != 0 {
		t.Fatal("零时长仍应异步更新")
	}
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if calls != 1 || completions != 1 {
		t.Fatal("零时长未恰好完成一次")
	}
}

func TestTweenCancelAndLifecycle(t *testing.T) {
	for _, action := range []string{"取消", "关闭文档", "退出App"} {
		for _, when := range []string{"首帧前", "更新中", "终点更新中"} {
			t.Run(action+when, func(t *testing.T) {
				app, doc, f := newAnimationTestApp(t)
				var cancel func()
				stop := func() {
					switch action {
					case "取消":
						cancel()
						cancel()
					case "关闭文档":
						doc.Close()
					case "退出App":
						app.Quit()
					}
				}
				calls := 0
				cancel = doc.Tween(TweenOptions{To: 1, Duration: time.Second,
					OnUpdate:   func(float64) { calls++; stop() },
					OnComplete: func() { t.Fatal("取消或关闭后仍调用完成回调") },
				})
				if when == "首帧前" {
					stop()
				}
				if when == "终点更新中" {
					f.now = f.now.Add(time.Second)
				} else {
					f.now = f.now.Add(animationFrameInterval)
				}
				animationStep(app)
				f.now = f.now.Add(time.Second)
				animationStep(app)
				want := 1
				if when == "首帧前" {
					want = 0
				}
				if calls != want || app.animation.stop != nil || len(app.animation.requests) != 0 {
					t.Fatal("停止后仍有更新或请求")
				}
			})
		}
	}
}

func TestTweenBackgroundTime(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	background := &Desktop{app: app}
	app.desktops.PushBack(background)
	other := addDesktopTestDocument(app, background)
	calls, completions := 0, 0
	other.Tween(TweenOptions{To: 1, Duration: time.Second,
		OnUpdate: func(v float64) {
			calls++
			if v != 1 {
				t.Fatal("后台时间未计入进度")
			}
		},
		OnComplete: func() { completions++ },
	})
	if app.animation.stop != nil {
		t.Fatal("后台 Tween 产生周期唤醒")
	}
	f.now = f.now.Add(2 * time.Second)
	app.SwitchTo(other.desktop)
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if calls != 1 || completions != 1 {
		t.Fatal("恢复后未直接完成")
	}
	app.SwitchTo(doc.desktop)
}

func TestTweenSharedFrameStartAndChaining(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	var got []float64
	doc.RequestAnimationFrame(func(now time.Time) {
		f.now = now.Add(100 * time.Millisecond) // 模拟帧内已有回调耗时。
		doc.Tween(TweenOptions{To: 1, Duration: time.Second,
			OnUpdate: func(v float64) { got = append(got, v) },
			OnComplete: func() {
				doc.Tween(TweenOptions{From: 1, To: 2, OnUpdate: func(v float64) { got = append(got, v) }})
			},
		})
	})
	f.now = f.now.Add(animationFrameInterval)
	start := f.now
	animationStep(app)
	if len(got) != 0 {
		t.Fatal("新动画不应在同帧执行")
	}
	f.now = start.Add(500 * time.Millisecond)
	animationStep(app)
	f.now = start.Add(time.Second)
	animationStep(app)
	if !slices.Equal(got, []float64{0.5, 1}) {
		t.Fatalf("未使用统一帧起点：%v", got)
	}
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if !slices.Equal(got, []float64{0.5, 1, 2}) {
		t.Fatal("完成回调创建的动画未延到下一帧")
	}
}

func TestTweenInvalidOptions(t *testing.T) {
	app, doc, _ := newAnimationTestApp(t)
	valid := TweenOptions{To: 1, Duration: time.Second, OnUpdate: func(float64) {}}
	for _, edit := range []func(*TweenOptions){
		func(o *TweenOptions) { o.OnUpdate = nil },
		func(o *TweenOptions) { o.Duration = -1 },
		func(o *TweenOptions) { o.Easing = Easing(255) },
		func(o *TweenOptions) { o.From = math.NaN() },
		func(o *TweenOptions) { o.To = math.Inf(1) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("无效参数应被拒绝")
				}
			}()
			o := valid
			edit(&o)
			doc.Tween(o)
		}()
	}
	for _, stopped := range []*Document{doc, {app: app}} {
		app.Quit()
		func() {
			defer func() {
				if recover() == nil {
					t.Error("已停止的动画时钟应被拒绝")
				}
			}()
			stopped.Tween(valid)
		}()
		app.animation.close()
	}
}

func TestTimelineSharesFramePerDocument(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	other := addDesktopTestDocument(app, doc.desktop)
	var order []int
	for i := range 4 {
		owner := doc
		if i >= 2 {
			owner = other
		}
		owner.Tween(TweenOptions{To: 1, Duration: time.Second,
			OnUpdate: func(v float64) {
				if v != 0.5 {
					t.Fatal("同帧进度不一致")
				}
				order = append(order, i)
			},
		})
	}
	if len(app.animation.requests) != 2 {
		t.Fatal("每个文档应只申请一个帧回调")
	}
	f.now = f.now.Add(time.Second / 2)
	animationStep(app)
	if !slices.Equal(order, []int{0, 1, 2, 3}) || len(app.animation.requests) != 2 {
		t.Fatal("Timeline 未按顺序统一推进或重复续订")
	}
}

func TestTimelineCancelAndReuse(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	var stopSecond func()
	var order []int
	stopFirst := doc.Tween(TweenOptions{To: 1, OnUpdate: func(float64) {
		order = append(order, 1)
		stopSecond()
	}})
	stopSecond = doc.Tween(TweenOptions{To: 1, OnUpdate: func(float64) { t.Fatal("同帧取消未生效") }})
	timeline := doc.timeline
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	stopFirst()
	stopSecond()
	if timeline.closed || len(timeline.animations) != 0 || len(app.animation.requests) != 0 {
		t.Fatal("空闲 Timeline 未正确清理，或被永久关闭")
	}
	stop := doc.Tween(TweenOptions{To: 1, OnUpdate: func(float64) { t.Fatal("首帧前取消未生效") }})
	stop()
	stop()
	if timeline != doc.timeline || timeline.pending != nil || app.animation.stop != nil {
		t.Fatal("空闲 Timeline 无法复用或取消最后一个动画后仍在续订")
	}
	doc.Tween(TweenOptions{To: 1, OnUpdate: func(float64) { order = append(order, 3) }})
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if !slices.Equal(order, []int{1, 3}) {
		t.Fatal("复用 Timeline 未执行新动画")
	}
}

func TestTimelineDefersAdditionsBeforeItsFrame(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	var order []int
	// 手写帧回调先执行，在已有 Timeline 的本帧快照之前新增 Tween。
	doc.RequestAnimationFrame(func(time.Time) {
		doc.Tween(TweenOptions{To: 1, OnUpdate: func(float64) { order = append(order, 2) }})
	})
	doc.Tween(TweenOptions{To: 1, OnUpdate: func(float64) { order = append(order, 1) }})
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if !slices.Equal(order, []int{1}) {
		t.Fatal("帧内新增 Tween 被已有 Timeline 提前执行")
	}
	f.now = f.now.Add(animationFrameInterval)
	animationStep(app)
	if !slices.Equal(order, []int{1, 2}) {
		t.Fatal("新 Tween 未在下一帧执行")
	}
}

func TestTimelinePausesDuringBatch(t *testing.T) {
	for _, mode := range []string{"切换桌面", "Detach"} {
		t.Run(mode, func(t *testing.T) {
			app, doc, f := newAnimationTestApp(t)
			background := &Desktop{app: app}
			app.desktops.PushBack(background)
			addDesktopTestDocument(app, background)
			calls := 0
			doc.Tween(TweenOptions{To: 1, OnUpdate: func(float64) {
				if mode == "Detach" {
					app.Detach()
				} else {
					app.SwitchTo(background)
				}
			}})
			doc.Tween(TweenOptions{To: 1, Duration: time.Second, OnUpdate: func(v float64) {
				calls++
				if v != 1 {
					t.Fatal("恢复后未计入后台时间")
				}
			}})
			f.now = f.now.Add(animationFrameInterval)
			animationStep(app)
			if calls != 0 || app.animation.stop != nil || len(doc.timeline.animations) != 1 {
				t.Fatal("当前帧后续动画未暂停，或产生了后台唤醒")
			}
			f.now = f.now.Add(2 * time.Second)
			if mode == "Detach" {
				app.Attach()
			} else {
				app.SwitchTo(doc.desktop)
			}
			animationStep(app)
			if calls != 1 {
				t.Fatal("恢复后未推进待执行的动画")
			}
		})
	}
}

func TestTimelineCloseReleasesCallbacks(t *testing.T) {
	for _, mode := range []string{"文档关闭", "App退出", "时钟关闭", "更新中关闭"} {
		t.Run(mode, func(t *testing.T) {
			app, doc, f := newAnimationTestApp(t)
			// 保留一个前台文档，避免被测文档关闭后后台桌面自动转到前台。
			addDesktopTestDocument(app, doc.desktop)
			background := &Desktop{app: app}
			app.desktops.PushBack(background)
			other := addDesktopTestDocument(app, background)
			if mode == "更新中关闭" {
				doc.Tween(TweenOptions{To: 1, OnUpdate: func(float64) { doc.Close() }})
			}
			opts := TweenOptions{To: 1, OnUpdate: func(float64) { t.Fatal("关闭后仍更新") },
				OnComplete: func() { t.Fatal("关闭后仍触发完成回调") },
			}
			stop := doc.Tween(opts)
			other.Tween(opts)
			entries := append([]*_TimelineAnimation(nil), doc.timeline.animations...)
			switch mode {
			case "文档关闭":
				doc.Close()
			case "App退出":
				app.Quit()
			case "时钟关闭":
				app.animation.close()
			}
			f.now = f.now.Add(animationFrameInterval)
			animationStep(app)
			stop()
			stop()
			if !doc.timeline.closed || len(doc.timeline.animations) != 0 || doc.timeline.pending != nil {
				t.Fatal("关闭后仍保留 Timeline 动画")
			}
			for _, a := range entries {
				if a.tick != nil {
					t.Fatal("关闭后仍保留用户回调")
				}
			}
			if mode == "App退出" || mode == "时钟关闭" {
				if !other.timeline.closed || len(other.timeline.animations) != 0 {
					t.Fatal("App退出未清理后台 Timeline")
				}
			} else if other.timeline.closed {
				t.Fatal("关闭一个文档影响了另一个文档的 Timeline")
			}
		})
	}
}
