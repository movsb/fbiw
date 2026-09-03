package fbiw

import (
	"strings"
	"testing"
	"time"
)

func TestParseBoxSupportsAnyBoxRoot(t *testing.T) {
	doc := &Document{}
	box, err := parseBox(doc, strings.NewReader(`<text>hello</text>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := box.(*Text); !ok {
		t.Fatalf(`根元素类型 = %T，期望 *Text`, box)
	}
}

func TestParseBoxRejectsInvalidFragmentRoots(t *testing.T) {
	tests := []string{
		``,
		`text only`,
		`<block></block><text></text>`,
	}
	for _, content := range tests {
		if _, err := parseBox(&Document{}, strings.NewReader(content)); err == nil {
			t.Errorf(`parseBox(%q) 没有返回错误`, content)
		}
	}
}

func TestDocumentTemplateValidation(t *testing.T) {
	tests := []string{
		`<template><block></block></template>`,
		`<template id=" "><block></block></template>`,
		`<template id="item"><block></block></template><template id="item"><text></text></template>`,
		`<template id="item">text<block></block></template>`,
	}
	for _, template := range tests {
		content := `<document>` + template + `<block></block></document>`
		if _, err := parseDocument(&Document{}, strings.NewReader(content)); err == nil {
			t.Errorf(`parseDocument(%q) 没有返回错误`, content)
		}
	}
}

func TestInstantiateTemplateWithTextRoot(t *testing.T) {
	doc := &Document{templates: map[string]string{`label`: `<text>hello</text>`}}
	type view struct {
		root Box
	}
	instance := doc.Instantiate[view](`label`)
	if _, ok := instance.root.(*Text); !ok {
		t.Fatalf(`模板根元素类型 = %T，期望 *Text`, instance.root)
	}
}

func waitPendingCallback(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.lock.Lock()
		count := len(app.pending)
		app.lock.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(`等待定时器回调超时`)
}

func runPendingCallbacks(app *App) {
	app.lock.Lock()
	pending := app.pending
	app.pending = nil
	app.lock.Unlock()
	for _, callback := range pending {
		callback()
	}
}

func pendingCallbackCount(app *App) int {
	app.lock.Lock()
	defer app.lock.Unlock()
	return len(app.pending)
}

func newAsyncTestDocument() (*App, *Document) {
	app := newDesktopTestApp()
	doc := &Document{}
	doc.bindApp(app)
	return app, doc
}

func TestDocumentAsyncRunsOnCurrentLifecycle(t *testing.T) {
	app, doc := newAsyncTestDocument()
	called := 0
	doc.Async(func() { called++ })
	if pendingCallbackCount(app) != 1 {
		t.Fatal("Document.Async 没有投递到 App")
	}
	runPendingCallbacks(app)
	if called != 1 {
		t.Fatal("有效生命周期中的回调没有执行")
	}
}

func TestDocumentAsyncIgnoresInvalidLifecycle(t *testing.T) {
	for _, mode := range []string{"提交前未绑定", "提交后关闭", "前一回调关闭", "App退出"} {
		t.Run(mode, func(t *testing.T) {
			app, doc := newAsyncTestDocument()
			called := 0
			if mode == "提交前未绑定" {
				doc.unbindApp()
			}
			if mode == "前一回调关闭" {
				doc.Async(func() { doc.Close() })
			}
			doc.Async(func() { called++ })
			switch mode {
			case "提交后关闭":
				doc.Close()
			case "App退出":
				app.Quit()
			}
			runPendingCallbacks(app)
			if called != 0 {
				t.Fatal("失效生命周期中的回调仍然执行")
			}
		})
	}
}

func TestDocumentAsyncCanBeSubmittedFromGoroutine(t *testing.T) {
	app, doc := newAsyncTestDocument()
	const count = 100
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range count {
			doc.Async(func() {})
		}
	}()
	<-done
	if pendingCallbackCount(app) != count {
		t.Fatalf("后台投递数量 = %d，期望 %d", pendingCallbackCount(app), count)
	}
	doc.Close()
	runPendingCallbacks(app)
}

func TestDocumentAsyncHelpersRespectLifecycle(t *testing.T) {
	app, doc := newAsyncTestDocument()
	doc.RequestLayoutAsync()
	doc.RequestPaintAsync()
	doc.Close()
	runPendingCallbacks(app)
	if doc.layoutDirty || doc.paintDirty {
		t.Fatal("关闭后仍执行了异步布局或重绘请求")
	}

	// 未绑定时直接忽略，不产生无法执行的 App 回调。
	doc.RequestLayoutAsync()
	doc.RequestPaintAsync()
	if pendingCallbackCount(app) != 0 {
		t.Fatal("未绑定文档仍然投递了异步请求")
	}
}

func TestDocumentAsyncRejectsNilCallback(t *testing.T) {
	_, doc := newAsyncTestDocument()
	defer func() {
		if recover() == nil {
			t.Fatal("nil 回调没有 panic")
		}
	}()
	doc.Async(nil)
}

func TestSetTimeoutSupportsMillisecondsAndDuration(t *testing.T) {
	app := newDesktopTestApp()
	doc := &Document{app: app}
	called := 0

	doc.SetTimeout(2, func() { called++ })
	waitPendingCallback(t, app)
	runPendingCallbacks(app)

	doc.SetTimeout(2*time.Millisecond, func() { called++ })
	waitPendingCallback(t, app)
	runPendingCallbacks(app)

	if called != 2 {
		t.Fatalf(`Timeout 回调次数 = %d，期望 2`, called)
	}
}

func TestCancelSuppressesQueuedTimeout(t *testing.T) {
	app := newDesktopTestApp()
	doc := &Document{app: app}
	called := 0
	cancel := doc.SetTimeout(time.Millisecond, func() { called++ })

	waitPendingCallback(t, app)
	cancel()
	cancel()
	runPendingCallbacks(app)
	if called != 0 {
		t.Fatalf(`取消后执行了已排队的 Timeout：%d`, called)
	}
}

func TestSetIntervalDoesNotAccumulateCallbacks(t *testing.T) {
	app := newDesktopTestApp()
	doc := &Document{app: app}
	called := 0
	cancel := doc.SetInterval(time.Millisecond, func() { called++ })

	waitPendingCallback(t, app)
	time.Sleep(10 * time.Millisecond)
	if got := pendingCallbackCount(app); got != 1 {
		t.Fatalf(`UI 忙碌时 Interval 排队了 %d 个回调，期望 1`, got)
	}
	runPendingCallbacks(app)
	if called != 1 {
		t.Fatalf(`Interval 回调次数 = %d，期望 1`, called)
	}

	waitPendingCallback(t, app)
	cancel()
	runPendingCallbacks(app)
	if called != 1 {
		t.Fatalf(`取消后执行了已排队的 Interval：%d`, called)
	}
}

func TestDocumentCloseCancelsTimers(t *testing.T) {
	app := newDesktopTestApp()
	doc := &Document{app: app}
	called := 0
	doc.SetTimeout(time.Millisecond, func() { called++ })
	doc.SetInterval(time.Millisecond, func() { called++ })

	waitPendingCallback(t, app)
	doc.Close()
	runPendingCallbacks(app)
	if called != 0 {
		t.Fatalf(`Document 关闭后执行了定时器：%d`, called)
	}
	doc.timersMu.Lock()
	remaining := len(doc.timers)
	doc.timersMu.Unlock()
	if remaining != 0 {
		t.Fatalf(`Document 关闭后仍有 %d 个定时器`, remaining)
	}
}

func TestTimerRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Document)
	}{
		{`zero timeout`, func(doc *Document) { doc.SetTimeout(0, func() {}) }},
		{`negative interval`, func(doc *Document) { doc.SetInterval(-time.Millisecond, func() {}) }},
		{`nil callback`, func(doc *Document) { doc.SetTimeout(time.Millisecond, nil) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal(`非法定时器参数没有 panic`)
				}
			}()
			test.run(&Document{app: newDesktopTestApp()})
		})
	}
}
