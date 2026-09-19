package ports

import (
	"context"
	"sync"
	"time"

	"github.com/movsb/fbiw/input"
	"github.com/movsb/fbiw/internal/event"
)

// keyRepeater 把一次按下模拟成桌面键盘式的自动重复：先立即发送一次
// InputDownEvent，等待 delay 后持续发送 InputDownEvent，直到收到松开。
type keyRepeater struct {
	ctx      context.Context
	delay    time.Duration
	interval time.Duration
	handler  func(*event.Message)

	mu   sync.Mutex
	held map[input.Name]context.CancelFunc
}

func newKeyRepeater(ctx context.Context, delay, interval time.Duration, handler func(*event.Message)) *keyRepeater {
	return &keyRepeater{
		ctx:      ctx,
		delay:    delay,
		interval: interval,
		handler:  handler,
		held:     make(map[input.Name]context.CancelFunc),
	}
}

func (r *keyRepeater) emit(name input.Name, pressed, repeat bool) {
	ty := event.EventInputUp
	if pressed {
		ty = event.EventInputDown
	}
	r.handler(&event.Message{
		Type:  ty,
		Input: event.InputArgs{Name: name, Repeat: repeat},
	})
}

func (r *keyRepeater) send(name input.Name, pressed bool) {
	r.mu.Lock()
	if !pressed {
		if cancel, ok := r.held[name]; ok {
			cancel()
			delete(r.held, name)
		}
		r.emit(name, false, false)
		r.mu.Unlock()
		return
	}

	if _, alreadyHeld := r.held[name]; alreadyHeld {
		r.mu.Unlock()
		return
	}
	repeatCtx, cancel := context.WithCancel(r.ctx)
	r.held[name] = cancel
	r.emit(name, true, false)
	r.mu.Unlock()

	go r.repeat(repeatCtx, name)
}

func (r *keyRepeater) repeat(ctx context.Context, name input.Name) {
	timer := time.NewTimer(r.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		r.mu.Lock()
		if ctx.Err() != nil {
			r.mu.Unlock()
			return
		}
		r.emit(name, true, true)
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *keyRepeater) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, cancel := range r.held {
		cancel()
		delete(r.held, name)
	}
}
