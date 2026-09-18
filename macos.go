//go:build darwin

package fbiw

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/movsb/fbiw/internal/canvas/cpu"
	"github.com/veandco/go-sdl2/sdl"
)

type _SdlDisplay struct {
	width, height int

	window  *sdl.Window
	surface *sdl.Surface
	buffer  *sdl.Surface
}

func openAcceleratedRenderer() (Renderer, bool) {
	return nil, false
}

func (d *_SdlDisplay) GetSize() (int, int, int) {
	return d.width, d.height, d.width * 4
}

func (d *_SdlDisplay) Sync(pixels []byte) {
	copy(d.buffer.Pixels(), pixels)

	originRect := sdl.Rect{X: 0, Y: 0, W: int32(d.width), H: int32(d.height)}
	scaledRect := sdl.Rect{X: 0, Y: 0, W: d.surface.W, H: d.surface.H}

	d.buffer.BlitScaled(&originRect, d.surface, &scaledRect)
	d.window.UpdateSurface()
}

func (d *_SdlDisplay) Close() {
	d.window.Destroy()
	sdl.Quit()
}

// 创建一个固定大小的基于SDL2的显示层/渲染器。
func OpenDisplay() Renderer {
	const (
		scale        = 1
		renderWidth  = 1024
		renderHeight = 768
		windowWidth  = renderWidth * scale
		windowHeight = renderHeight * scale
	)

	if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
		panic(err)
	}

	// 启动即关闭输入法。
	sdl.StopTextInput()

	window, err := sdl.CreateWindow("fbiw",
		sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED,
		windowWidth, windowHeight, sdl.WINDOW_SHOWN,
	)
	if err != nil {
		panic(err)
	}

	// wid, _ := window.GetID()

	surface, err := window.GetSurface()
	if err != nil {
		panic(err)
	}

	buffer, err := sdl.CreateRGBSurface(0, renderWidth, renderHeight, 32, 0, 0, 0, 0)
	if err != nil {
		panic(err)
	}

	d := &_SdlDisplay{
		width:   renderWidth,
		height:  renderHeight,
		window:  window,
		surface: surface,
		buffer:  buffer,
	}
	r := cpu.New(d.width, d.height)
	r.Present = d.Sync
	r.CloseFunc = d.Close
	return r
}

func pollEvents(
	ctx context.Context, cancel context.CancelFunc,
	unblock chan struct{}, unblockHandler func(),
	sync func(), eventHandler func(*Event),
) {
	wakeEvent := sdl.RegisterEvents(1)
	if wakeEvent == ^uint32(0) {
		panic("无法注册 SDL 唤醒事件。")
	}
	pollSDLEvents(
		ctx, cancel,
		unblock, unblockHandler,
		sync, eventHandler,
		sdl.WaitEventTimeout,
		func() {
			filtered, err := sdl.PushEvent(&sdl.UserEvent{Type: wakeEvent})
			if err != nil || filtered {
				log.Printf("SDL 唤醒事件未入队：filtered=%v, error=%v", filtered, err)
			}
		},
	)
}

// wait 必须在 UI 主线程执行；push 可以在其它线程执行。
// 一秒超时仅兜底事件被过滤或队列满的情况，正常唤醒由自定义事件即时触发。
func pollSDLEvents(
	ctx context.Context, cancel context.CancelFunc,
	unblock <-chan struct{}, unblockHandler func(),
	sync func(), eventHandler func(*Event),
	wait func(int) sdl.Event, push func(),
) {
	var pending atomic.Bool
	stopped, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				push() // Quit 可以从其它线程调用，须唤醒正在等待的 UI 主线程。
				return
			case <-unblock:
				if !pending.Swap(true) {
					push()
				}
			}
		}
	}()
	// 保证退出消息循环后，不再有桥接线程访问 SDL。
	defer func() { close(stopped); <-joined }()
	sendKey := func(name KeyName, pressed, repeat bool) {
		eventHandler(&Event{
			Type: Iif(pressed, StickDownEvent, StickUpEvent),
			Stick: KeyEventArgs{
				Name:   name,
				Repeat: repeat,
			},
		})
	}

	keyMaps := map[sdl.Keycode]KeyName{
		sdl.K_w: Up,
		sdl.K_s: Down,
		sdl.K_a: Left,
		sdl.K_d: Right,
		sdl.K_k: A,
		sdl.K_j: B,
		sdl.K_i: X,
		sdl.K_u: Y,
		sdl.K_r: Menu,
		sdl.K_t: Select,
		sdl.K_y: Start,
		sdl.K_q: L1,
		sdl.K_o: R1,
	}

	for {
		if ctx.Err() != nil {
			return
		}
		event := wait(1000)
		if ctx.Err() != nil {
			return
		}
		// 先释放合并标记；处理期间的新任务会再次入队唤醒，不会丢失。
		if pending.Swap(false) {
			unblockHandler()
		}
		if ctx.Err() != nil {
			return
		}
		switch event := event.(type) {
		case *sdl.QuitEvent:
			cancel()
			return
		case *sdl.KeyboardEvent:
			pressed := event.Type == sdl.KEYDOWN
			key := event.Keysym.Sym
			if mapped, ok := keyMaps[key]; ok {
				sendKey(mapped, pressed, event.Repeat != 0)
			}
		}
		sync()
	}
}
