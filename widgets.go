package fbiw

import (
	"fmt"
	"strconv"
)

var (
	toggleTrackOffColor = ColorFromRGBA(101, 107, 118, 255)
	toggleTrackOnColor  = ColorFromRGBA(54, 183, 102, 255)
	toggleKnobColor     = ColorFromRGBA(255, 255, 255, 255)
)

// ToggleChangeEvent 在 Toggle 的选中状态发生变化后派发。
var ToggleChangeEvent = RegisterEventType()

type ToggleChangeArgs struct {
	Checked bool
}

// Toggle 是一个有选中状态的开关。激活后按 A 键切换状态。
//
// Toggle 只绘制开关本身，不接受子节点。开关对应的文字等内容应由外部
// 元素提供。选中状态会同步为 checked 类名。
type Toggle struct {
	BaseBox

	checked           bool
	trackColor        Color
	checkedTrackColor Color
	knobColor         Color
}

func init() {
	Define(`toggle`, true, NewToggle)
}

func NewToggle(doc *Document) *Toggle {
	b := &Toggle{
		BaseBox:           NewBaseBox(doc, `toggle`),
		trackColor:        toggleTrackOffColor,
		checkedTrackColor: toggleTrackOnColor,
		knobColor:         toggleKnobColor,
	}
	b.Listen(StickDownEvent, func(event *Event) {
		if event.Stick.Name != A || event.Stick.Repeat {
			return
		}
		b.SetChecked(!b.Checked())
		event.StopPropagation()
	})
	return b
}

// intrinsicSize 根据当前字号计算默认尺寸。比例以 32 像素字号下的
// 72×40 开关为基准。
func (b *Toggle) intrinsicSize() (width, height int) {
	fontSize := int(b.computedStyles.FontSize.Number)
	return max(1, fontSize*9/4), max(1, fontSize*5/4)
}

// Calc 使用开关图形作为未指定尺寸时的固有尺寸。
func (b *Toggle) Calc(availWidth, availHeight int, constraints Constraints) {
	intrinsicWidth, intrinsicHeight := b.intrinsicSize()
	b.layoutBox.Width = resolveSize(
		b.computedStyles.Width,
		availWidth,
		false,
		min(availWidth, intrinsicWidth+b.HorizontalInsets()),
	)
	b.layoutBox.Height = resolveSize(
		b.computedStyles.Height,
		availHeight,
		false,
		min(availHeight, intrinsicHeight+b.VerticalInsets()),
	)
}

// Draw 在内容区域中绘制轨道和滑块。
func (b *Toggle) Draw(canvas *Canvas) {
	b.BaseBox.draw(canvas, false)

	trackX := b.InsetLeft()
	trackY := b.InsetTop()
	trackWidth := b.layoutBox.Width - b.HorizontalInsets()
	trackHeight := b.layoutBox.Height - b.VerticalInsets()
	inset := max(1, min(trackWidth, trackHeight)/10)
	if trackHeight <= inset*2 {
		return
	}
	if trackWidth <= inset*2 {
		return
	}

	trackColor := b.trackColor
	if b.checked {
		trackColor = b.checkedTrackColor
	}
	canvas.FillRect(trackX, trackY, trackWidth, trackHeight, trackColor)

	knobSize := min(trackHeight-inset*2, trackWidth-inset*2)
	knobX := trackX + inset
	if b.checked {
		knobX = trackX + trackWidth - inset - knobSize
	}
	knobY := trackY + (trackHeight-knobSize)/2
	canvas.FillRect(knobX, knobY, knobSize, knobSize, b.knobColor)
}

func (b *Toggle) Checked() bool {
	return b.checked
}

// SetChecked 设置选中状态，并在状态发生变化时派发 ToggleChangeEvent。
func (b *Toggle) SetChecked(checked bool) {
	b.setChecked(checked, true)
}

func (b *Toggle) setChecked(checked, dispatch bool) {
	if b.checked == checked {
		return
	}
	b.checked = checked
	b.ClassToggle(`checked`, checked)
	if dispatch {
		b.Dispatch(ToggleChangeEvent, ToggleChangeArgs{Checked: checked})
	}
}

func (b *Toggle) SetProp(key, value string) error {
	switch key {
	case `track-color`, `checked-track-color`, `knob-color`:
		parsed, err := ParseColor(value)
		if err != nil || !parsed.IsColor() {
			return fmt.Errorf(`%s 属性不是颜色：%s`, key, value)
		}
		switch key {
		case `track-color`:
			b.trackColor = parsed.Color
		case `checked-track-color`:
			b.checkedTrackColor = parsed.Color
		case `knob-color`:
			b.knobColor = parsed.Color
		}
		b.document.paintDirty = true
		return nil
	case `checked`:
	default:
		return b.Base().SetProp(key, value)
	}

	// 与 HTML 布尔属性相同，空的 checked 属性表示 true。
	if value == `` {
		b.setChecked(true, false)
		return nil
	}
	checked, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf(`checked 属性不是布尔值：%s`, value)
	}
	b.setChecked(checked, false)
	return nil
}

func (b *Toggle) OnChange(handler func(checked bool)) func() {
	return b.Listen(ToggleChangeEvent, func(event *Event) {
		args := event.Data[ToggleChangeArgs]()
		handler(args.Checked)
	})
}
