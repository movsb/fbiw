// Package event defines events exchanged between platform ports and the UI.
package event

import "github.com/movsb/fbiw/input"

type Type uint

const (
	EventUnknown Type = iota + 1
	EventInputDown
	EventInputUp

	maxSystemType Type = 0xFFFF
)

var nextType = maxSystemType

// 注册一个全局不重复的自定义事件类型。
//
// 应该在全局变量初始化过程或init()函数中调用。
// 不支持在非主线程调用。
func RegisterType() Type {
	nextType++
	return nextType
}

// 设备输入消息。
//
// 包含手柄🕹️、键盘⌨️等。
//
// Name 来自于 keys.*、sticks.*。
type InputArgs struct {
	Name   input.Name
	Repeat bool
}

// 内部定义与UI层的消息桥接层。
type Message struct {
	Type  Type
	Input InputArgs
}
