package main

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
		return `增量+`
	case VolumeDown:
		return `增量-`
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
	Width, Height int
	Bpp, Stride   int

	// 内部双缓冲，可放心写。
	Data []byte

	// 写完调用此方法同步到屏幕。
	Sync func()

	Close func()
}

type EventType uint

const (
	QuitEvent EventType = iota
	KeyboardEvent
)

type KeyboardEventArgs struct {
	Name    KeyName
	Pressed bool
}

type Event struct {
	Type EventType

	Keyboard KeyboardEventArgs
}
