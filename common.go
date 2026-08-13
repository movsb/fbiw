package fbiw

import "slices"

type KeyName uint8

const (
	Up KeyName = iota
	Down
	Left
	Right
	A
	B
	X
	Y
	Menu
	Select
	Start
	Fn1
	Fn2
	VolumeUp
	VolumeDown
	Home
	L1
	R1
)

func (k KeyName) String() string {
	switch k {
	case Up:
		return `上`
	case Down:
		return `下`
	case Left:
		return `左`
	case Right:
		return `右`
	case A:
		return `A`
	case B:
		return `B`
	case X:
		return `X`
	case Y:
		return `Y`
	case Menu:
		return `菜单`
	case Select:
		return `选择`
	case Start:
		return `开始`
	case Fn1:
		return `Fn1`
	case Fn2:
		return `Fn2`
	case VolumeUp:
		return `音量+`
	case VolumeDown:
		return `音量-`
	case Home:
		return `HOME`
	case L1:
		return `L1`
	case R1:
		return `R1`
	}
	return `未知按键`
}

type Display struct {
	// 屏幕的宽度和高度
	Width, Height int

	// 位深以及一行的字节数（带padding）
	Bpp, Stride int

	// 内部双缓冲，可放心写这个缓冲。
	Data []byte

	// 写完后调用此方法同步到屏幕。
	sync func()

	close func()
}

// 把数据同步到屏幕显示。
func (d *Display) Sync() {
	d.sync()
}

// 关闭屏幕（断开与屏幕的连接）。
func (d *Display) Close() {
	d.close()
}

type EventType uint

const (
	UnknownEvent EventType = iota + 1

	asyncCallback
	appDirty

	QuitEvent
	KeyboardEvent
)

type KeyboardEventArgs struct {
	Name KeyName
	// 表示是按下(KeyDown)还是弹起(KeyUp)
	KeyDown bool
}

// 整个系统使用的事件类型。
//
// 一个事件分成3️⃣个部分：
//
//  1. 事件类型
//  2. 事件的阶段和对象
//  3. 事件类型关联的数据
type Event struct {
	Type EventType

	phase _EventPhase
	// 事件真实发生的对象。
	Target Box
	// 当前处理阶段的对象。
	Current Box

	propagationStopped bool

	// 以下属于事件数据，随事件类型选择其一。
	asyncCallback func()
	Keyboard      KeyboardEventArgs
}

func (e *Event) Capturing() bool {
	return e.phase == eventPhaseCapturing
}
func (e *Event) Bubbling() bool {
	return e.phase == eventPhaseBubbling
}
func (e *Event) AtTarget() bool {
	return e.phase == eventPhaseAtTarget
}

func (e *Event) StopPropagation() {
	e.propagationStopped = true
}

func (e *Event) KeyDown(name KeyName) bool {
	return e.Keyboard.KeyDown && e.Keyboard.Name == name
}

// [EventTarget - Web APIs | MDN](https://developer.mozilla.org/en-US/docs/Web/API/EventTarget)
type _EventTarget struct {
	box      Box
	nextID   uint32
	handlers map[EventType][]_EventHandler
}

type _EventHandler struct {
	id      uint32
	handler func(*Event)
	options EventOptions
}

type _EventPhase uint8

const (
	eventPhaseCapturing _EventPhase = iota
	eventPhaseAtTarget
	eventPhaseBubbling
)

type EventOptions struct {
	Capture bool
}

// 添加一个指定事件类型的事件处理器。
//
// 返回值用于删除此事件处理器。
//
// addEventListener
//
// Attach 方法被 app 用了，暂时用这个方法表示监听。
func (e *_EventTarget) Listen(ty EventType, handler func(*Event), options EventOptions) func() {
	if e.handlers == nil {
		e.handlers = map[EventType][]_EventHandler{}
	}
	e.nextID++
	wrapped := _EventHandler{
		id:      e.nextID,
		handler: handler,
		options: options,
	}
	e.handlers[ty] = append(e.handlers[ty], wrapped)
	return func() { e.detach(ty, wrapped.id) }
}

// 用于删除事件处理器。
//
// Go的函数不能进行相等性比较（除和nil外），所以不能直接用于删除。
// 所以创建了唯一ID。
//
// removeEventListener
func (e *_EventTarget) detach(ty EventType, id uint32) {
	e.handlers[ty] = slices.DeleteFunc(e.handlers[ty], func(h _EventHandler) bool {
		return h.id == id
	})
}

// 向该事件对象自己投递事件。
//
// 监听该对象事件的函数均会收到回调。
//
// The dispatchEvent() method of the EventTarget sends an Event to the object,
// (synchronously) invoking the affected event listeners in the appropriate order.
func (e *_EventTarget) Dispatch(event *Event) {
	event.Target = e.box

	// Capturing Phase
	event.phase = eventPhaseCapturing
	for ancestor := range e.box.Base().ancestorsForward() {
		if event.propagationStopped {
			return
		}
		box := ancestor.Base()
		for _, handler := range box._EventTarget.handlers[event.Type] {
			if !handler.options.Capture {
				continue
			}
			event.Current = box
			handler.handler(event)
			if event.propagationStopped {
				return
			}
		}
	}

	// at target
	event.Current = e.box
	event.phase = eventPhaseAtTarget
	for _, handler := range e.box.Base()._EventTarget.handlers[event.Type] {
		handler.handler(event)
		if event.propagationStopped {
			return
		}
	}

	// bubbling
	// TODO 不是所有事件都需要冒泡
	event.phase = eventPhaseBubbling
	for ancestor := range e.box.Base().Ancestors() {
		if event.propagationStopped {
			return
		}
		box := ancestor.Base()
		for _, handler := range box._EventTarget.handlers[event.Type] {
			if handler.options.Capture {
				continue
			}
			event.Current = box
			handler.handler(event)
			if event.propagationStopped {
				return
			}
		}
	}
}
