//go:build js && wasm

package ports

import (
	"context"
	"syscall/js"

	"github.com/movsb/fbiw/input"
	"github.com/movsb/fbiw/input/sticks"
	"github.com/movsb/fbiw/internal/canvas"
	"github.com/movsb/fbiw/internal/canvas/gpu/webgl"
	"github.com/movsb/fbiw/internal/event"
)

func OpenDisplay() canvas.Renderer {
	doc := js.Global().Get("document")
	element := doc.Call("getElementById", "fbiw-canvas")
	if element.IsNull() || element.IsUndefined() {
		panic("missing #fbiw-canvas")
	}
	r, err := webgl.New(element)
	if err != nil {
		panic(err)
	}
	return r
}

func PollEvents(
	ctx context.Context, cancel context.CancelFunc,
	unblock chan struct{}, unblockHandler func(),
	syncFrame func(), resize func(int, int),
	eventHandler func(*event.Message),
) {
	window := js.Global()
	doc := window.Get("document")
	canvasElement := doc.Call("getElementById", "fbiw-canvas")
	keys := map[string]input.Name{
		"KeyW": sticks.Up,
		"KeyS": sticks.Down,
		"KeyA": sticks.Left,
		"KeyD": sticks.Right,
		"KeyK": sticks.A,
		"KeyJ": sticks.B,
		"KeyI": sticks.X,
		"KeyU": sticks.Y,
		"KeyR": sticks.Menu,
		"KeyT": sticks.Select,
		"KeyY": sticks.Start,
		"KeyQ": sticks.L1,
		"KeyO": sticks.R1,
	}
	pressed := map[input.Name]bool{}
	type listener struct {
		target js.Value
		name   string
		fn     js.Func
	}
	var listeners []listener
	add := func(target js.Value, name string, fn func(js.Value, []js.Value) any) {
		cb := js.FuncOf(fn)
		target.Call("addEventListener", name, cb)
		listeners = append(listeners, listener{target, name, cb})
	}
	defer func() {
		for _, l := range listeners {
			l.target.Call("removeEventListener", l.name, l.fn)
			l.fn.Release()
		}
	}()
	send := func(name input.Name, down, repeat bool) {
		kind := event.EventInputUp
		if down {
			kind = event.EventInputDown
		}
		eventHandler(&event.Message{Type: kind, Input: event.InputArgs{Name: name, Repeat: repeat}})
	}
	add(window, "keydown", func(_ js.Value, args []js.Value) any {
		e := args[0]
		name, ok := keys[e.Get("code").String()]
		if !ok {
			return nil
		}
		e.Call("preventDefault")
		if !pressed[name] {
			pressed[name] = true
			send(name, true, false)
		} else if e.Get("repeat").Bool() {
			send(name, true, true)
		}
		syncFrame()
		return nil
	})
	add(window, "keyup", func(_ js.Value, args []js.Value) interface{} {
		e := args[0]
		name, ok := keys[e.Get("code").String()]
		if !ok {
			return nil
		}
		e.Call("preventDefault")
		if pressed[name] {
			delete(pressed, name)
			send(name, false, false)
		}
		syncFrame()
		return nil
	})
	add(window, "blur", func(_ js.Value, _ []js.Value) any {
		for name := range pressed {
			send(name, false, false)
			delete(pressed, name)
		}
		syncFrame()
		return nil
	})
	add(window, "resize", func(_ js.Value, _ []js.Value) any {
		w, h := canvasElement.Get("clientWidth").Int(), canvasElement.Get("clientHeight").Int()
		resize(w, h)
		syncFrame()
		return nil
	})
	// JS callbacks must return promptly. requestAnimationFrame polls the Go wake queue and animation clock.
	done := make(chan struct{})
	var frame js.Func
	frame = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		select {
		case <-ctx.Done():
			close(done)
			return nil
		default:
		}
		select {
		case <-unblock:
			unblockHandler()
		default:
		}
		syncFrame()
		window.Call("requestAnimationFrame", frame)
		return nil
	})
	window.Call("requestAnimationFrame", frame)
	defer frame.Release()
	<-done
	cancel()
}
