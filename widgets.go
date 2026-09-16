package fbiw

import (
	"embed"
	"fmt"
	"image"
	"image/draw"
	"math"
	"slices"
	"strconv"
	"time"

	"golang.org/x/image/vector"
)

// ButtonClickEvent 在 Button 被有效触发时派发。
var ButtonClickEvent = RegisterEventType()

type ButtonVariant string

const (
	ButtonNormal      ButtonVariant = `normal`
	ButtonPrimary     ButtonVariant = `primary`
	ButtonDestructive ButtonVariant = `destructive`
)

// Button 是一个带默认外观和按钮语义的容器。激活后按 A 键触发点击；
// disabled 状态下仍会消费 A 键，但不会派发点击事件。
type Button struct {
	BaseBox

	variant  ButtonVariant
	disabled bool
}

func init() {
	Define(`button`, false, NewButton)
}

func NewButton(doc *Document) *Button {
	b := &Button{
		BaseBox: NewBaseBox(doc, `button`),
		variant: ButtonNormal,
	}
	b.Listen(StickDownEvent, func(event *Event) {
		if event.Stick.Name != A || event.Stick.Repeat {
			return
		}
		event.StopPropagation()
		if b.disabled {
			return
		}
		b.Dispatch(ButtonClickEvent, nil)
	})
	return b
}

func (b *Button) Variant() ButtonVariant {
	return b.variant
}

func (b *Button) SetVariant(variant ButtonVariant) error {
	switch variant {
	case ButtonNormal, ButtonPrimary, ButtonDestructive:
	default:
		return fmt.Errorf(`不认识的 button variant：%s`, variant)
	}
	if b.variant == variant {
		return nil
	}
	b.variant = variant
	b.class.Remove(`button-primary`)
	b.class.Remove(`button-destructive`)
	switch variant {
	case ButtonPrimary:
		b.class.Add(`button-primary`)
	case ButtonDestructive:
		b.class.Add(`button-destructive`)
	}
	b.classChanged()
	return nil
}

func (b *Button) Disabled() bool {
	return b.disabled
}

func (b *Button) SetDisabled(disabled bool) {
	if b.disabled == disabled {
		return
	}
	b.disabled = disabled
	b.ClassToggle(`disabled`, disabled)
}

func (b *Button) SetProp(key, value string) error {
	switch key {
	case `variant`:
		return b.SetVariant(ButtonVariant(value))
	case `disabled`:
		disabled, err := parseBooleanAttribute(`disabled`, value)
		if err != nil {
			return err
		}
		b.SetDisabled(disabled)
		return nil
	default:
		return b.Base().SetProp(key, value)
	}
}

func (b *Button) OnClick(handler func()) func() {
	return b.Listen(ButtonClickEvent, func(*Event) {
		handler()
	})
}

func parseBooleanAttribute(name, value string) (bool, error) {
	if value == `` {
		return true, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf(`%s 属性不是布尔值：%s`, name, value)
	}
	return parsed, nil
}

var (
	toggleTrackOffThemeColor = RegisterThemeColor(`--toggle-track-color`, `#656b76`, `#656b76`)
	toggleTrackOnThemeColor  = RegisterThemeColor(`--toggle-checked-track-color`, `var(--color-primary)`, `var(--color-primary)`)
	toggleKnobThemeColor     = RegisterThemeColor(`--toggle-knob-color`, `#ffffff`, `#ffffff`)
	checkBoxThemeColor       = RegisterThemeColor(`--check-box-color`, `#656b76`, `#8b919c`)
	checkCheckedThemeColor   = RegisterThemeColor(`--check-checked-box-color`, `var(--color-primary)`, `var(--color-primary)`)
	checkMarkThemeColor      = RegisterThemeColor(`--check-mark-color`, `#ffffff`, `#ffffff`)
)

const (
	toggleAnimationDuration       = 250 * time.Millisecond
	progressAnimationDuration     = 250 * time.Millisecond
	progressIndeterminateDuration = 900 * time.Millisecond
	progressIndeterminatePause    = 250 * time.Millisecond
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

	checked bool

	trackColor        Color
	checkedTrackColor Color
	knobColor         Color

	themeTrackColor        Color
	themeCheckedTrackColor Color
	themeKnobColor         Color

	customTrackColor        bool
	customCheckedTrackColor bool
	customKnobColor         bool

	// 逻辑状态立即改变，滑块使用独立的显示进度。
	knobProgress   float64
	painted        bool
	knobTransition *Transition[float64]
}

func init() {
	Define(`toggle`, true, NewToggle)
}

func NewToggle(doc *Document) *Toggle {
	var (
		trackColor        = doc.ResolveThemeColor(toggleTrackOffThemeColor)
		checkedTrackColor = doc.ResolveThemeColor(toggleTrackOnThemeColor)
		knobColor         = doc.ResolveThemeColor(toggleKnobThemeColor)
	)
	b := &Toggle{
		BaseBox:                NewBaseBox(doc, `toggle`),
		trackColor:             trackColor,
		checkedTrackColor:      checkedTrackColor,
		knobColor:              knobColor,
		themeTrackColor:        trackColor,
		themeCheckedTrackColor: checkedTrackColor,
		themeKnobColor:         knobColor,
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
	fontSize := int(b.computedStyles.FontSize.Number())
	return max(1, fontSize*9/4), max(1, fontSize*5/4)
}

// Calc 使用开关图形作为未指定尺寸时的固有尺寸。
func (b *Toggle) Calc(availWidth, availHeight int, constraints Constraints) {
	size := b.resolveDimensions(constraints)
	intrinsicWidth, intrinsicHeight := b.intrinsicSize()
	b.layoutBox.Width = resolveSize(
		size.Width,
		availWidth,
		false,
		constrainNaturalSize(intrinsicWidth+b.HorizontalInsets(), availWidth, constraints.UnboundedWidth),
	)
	b.layoutBox.Height = resolveSize(
		size.Height,
		availHeight,
		false,
		constrainNaturalSize(intrinsicHeight+b.VerticalInsets(), availHeight, constraints.UnboundedHeight),
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
	b.painted = true

	trackOffColor := b.trackColor
	if !b.customTrackColor && b.trackColor == b.themeTrackColor {
		trackOffColor = b.document.ResolveThemeColor(toggleTrackOffThemeColor)
		b.trackColor = trackOffColor
		b.themeTrackColor = trackOffColor
	}
	trackOnColor := b.checkedTrackColor
	if !b.customCheckedTrackColor && b.checkedTrackColor == b.themeCheckedTrackColor {
		trackOnColor = b.document.ResolveThemeColor(toggleTrackOnThemeColor)
		b.checkedTrackColor = trackOnColor
		b.themeCheckedTrackColor = trackOnColor
	}
	knobColor := b.knobColor
	if !b.customKnobColor && b.knobColor == b.themeKnobColor {
		knobColor = b.document.ResolveThemeColor(toggleKnobThemeColor)
		b.knobColor = knobColor
		b.themeKnobColor = knobColor
	}
	trackColor := ColorAnimator(trackOffColor, trackOnColor)(b.knobProgress)
	canvas.FillRect(trackX, trackY, trackWidth, trackHeight, trackColor)

	knobSize := min(trackHeight-inset*2, trackWidth-inset*2)
	travel := trackWidth - inset*2 - knobSize
	knobX := trackX + inset + int(math.Round(float64(travel)*b.knobProgress))
	knobY := trackY + (trackHeight-knobSize)/2
	canvas.FillRect(knobX, knobY, knobSize, knobSize, knobColor)
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
	b.animateKnob()
	if dispatch {
		b.Dispatch(ToggleChangeEvent, ToggleChangeArgs{Checked: checked})
	}
}

// 从当前显示位置转向新目标；先安排动画再派发状态事件，
// 使 OnChange 中再次切换状态时，可以正确取消本次动画。
func (b *Toggle) animateKnob() {
	target := 0.0
	if b.checked {
		target = 1
	}
	if b.knobTransition == nil {
		b.knobTransition = b.document.NewTransition(b.knobProgress, TransitionOptions[float64]{
			Duration: toggleAnimationDuration,
			Easing:   EaseOut,
			Animator: NumberAnimator,
			OnUpdate: func(value float64) {
				b.knobProgress = value
				b.document.RequestPaint()
			},
		})
	}
	if !b.painted {
		b.knobTransition.SetValue(target)
	} else {
		b.knobTransition.SetTarget(target)
	}
	b.document.RequestPaint()
}

func (b *Toggle) SetProp(key, value string) error {
	switch key {
	case `track-color`, `checked-track-color`, `knob-color`:
		parsed, err := ParseColor(value)
		if err != nil {
			return fmt.Errorf(`%s 属性不是颜色：%s`, key, value)
		}
		switch key {
		case `track-color`:
			b.trackColor = parsed
			b.customTrackColor = true
		case `checked-track-color`:
			b.checkedTrackColor = parsed
			b.customCheckedTrackColor = true
		case `knob-color`:
			b.knobColor = parsed
			b.customKnobColor = true
		}
		b.document.paintDirty = true
		return nil
	case `checked`:
	default:
		return b.Base().SetProp(key, value)
	}

	checked, err := parseBooleanAttribute(`checked`, value)
	if err != nil {
		return err
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

// CheckChangeEvent 在 CheckBox 的选中状态发生变化后派发。
var CheckChangeEvent = RegisterEventType()

type CheckChangeArgs struct {
	Checked bool
}

// CheckBox 是一个有选中状态的复选框。激活后按 A 键切换状态。
//
// CheckBox 只绘制方框和勾号，不接受子节点。选中状态会同步为 checked 类名。
type CheckBox struct {
	BaseBox

	checked bool

	boxColor        Color
	checkedBoxColor Color
	markColor       Color

	themeBoxColor        Color
	themeCheckedBoxColor Color
	themeMarkColor       Color

	customBoxColor        bool
	customCheckedBoxColor bool
	customMarkColor       bool
}

func init() {
	Define(`check`, true, NewCheckBox)
}

func NewCheckBox(doc *Document) *CheckBox {
	boxColor := doc.ResolveThemeColor(checkBoxThemeColor)
	checkedBoxColor := doc.ResolveThemeColor(checkCheckedThemeColor)
	markColor := doc.ResolveThemeColor(checkMarkThemeColor)
	b := &CheckBox{
		BaseBox:              NewBaseBox(doc, `check`),
		boxColor:             boxColor,
		checkedBoxColor:      checkedBoxColor,
		markColor:            markColor,
		themeBoxColor:        boxColor,
		themeCheckedBoxColor: checkedBoxColor,
		themeMarkColor:       markColor,
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

func (b *CheckBox) intrinsicSize() (width, height int) {
	fontSize := int(b.computedStyles.FontSize.Number())
	size := max(1, fontSize*5/4)
	return size, size
}

func (b *CheckBox) Calc(availWidth, availHeight int, constraints Constraints) {
	size := b.resolveDimensions(constraints)
	intrinsicWidth, intrinsicHeight := b.intrinsicSize()
	b.layoutBox.Width = resolveSize(size.Width, availWidth, false, constrainNaturalSize(intrinsicWidth+b.HorizontalInsets(), availWidth, constraints.UnboundedWidth))
	b.layoutBox.Height = resolveSize(size.Height, availHeight, false, constrainNaturalSize(intrinsicHeight+b.VerticalInsets(), availHeight, constraints.UnboundedHeight))
}

func (b *CheckBox) Draw(canvas *Canvas) {
	b.BaseBox.draw(canvas, false)

	x, y := b.InsetLeft(), b.InsetTop()
	width := b.layoutBox.Width - b.HorizontalInsets()
	height := b.layoutBox.Height - b.VerticalInsets()
	if width < 3 || height < 3 {
		return
	}

	boxColor := b.boxColor
	if !b.customBoxColor && b.boxColor == b.themeBoxColor {
		boxColor = b.document.ResolveThemeColor(checkBoxThemeColor)
		b.boxColor, b.themeBoxColor = boxColor, boxColor
	}
	checkedBoxColor := b.checkedBoxColor
	if !b.customCheckedBoxColor && b.checkedBoxColor == b.themeCheckedBoxColor {
		checkedBoxColor = b.document.ResolveThemeColor(checkCheckedThemeColor)
		b.checkedBoxColor, b.themeCheckedBoxColor = checkedBoxColor, checkedBoxColor
	}
	markColor := b.markColor
	if !b.customMarkColor && b.markColor == b.themeMarkColor {
		markColor = b.document.ResolveThemeColor(checkMarkThemeColor)
		b.markColor, b.themeMarkColor = markColor, markColor
	}

	// 画背景、边框。
	if b.checked {
		canvas.FillRect(x, y, width, height, checkedBoxColor)
	} else {
		border := max(1, min(width, height)/10)
		canvas.FillRect(x, y, width, border, boxColor)
		canvas.FillRect(x, y+height-border, width, border, boxColor)
		canvas.FillRect(x, y+border, border, height-border*2, boxColor)
		canvas.FillRect(x+width-border, y+border, border, height-border*2, boxColor)
	}

	if !b.checked {
		return
	}

	drawCheckMark(canvas, x, y, width, height, markColor)
}

// drawCheckMark 将勾号作为一个连续的六边形路径进行抗锯齿填充。
// 相比沿折线堆叠方块，这能保持两段笔画等宽，并避免转折处鼓包。
func drawCheckMark(canvas *Canvas, x, y, width, height int, color Color) {
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	rasterizer := vector.NewRasterizer(width, height)
	points := [][2]float32{
		{0.41, 0.62},
		{0.22, 0.43},
		{0.15, 0.50},
		{0.41, 0.76},
		{0.87, 0.30},
		{0.80, 0.23},
	}
	rasterizer.MoveTo(points[0][0]*float32(width), points[0][1]*float32(height))
	for _, point := range points[1:] {
		rasterizer.LineTo(point[0]*float32(width), point[1]*float32(height))
	}
	rasterizer.ClosePath()
	rasterizer.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	draw.DrawMask(
		canvas.drawable(),
		image.Rect(x, y, x+width, y+height),
		image.NewUniform(color.NRGBA()),
		image.Point{},
		mask,
		image.Point{},
		draw.Over,
	)
}

func (b *CheckBox) Checked() bool { return b.checked }

// SetChecked 设置选中状态，并在状态发生变化时派发 CheckChangeEvent。
func (b *CheckBox) SetChecked(checked bool) { b.setChecked(checked, true) }

func (b *CheckBox) setChecked(checked, dispatch bool) {
	if b.checked == checked {
		return
	}
	b.checked = checked
	b.ClassToggle(`checked`, checked)
	b.document.RequestPaint()
	if dispatch {
		b.Dispatch(CheckChangeEvent, CheckChangeArgs{Checked: checked})
	}
}

func (b *CheckBox) SetProp(key, value string) error {
	switch key {
	case `box-color`, `checked-box-color`, `mark-color`:
		parsed, err := ParseColor(value)
		if err != nil {
			return fmt.Errorf(`%s 属性不是颜色：%s`, key, value)
		}
		switch key {
		case `box-color`:
			b.boxColor, b.customBoxColor = parsed, true
		case `checked-box-color`:
			b.checkedBoxColor, b.customCheckedBoxColor = parsed, true
		case `mark-color`:
			b.markColor, b.customMarkColor = parsed, true
		}
		b.document.paintDirty = true
		return nil
	case `checked`:
		checked, err := parseBooleanAttribute(`checked`, value)
		if err != nil {
			return err
		}
		b.setChecked(checked, false)
		return nil
	default:
		return b.Base().SetProp(key, value)
	}
}

func (b *CheckBox) OnChange(handler func(checked bool)) func() {
	return b.Listen(CheckChangeEvent, func(event *Event) {
		handler(event.Data[CheckChangeArgs]().Checked)
	})
}

var (
	progressTrackThemeColor = RegisterThemeColor(`--progress-track-color`, `#656b76`, `#43484f`)
	progressValueThemeColor = RegisterThemeColor(`--progress-value-color`, `var(--color-primary)`, `var(--color-primary)`)
)

// ProgressBar 是一个使用 [0,1] 表示完成比例的进度条。
// 它只绘制进度条本身，不接受子节点，也不处理输入事件。
type ProgressBar struct {
	BaseBox

	value float64

	displayValue float64

	trackColor Color
	valueColor Color

	themeTrackColor Color
	themeValueColor Color

	customTrackColor bool
	customValueColor bool

	painted bool

	indeterminate        bool
	indeterminatePhase   float64
	indeterminateForward bool

	valueTransition     *Transition[float64]
	cancelIndeterminate func()
}

func init() {
	Define(`progress`, true, NewProgressBar)
}

func NewProgressBar(doc *Document) *ProgressBar {
	trackColor := doc.ResolveThemeColor(progressTrackThemeColor)
	valueColor := doc.ResolveThemeColor(progressValueThemeColor)
	return &ProgressBar{
		BaseBox:         NewBaseBox(doc, `progress`),
		trackColor:      trackColor,
		valueColor:      valueColor,
		themeTrackColor: trackColor,
		themeValueColor: valueColor,
	}
}

// intrinsicSize 根据当前字号计算默认尺寸。
func (b *ProgressBar) intrinsicSize() (width, height int) {
	fontSize := int(b.computedStyles.FontSize.Number())
	return max(1, fontSize*8), max(1, fontSize/2)
}

// Calc 使用进度条图形作为未指定尺寸时的固有尺寸。
func (b *ProgressBar) Calc(availWidth, availHeight int, constraints Constraints) {
	size := b.resolveDimensions(constraints)
	intrinsicWidth, intrinsicHeight := b.intrinsicSize()
	b.layoutBox.Width = resolveSize(
		size.Width,
		availWidth,
		false,
		constrainNaturalSize(intrinsicWidth+b.HorizontalInsets(), availWidth, constraints.UnboundedWidth),
	)
	b.layoutBox.Height = resolveSize(
		size.Height,
		availHeight,
		false,
		constrainNaturalSize(intrinsicHeight+b.VerticalInsets(), availHeight, constraints.UnboundedHeight),
	)
}

// Draw 先绘制完整轨道，再绘制完成部分。
func (b *ProgressBar) Draw(canvas *Canvas) {
	b.BaseBox.draw(canvas, false)
	firstPaint := !b.painted
	b.painted = true
	if firstPaint && b.indeterminate {
		b.animateIndeterminate()
	}

	x := b.InsetLeft()
	y := b.InsetTop()
	width := b.layoutBox.Width - b.HorizontalInsets()
	height := b.layoutBox.Height - b.VerticalInsets()
	if width <= 0 || height <= 0 {
		return
	}

	trackColor := b.trackColor
	if !b.customTrackColor && b.trackColor == b.themeTrackColor {
		trackColor = b.document.ResolveThemeColor(progressTrackThemeColor)
		b.trackColor = trackColor
		b.themeTrackColor = trackColor
	}
	valueColor := b.valueColor
	if !b.customValueColor && b.valueColor == b.themeValueColor {
		valueColor = b.document.ResolveThemeColor(progressValueThemeColor)
		b.valueColor = valueColor
		b.themeValueColor = valueColor
	}
	canvas.FillRect(x, y, width, height, trackColor)
	if b.indeterminate {
		segmentWidth := max(1, width/4)
		// 运动范围两端都在轨道外，使色块到达端点时完全消失。
		travel := width + segmentWidth
		segmentX := x - segmentWidth + int(math.Round(float64(travel)*b.indeterminatePhase))
		// Canvas 只按整张画布裁剪；这里还要限制在进度条轨道内。
		visibleStart := max(x, segmentX)
		visibleEnd := min(x+width, segmentX+segmentWidth)
		if visibleStart < visibleEnd {
			canvas.FillRect(visibleStart, y, visibleEnd-visibleStart, height, valueColor)
		}
		return
	}
	valueWidth := int(math.Round(float64(width) * b.displayValue))
	valueWidth = min(width, max(0, valueWidth))
	if valueWidth > 0 {
		canvas.FillRect(x, y, valueWidth, height, valueColor)
	}
}

// Value 返回当前完成比例，取值范围为 [0,1]。
func (b *ProgressBar) Value() float64 {
	return b.value
}

// SetValue 设置完成比例。value 必须位于 [0,1] 内；NaN、无穷值和
// 越界值会返回错误并保留原值。数值真实变化时会请求重绘。
func (b *ProgressBar) SetValue(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return fmt.Errorf(`progress value 必须在 [0,1] 内：%v`, value)
	}
	if b.value == value {
		return nil
	}
	b.value = value
	transition := b.progressTransition()
	if b.indeterminate || !b.painted {
		transition.SetValue(value)
	} else {
		transition.SetTarget(value)
	}
	b.document.RequestPaint()
	return nil
}

// 按需创建确定进度过渡；不确定模式的循环与停留独立管理。
func (b *ProgressBar) progressTransition() *Transition[float64] {
	if b.valueTransition == nil {
		b.valueTransition = b.document.NewTransition(b.displayValue, TransitionOptions[float64]{
			Duration: progressAnimationDuration, Easing: EaseOut, Animator: NumberAnimator,
			OnUpdate: func(value float64) { b.displayValue = value; b.document.RequestPaint() },
		})
	}
	return b.valueTransition
}

// Indeterminate 返回进度条是否处于不确定模式。
func (b *ProgressBar) Indeterminate() bool {
	return b.indeterminate
}

// SetIndeterminate 切换不确定模式。不确定模式只表示任务仍在进行，
// Value 仍可保存下次切回确定模式时要显示的完成比例。
func (b *ProgressBar) SetIndeterminate(indeterminate bool) {
	if b.indeterminate == indeterminate {
		return
	}
	if b.cancelIndeterminate != nil {
		b.cancelIndeterminate()
		b.cancelIndeterminate = nil
	}
	if b.valueTransition != nil {
		b.valueTransition.Cancel()
	}
	b.indeterminate = indeterminate
	b.indeterminatePhase = 0
	b.indeterminateForward = true
	if indeterminate {
		b.animateIndeterminate()
	} else {
		b.progressTransition().SetValue(b.value)
	}
	b.document.RequestPaint()
}

// 在轨道内往返移动色块；每次完成后续订下一段，不要求公共动画支持循环。
func (b *ProgressBar) animateIndeterminate() {
	if !b.painted || !b.indeterminate {
		return
	}
	from, to := 0.0, 1.0
	if !b.indeterminateForward {
		from, to = to, from
	}
	phaseAt := NumberAnimator(from, to)
	b.cancelIndeterminate = b.document.Animate(AnimationOptions{
		Duration: progressIndeterminateDuration,
		Easing:   EaseInOut,
		OnUpdate: func(progress float64) {
			b.indeterminatePhase = phaseAt(progress)
			b.document.RequestPaint()
		},
		OnComplete: func() {
			b.cancelIndeterminate = nil
			if !b.indeterminate {
				return
			}
			// 到达端点后停止动画帧，只保留一次生命周期绑定的定时器。
			b.cancelIndeterminate = b.document.SetTimeout(progressIndeterminatePause, func() {
				b.cancelIndeterminate = nil
				if !b.indeterminate {
					return
				}
				b.indeterminateForward = !b.indeterminateForward
				b.animateIndeterminate()
			})
		},
	})
}

func (b *ProgressBar) SetProp(key, value string) error {
	switch key {
	case `value`:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf(`progress value 属性不是数值：%s`, value)
		}
		return b.SetValue(parsed)
	case `indeterminate`:
		parsed, err := parseBooleanAttribute(`indeterminate`, value)
		if err != nil {
			return err
		}
		b.SetIndeterminate(parsed)
		return nil
	case `track-color`, `value-color`:
		parsed, err := ParseColor(value)
		if err != nil {
			return fmt.Errorf(`%s 属性不是颜色：%s`, key, value)
		}
		if key == `track-color` {
			b.trackColor = parsed
			b.customTrackColor = true
		} else {
			b.valueColor = parsed
			b.customValueColor = true
		}
		b.document.RequestPaint()
		return nil
	default:
		return b.Base().SetProp(key, value)
	}
}

//go:embed assets/select.html
var selectAssets embed.FS

// SelectChangeEvent 在 SelectBox 的已选索引发生变化后派发。
var SelectChangeEvent = RegisterEventType()

type SelectChangeArgs struct {
	Index int
}

// SelectBox 是一个使用模态列表选择预定义选项的叶子组件。
type SelectBox struct {
	BaseBox

	items       []string
	index       int
	placeholder string
	disabled    bool
	popup       *Document
	popupView   _SelectPopupView
}

type _SelectPopupView struct {
	root  Box
	list  *List `css:"#list"`
	empty Box   `css:"#empty"`
}

type _SelectItemView struct {
	root Box
	text *Text `css:"text"`
}

func init() {
	Define(`select`, true, NewSelectBox)
}

func NewSelectBox(doc *Document) *SelectBox {
	b := &SelectBox{
		BaseBox:     NewBaseBox(doc, `select`),
		index:       -1,
		placeholder: `请选择`,
	}
	b.Listen(StickDownEvent, func(event *Event) {
		if event.Stick.Name != A || event.Stick.Repeat {
			return
		}
		event.StopPropagation()
		if !b.disabled {
			b.Open()
		}
	})
	return b
}

// intrinsicSize 根据当前字号计算默认尺寸。
func (b *SelectBox) intrinsicSize() (width, height int) {
	fontSize := int(b.computedStyles.FontSize.Number())
	return max(1, fontSize*9), max(1, fontSize*3/2)
}

func (b *SelectBox) Calc(availWidth, availHeight int, constraints Constraints) {
	size := b.resolveDimensions(constraints)
	intrinsicWidth, intrinsicHeight := b.intrinsicSize()
	b.layoutBox.Width = resolveSize(
		size.Width,
		availWidth,
		false,
		constrainNaturalSize(intrinsicWidth+b.HorizontalInsets(), availWidth, constraints.UnboundedWidth),
	)
	b.layoutBox.Height = resolveSize(
		size.Height,
		availHeight,
		false,
		constrainNaturalSize(intrinsicHeight+b.VerticalInsets(), availHeight, constraints.UnboundedHeight),
	)
}

// Draw 绘制当前值或占位文字，以及右侧的下拉提示。
func (b *SelectBox) Draw(canvas *Canvas) {
	b.BaseBox.draw(canvas, false)

	width := b.layoutBox.Width - b.HorizontalInsets()
	height := b.layoutBox.Height - b.VerticalInsets()
	if width <= 0 || height <= 0 {
		return
	}

	text := b.placeholder
	color := b.computedStyles.Color
	if selected, ok := b.Selected(); ok {
		text = selected
	}

	faces := b.document.LoadFaces(b)
	fontSize := max(1, int(b.computedStyles.FontSize.Number()))
	arrowWidth := min(width, fontSize*1)
	content := canvas.Offset(b.InsetLeft(), b.InsetTop())
	content.DrawString(text, faces, color)
	content.Offset(max(0, width-arrowWidth), 0).DrawString(`▼`, faces, color)
}

func (b *SelectBox) SetItems(items []string) {
	b.items = slices.Clone(items)
	if b.popup != nil {
		b.Close()
	}
	if b.index >= len(b.items) {
		b.setIndex(-1)
	} else {
		b.document.RequestPaint()
	}
}

func (b *SelectBox) Items() []string {
	return slices.Clone(b.items)
}

func (b *SelectBox) Index() int {
	return b.index
}

func (b *SelectBox) SetIndex(index int) error {
	if index < -1 || index >= len(b.items) {
		return fmt.Errorf(`select 索引超出范围：%d`, index)
	}
	b.setIndex(index)
	return nil
}

func (b *SelectBox) setIndex(index int) {
	if b.index == index {
		return
	}
	b.index = index
	b.document.RequestPaint()
	b.Dispatch(SelectChangeEvent, SelectChangeArgs{Index: index})
}

func (b *SelectBox) Selected() (string, bool) {
	if b.index < 0 || b.index >= len(b.items) {
		return ``, false
	}
	return b.items[b.index], true
}

func (b *SelectBox) Disabled() bool {
	return b.disabled
}

func (b *SelectBox) SetDisabled(disabled bool) {
	if b.disabled == disabled {
		return
	}
	b.disabled = disabled
	b.ClassToggle(`disabled`, disabled)
	if disabled {
		b.Close()
	}
}

func (b *SelectBox) SetProp(key, value string) error {
	switch key {
	case `placeholder`:
		b.placeholder = value
		b.document.RequestPaint()
		return nil
	case `disabled`:
		disabled, err := parseBooleanAttribute(`disabled`, value)
		if err != nil {
			return err
		}
		b.SetDisabled(disabled)
		return nil
	default:
		return b.Base().SetProp(key, value)
	}
}

func (b *SelectBox) Open() {
	if b.disabled || b.popup != nil {
		return
	}
	if b.document.App() == nil {
		panic(`SelectBox.Open 未绑定 App`)
	}

	doc := b.document.App().NewPopup(selectAssets, `assets/select.html`, b.document)
	b.popup = doc
	b.popupView = _SelectPopupView{}
	doc.Bind(&b.popupView)

	items := slices.Clone(b.items)
	b.popupView.list.SetItems(len(items), func() (Box, *_SelectItemView) {
		view := doc.Unmarshal[_SelectItemView](`<block class="item"><text></text></block>`)
		return view.root, view
	}, func(view *_SelectItemView, index int) {
		view.text.SetText(items[index])
	})

	if len(items) == 0 {
		mustSetProp(b.popupView.list, `display`, `false`)
	} else {
		mustSetProp(b.popupView.empty, `display`, `false`)
	}

	if b.index >= 0 {
		row := min(b.index, b.popupView.list.rows-1)
		b.popupView.list.SetIndex(row, 0, b.index-row)
	} else {
		b.popupView.list.Deselect()
	}

	b.popupView.root.Listen(StickDownEvent, b.handlePopupStickDown)
	b.popupView.list.Activate()
}

func (b *SelectBox) handlePopupStickDown(event *Event) {
	if b.popup == nil {
		return
	}
	switch event.Stick.Name {
	case A:
		if event.Stick.Repeat {
			return
		}
		index := b.popupView.list.DataIndex()
		if index < 0 {
			return
		}
		event.StopPropagation()
		b.Close()
		_ = b.SetIndex(index)
	case B:
		if event.Stick.Repeat {
			return
		}
		event.StopPropagation()
		b.Close()
	}
}

func (b *SelectBox) Close() {
	if b == nil || b.popup == nil {
		return
	}
	doc := b.popup
	b.popup = nil
	b.popupView = _SelectPopupView{}
	doc.Close()
}

func (b *SelectBox) Opened() bool {
	return b != nil && b.popup != nil
}

func (b *SelectBox) OnChange(handler func(index int)) func() {
	return b.Listen(SelectChangeEvent, func(event *Event) {
		args := event.Data[SelectChangeArgs]()
		handler(args.Index)
	})
}

//go:embed assets/alert_dialog.html
var alertDialogAssets embed.FS

type AlertDialogOptions struct {
	// 标题与正文。
	Title       string
	Description string

	// 以下均可为空。

	// “确定”按钮文本。默认为“确定”。
	ActionText    string
	ActionVariant ButtonVariant
	OnAction      func()

	// 以下均可为空。

	CancelText string
	OnCancel   func()
}

// AlertDialog 是通过 Popup 文档显示的模态警告对话框。
type AlertDialog struct {
	document *Document
	view     _AlertDialogView

	onAction  func()
	onCancel  func()
	hasCancel bool
	closed    bool
}

type _AlertDialogView struct {
	root                Box
	popup               Box     `css:"#popup"`
	title               *Text   `css:"#title"`
	description         *Text   `css:"#description"`
	descriptionViewport Box     `css:"#description-viewport"`
	descriptionGap      Box     `css:"#description-gap"`
	cancel              *Button `css:"#cancel"`
	cancelText          *Text   `css:"#cancel-text"`
	action              *Button `css:"#action"`
	actionText          *Text   `css:"#action-text"`
}

// ShowAlertDialog 创建并立即显示一个 Alert Dialog。
func (app *App) ShowAlertDialog(opener *Document, options AlertDialogOptions) *AlertDialog {
	if options.Title == `` {
		panic(`AlertDialog Title 不能为空`)
	}
	if options.CancelText == `` && options.OnCancel != nil {
		panic(`AlertDialog 设置 OnCancel 时必须同时设置 CancelText`)
	}
	if options.ActionText == `` {
		options.ActionText = `确定`
	}
	if options.ActionVariant == `` {
		options.ActionVariant = ButtonPrimary
	}
	switch options.ActionVariant {
	case ButtonNormal, ButtonPrimary, ButtonDestructive:
	default:
		panic(`AlertDialog ActionVariant 无效：` + options.ActionVariant)
	}

	doc := app.NewPopup(alertDialogAssets, `assets/alert_dialog.html`, opener)
	dialog := &AlertDialog{
		document:  doc,
		onAction:  options.OnAction,
		onCancel:  options.OnCancel,
		hasCancel: options.CancelText != ``,
	}
	doc.Bind(&dialog.view)

	dialog.view.title.SetText(options.Title)
	dialog.view.description.SetText(options.Description)
	dialog.view.actionText.SetText(options.ActionText)
	if err := dialog.view.action.SetVariant(options.ActionVariant); err != nil {
		panic(err)
	}

	if options.Description == `` {
		mustSetProp(dialog.view.popup, `height`, `220`)
		mustSetProp(dialog.view.descriptionViewport, `display`, `false`)
		mustSetProp(dialog.view.descriptionGap, `display`, `false`)
	}
	if options.CancelText == `` {
		mustSetProp(dialog.view.cancel, `display`, `false`)
	} else {
		dialog.view.cancelText.SetText(options.CancelText)
	}

	dialog.view.root.Listen(StickDownEvent, dialog.handleStickDown)
	dialog.view.root.Activate()
	return dialog
}

func mustSetProp(box Box, key, value string) {
	if err := box.SetProp(key, value); err != nil {
		panic(err)
	}
}

func (d *AlertDialog) handleStickDown(event *Event) {
	if d.closed {
		return
	}
	switch event.Stick.Name {
	case Up:
		d.view.description.ScrollLineUp()
		event.StopPropagation()
	case Down:
		d.view.description.ScrollLineDown()
		event.StopPropagation()
	case A:
		if event.Stick.Repeat {
			return
		}
		event.StopPropagation()
		d.finish(d.onAction)
	case B:
		if event.Stick.Repeat || !d.hasCancel {
			return
		}
		event.StopPropagation()
		d.finish(d.onCancel)
	}
}

func (d *AlertDialog) finish(callback func()) {
	if d.closed {
		return
	}
	d.Close()
	if callback != nil {
		callback()
	}
}

// Close 关闭对话框，不触发 Action 或 Cancel 回调。
func (d *AlertDialog) Close() {
	if d == nil || d.closed {
		return
	}
	d.closed = true
	d.document.Close()
}

func (d *AlertDialog) Closed() bool {
	return d == nil || d.closed
}

//------------------------------------------------------------------------------

type List struct {
	BaseBox

	// 如果指定了，则列表项的高度由此决定。
	// 如果没指定，则会平均分。
	rowHeight int

	// 指定了 max-rows 的时候此为 true，决定是否要收缩容器高度。
	// max-rows 使用和 rows 相同的槽位容量，但在数据不足时收缩高度。
	shrinkRows bool

	bind func(user any, index int)

	_ListState
}

var (
	// 列表选中项发生了改变。
	ListSelectionChange = RegisterEventType()
)

// ListSelectionAware 可由 SetItems 返回的 user 实现。List 会在对应
// 列表项被选中或取消选中时同步通知；未实现该接口时不会执行额外操作。
type ListSelectionAware interface {
	ListSelectionChanged(selected bool)
}

type _ListState struct {
	// 列表的数据总量。
	count int

	// 列表的行数。
	rows int
	// 列表的列数。
	cols int

	// child selection index
	rowIndex int
	colIndex int

	// 虚拟滚动的item顶部起始元素。
	itemOffset int
}

func NewList(doc *Document) *List {
	list := &List{
		BaseBox:    NewBaseBox(doc, `list`),
		rows:       1,
		cols:       1,
		rowIndex:   -1,
		colIndex:   -1,
		itemOffset: 0,
	}

	list.Listen(StickDownEvent, func(e *Event) {
		list.navigate(e)
	})

	return list
}

func init() {
	Define(`list`, false, NewList)
}

// TODO 取消重复计算，大小不变的情况下只需要计算一次。
func (b *List) Calc(availWidth, availHeight int, constraints Constraints) {
	size := b.resolveDimensions(constraints)
	// 行列间距统一来自样式，支持 CSS 层叠和动态属性更新。
	gap := max(0, b.computedStyles.Gap)
	var (
		boxMaxWidth  = Iif(size.Width.IsNumber(), int(size.Width.Number()), availWidth)
		boxMaxHeight = Iif(size.Height.IsNumber(), int(size.Height.Number()), availHeight)

		contentAvailWidth  = boxMaxWidth - (b.HorizontalInsets() + (b.cols-1)*gap)
		contentAvailHeight = boxMaxHeight - (b.VerticalInsets() + (b.rows-1)*gap)

		offsetX = b.InsetLeft()
		offsetY = b.InsetTop()

		// child 的高度应受槽位高度约束，而不应该反过来根据 child 高度计算 gap。否则会有几个问题：
		//  - rows 不再能保证准确显示指定数量的行；
		//  - 不同批次虚拟化数据可能导致 gap 和布局跳动；
		//  - 超高 child 会挤压其他行，滚动和选中位置也会变得不稳定；
		//  - child 总高度超过视口时，无法通过“算 gap”合理解决。
		avgHeight = average(contentAvailHeight, b.rows)
		avgWidth  = average(contentAvailWidth, b.cols)
	)
	if b.rowHeight > 0 {
		avgHeight = b.rowHeight
	}

	activeCount := min(b.count, len(b.children))
	for i := range activeCount {
		b.children[i].(*_ListItem).bindData()
	}

	// 指定宽度或父布局要求占满时，槽位平均分配全部可用宽度。
	// 否则先测量 item 的自然宽度，再以最宽 item 作为等宽槽位宽度。
	fillWidth := size.Width.IsNumber() || constraints.PrefersMaxWidth
	if !fillWidth {
		avgWidth = 0
		maxSlotWidth := average(contentAvailWidth, b.cols)
		for i := range activeCount {
			child := b.children[i].(*_ListItem)
			child.forceCalc(0, 0, maxSlotWidth, avgHeight, false)
			childWidth := child.HorizontalInsets() + child.children[0].Base().layoutBox.Width
			avgWidth = max(avgWidth, childWidth)
		}
	}

	if activeCount > 0 {
		for i := range activeCount {
			child := b.children[i].(*_ListItem)
			child.forceCalc(offsetX, offsetY, avgWidth, avgHeight, fillWidth)
			// 需要换行了
			if (i+1)%b.cols == 0 {
				offsetX = b.InsetLeft()
				offsetY += gap
				offsetY += avgHeight
			} else {
				offsetX += gap
				offsetX += avgWidth
			}
		}
	}

	visibleCols := min(b.cols, activeCount)
	actualWidth := b.HorizontalInsets()
	if visibleCols > 0 {
		actualWidth += visibleCols*avgWidth + (visibleCols-1)*gap
	}
	b.layoutBox.Width = resolveSize(size.Width, availWidth, constraints.PrefersMaxWidth, min(availWidth, actualWidth))
	if b.shrinkRows && !constraints.FixedHeight.IsNumber() {
		visibleRows := min(b.rows, divideRoundUp(b.count, b.cols))
		visibleGaps := max(visibleRows-1, 0)
		b.layoutBox.Height = b.VerticalInsets() + visibleRows*avgHeight + visibleGaps*gap
	} else {
		b.layoutBox.Height = resolveSize(size.Height, availHeight, constraints.PrefersMaxHeight, offsetY+b.InsetBottom())
	}

	// 可能还未初始化。
	if len(b.children) > 0 {
		b.adjust()
	}
}

func divideRoundUp(value, divisor int) int {
	if value <= 0 {
		return 0
	}
	return (value + divisor - 1) / divisor
}

// 为什么用浮点？
// 假设是 14 / 5，则一个元素只能等于 2
// 如果是浮点，则是 14 / 5 ≈ 2.8，round 到 3
func average(all, count int) int {
	return int(math.Round(float64(all) / float64(count)))
}

// 因为要实现虚拟draw方法，所以有它的存在。
type _ListItem struct {
	BaseBox

	list *List

	user any

	// 在列表中的位置。
	// rowIndex*cols + colIndex + topIndex == item数据
	rowIndex int
	colIndex int

	boundDataIndex int
}

func _NewListItem(doc *Document) *_ListItem {
	box := &_ListItem{BaseBox: NewBaseBox(doc, `list-item`), boundDataIndex: -1}
	box._EventTarget.box = box
	return box
}

func (b *_ListItem) Draw(canvas *Canvas) {
	// 槽位自身的 outline 可以画到边界外，但列表项内容必须限制在
	// 槽位内，避免超宽文本或其它子内容覆盖相邻列表项。
	b.Base().draw(canvas, false)
	clipped := canvas.Clip(0, 0, b.layoutBox.Width, b.layoutBox.Height)
	for _, child := range b.children {
		if !displaying(child) {
			continue
		}
		layout := child.Base().layoutBox
		child.Draw(clipped.Offset(layout.X, layout.Y))
	}
	// canvas.SaveToFile(fmt.Sprintf(`%d.png`, b.itemIndex()))
}

func (b *_ListItem) dataIndex() int {
	return b.rowIndex*b.list.cols + b.colIndex + b.list.itemOffset
}

func (b *_ListItem) bindData() {
	// 没有数据的项实际是被隐藏的，被隐藏的项不会参与计算。
	// 所以如果代码运行到了这里，那一定是出现了内部逻辑错误。
	if b.dataIndex() < b.list.count {
		dataIndex := b.dataIndex()
		changed := b.boundDataIndex != dataIndex
		if !changed {
			return
		}
		if b.selected() {
			b.notifySelection(false)
		}
		// 提前绑定上去才能提供数据、提供计算支撑。
		// TODO 现在是处理 calc 中，如果限定了尺寸的话，
		// 其实是不需要此刻 bind 的，Draw 的时候 bind 才比较好。
		// 因为其它控件需要calc的时候此控件不一定需要。
		//
		// 而且，如果项目过多，可能导致bind触发过多的RequestPaint阻塞队列？
		// 队列满了的话，会不会死在这里？
		b.list.bind(b.user, dataIndex)
		b.boundDataIndex = dataIndex
		if b.selected() {
			b.notifySelection(true)
		}
	}
}

func (b *_ListItem) selected() bool {
	return b.ClassContains(`selected`)
}

func (b *_ListItem) notifySelection(selected bool) {
	if aware, ok := b.user.(ListSelectionAware); ok {
		aware.ListSelectionChanged(selected)
	}
}

func (b *_ListItem) setSelected(selected bool) {
	if selected {
		b.ClassAdd(`selected`)
	} else {
		b.ClassRemove(`selected`)
	}
	b.notifySelection(selected)
}

func (b *_ListItem) forceCalc(x, y int, contentAvailWidth, avgHeight int, prefersMaxWidth bool) {
	base := b.Base()
	base.layoutBox.X = x
	base.layoutBox.Y = y
	base.layoutBox.Width = contentAvailWidth
	base.layoutBox.Height = avgHeight

	childContentAvailWidth := contentAvailWidth - b.HorizontalInsets()
	childContentAvailHeight := avgHeight - b.VerticalInsets()

	child := base.children[0]
	base = child.Base()
	base.layoutBox.Width = childContentAvailWidth
	base.layoutBox.Height = childContentAvailHeight

	child.Calc(childContentAvailWidth, childContentAvailHeight, Constraints{
		ParentContentWidth:  max(0, childContentAvailWidth),
		ParentContentHeight: max(0, childContentAvailHeight),
		PrefersMaxWidth:     prefersMaxWidth,
		PrefersMaxHeight:    true,
	})
	child.Base().layoutBox.X = b.InsetLeft()
	child.Base().layoutBox.Y = b.InsetTop()
}

func (b *List) SetProp(key, value string) error {
	switch key {
	case `rows`:
		b.rows = Must1(strconv.Atoi(value))
		b.shrinkRows = false
		return nil
	case `max-rows`:
		b.rows = Must1(strconv.Atoi(value))
		b.shrinkRows = true
		return nil
	case `row-height`:
		b.rowHeight = Must1(strconv.Atoi(value))
		return nil
	case `cols`:
		b.cols = Must1(strconv.Atoi(value))
		return nil
	default:
		return b.BaseBox.SetProp(key, value)
	}
}

// 设置滚动盒子的内容。
//
//   - count 元素个数
//   - create 给元素创建视图
//   - bind 绑定元素到视图
func (b *List) SetItems[T any](count int, create func() (root Box, user T), bind func(user T, index int)) {
	b._setItems(count,
		func() (root Box, user any) {
			return create()
		},
		func(user any, index int) {
			bind(user.(T), index)
		},
	)
	b.Dispatch(ListSelectionChange, nil)
}

func (b *List) _setItems(count int, create func() (root Box, user any), bind func(user any, index int)) {
	if child := b.selectedChild(b._ListState); child != nil {
		child.setSelected(false)
	}
	b.children = nil
	b.count = count
	b.bind = bind
	b.rowIndex = -1
	b.colIndex = 0
	b.itemOffset = 0

	// 始终创建指定的行列数，但是最后一行可能个数不够。
	// 滚动的时候会自动计算并隐藏。
	for r := range b.rows {
		for c := range b.cols {
			box, user := create()
			wrapper := _NewListItem(b.document)
			wrapper.user = user
			wrapper.list = b
			wrapper.rowIndex = r
			wrapper.colIndex = c
			wrapper.AppendChild(box)
			wrapper.notifySelection(false)
			b.AppendChild(wrapper)
		}
	}
}

func (b *List) navigate(event *Event) {
	name := event.Stick.Name

	if !(name == Up || name == Down || name == Left || name == Right) {
		return
	}

	oldState := b._ListState
	if !b._ListState.navigate(name) {
		return
	}

	b.selectionChanged(oldState)

	b.document.RequestPaint()
	event.StopPropagation()
	// 发送状态变化事件。
	b.Dispatch(ListSelectionChange, nil)
}

func (b *List) selectedChild(state _ListState) *_ListItem {
	childIndex := state.rowIndex*state.cols + state.colIndex
	if state.rowIndex < 0 || childIndex < 0 || childIndex >= len(b.children) {
		return nil
	}
	return b.children[childIndex].(*_ListItem)
}

func (b *List) selectionChanged(oldState _ListState) {
	if child := b.selectedChild(oldState); child != nil {
		child.setSelected(false)
	}
	if child := b.selectedChild(b._ListState); child != nil {
		// 虚拟槽位可能已经代表另一条数据；先换绑，再通知选中。
		child.bindData()
		child.setSelected(true)
	}
}

// navigate 计算一次导航后的选中状态。
// 返回值表示状态是否发生了变化。
func (b *_ListState) navigate(name KeyName) bool {
	old := *b

	switch name {
	case Up:
		switch {
		case b.rowIndex > 0:
			b.rowIndex--
		case b.rowIndex == 0:
			// 已经到了物理列表的顶部、但是还没有到虚拟列表的顶部。
			if b.itemOffset > 0 {
				b.itemOffset -= b.cols
			}
		}
	case Down:
		// 先加再判断错误
		if b.curDataRow() >= b.maxDataRow() {
			return false
		}
		// 行增加成功说明下一行一定有数据。
		b.rowIndex++
		// 最后一行可能没有整行数据，往前挪。
		for b.rowIndex*b.cols+b.colIndex+b.itemOffset > b.count-1 && b.rowIndex > 0 {
			b.colIndex--
		}
		// 超出列表行数了，回到最后一行，并滚动数据。
		if b.rowIndex > b.rows-1 {
			b.itemOffset += b.cols
			b.rowIndex--
		}
	case Left:
		// 左右可以翻页（只针对于1列的盒子）
		if b.cols == 1 {
			b.pageLeft()
		} else if b.colIndex > 0 {
			b.colIndex--
		}
	case Right:
		// 左右可以翻页（只针对于1列的盒子）
		if b.cols == 1 {
			b.pageRight()
		} else if b.rowIndex >= 0 {
			maxCol := b.cols - 1
			if b.curDataRow() == b.maxDataRow() && b.count%b.cols != 0 {
				maxCol = b.count%b.cols - 1
			}
			if b.colIndex < maxCol {
				b.colIndex++
			}
		}
	}

	return old != *b
}

func (b *_ListState) pageLeft() {
	if b.rowIndex < 0 {
		return
	}
	if b.itemOffset >= b.rows {
		b.itemOffset -= b.rows
		return
	}

	b.rowIndex = 0
	b.itemOffset = 0
}

func (b *_ListState) pageRight() {
	if b.rowIndex < 0 || b.count == 0 {
		return
	}
	if b.itemOffset+b.rows+b.rowIndex < b.count {
		b.itemOffset += b.rows
		return
	}

	b.rowIndex = min(b.rows-1, b.count-1)
	b.itemOffset = b.count - 1 - b.rowIndex
}

func (b *_ListState) curDataRow() int {
	return b.rowIndex + b.itemOffset/b.cols
}

// [0,rows-1]
func (b *_ListState) maxDataRow() int {
	return b.count/b.cols + Iif(b.count%b.cols > 0, 1, 0) - 1
}

func (b *List) adjust() {
	for r := range b.rows {
		for c := range b.cols {
			child := b.children[r*b.cols+c].(*_ListItem)
			display := child.dataIndex() <= b.count-1
			if displaying(child) != display {
				// TODO 可以不用重新排版
				child.SetProp(`display`, fmt.Sprint(display))
			}
		}
	}
}

// 返回当前选中的数据索引。
// 如果没有选中，返回-1。
func (b *List) DataIndex() int {
	if b.rowIndex < 0 {
		return -1
	}
	return b.rowIndex*b.cols + b.colIndex + b.itemOffset
}

// 暂时忽略错误。
func (b *List) SetIndex(rowIndex, colIndex, dataIndexOffset int) {
	if rowIndex < 0 || rowIndex >= b.rows {
		return
	}
	if colIndex < 0 || colIndex >= b.cols {
		return
	}
	if rowIndex*b.cols+colIndex+dataIndexOffset >= b.count {
		return
	}

	oldState := b._ListState
	b.rowIndex = rowIndex
	b.colIndex = colIndex
	b.itemOffset = dataIndexOffset

	if oldState != b._ListState {
		b.selectionChanged(oldState)
	}

	b.document.RequestPaint()
}

// 返回数据总量。
func (b *List) DataCount() int {
	return b.count
}

// 返回当前的可视行号（非数据行号）。
func (b *List) RowIndex() int {
	return b.rowIndex
}

func (b *List) DataRowIndex() int {
	return b.curDataRow()
}

// 取消选中当前的选中项。
func (b *List) Deselect() {
	oldState := b._ListState
	b.rowIndex = -1
	b.colIndex = 0
	// 好像可以不用归位？
	b.itemOffset = 0
	b.selectionChanged(oldState)

	b.document.RequestPaint()
}

// 返回当前的选中状态信息，可用于后期恢复。
func (b *List) GetState() any {
	return b._ListState
}

// 用于恢复之前的选中状态。
// 如果重新调用过 SetItems，此前的状态不再有效。
func (b *List) SetState(state any) {
	st, ok := state.(_ListState)
	if !ok {
		panic(`无效状态`)
	}

	oldState := b._ListState
	b._ListState = st
	if oldState != b._ListState {
		b.selectionChanged(oldState)
	}

	b.Dispatch(ListSelectionChange, nil)

	b.document.RequestPaint()
}

//------------------------------------------------------------------------------

// Scroll 是承载任意内容树的像素级滚动视口。
// 它只接受一个直接子节点；子节点内部可以使用任意普通布局。
type Scroll struct {
	BaseBox

	direction string
	step      int
	maxX      int
	maxY      int

	// targetX 是“最终想滚到哪里”，
	// offsetX 是“当前画面实际滚到哪里”。
	//
	// 如果不开平滑滚动，则两者一样；
	// 如果开了平滑滚动，则offset可能处于动画的中间值。
	offset _ScrollPosition
	target _ScrollPosition

	offsetTransition *Transition[_ScrollPosition]
}

type _ScrollPosition struct{ X, Y int }

func (p _ScrollPosition) clamp(maxX, maxY int) _ScrollPosition {
	return _ScrollPosition{
		X: min(max(0, p.X), maxX),
		Y: min(max(0, p.Y), maxY),
	}
}

const scrollSmoothDuration = 180 * time.Millisecond

type ScrollChangeArgs struct {
	X, Y int
}

var ScrollChange = RegisterEventType()

func NewScroll(doc *Document) *Scroll {
	scroll := &Scroll{
		BaseBox:   NewBaseBox(doc, `scroll`),
		direction: `vertical`,
		step:      32,
	}
	scroll.Listen(StickDownEvent, scroll.handleStickDown)
	return scroll
}

func init() {
	Define(`scroll`, false, NewScroll)
}

func (b *Scroll) validateChildren() error {
	if len(b.children) > 1 {
		return fmt.Errorf(`scroll 只能包含一个直接子节点`)
	}
	return nil
}

func (b *Scroll) SetProp(key, value string) error {
	switch key {
	case `direction`:
		switch value {
		case `vertical`, `horizontal`, `both`:
			b.direction = value
			return nil
		default:
			return fmt.Errorf(`无效的 scroll direction：%s`, value)
		}
	case `step`:
		step, err := strconv.Atoi(value)
		if err != nil || step <= 0 {
			return fmt.Errorf(`无效的 scroll step：%s`, value)
		}
		b.step = step
		return nil
	case `smooth`:
		smooth, err := parseBooleanAttribute(`smooth`, value)
		if err != nil {
			return err
		}
		b.SetSmooth(smooth)
		return nil
	default:
		return b.BaseBox.SetProp(key, value)
	}
}

func (b *Scroll) scrollsHorizontally() bool {
	return b.direction == `horizontal` || b.direction == `both`
}

func (b *Scroll) scrollsVertically() bool {
	return b.direction == `vertical` || b.direction == `both`
}

func (b *Scroll) Calc(availWidth, availHeight int, constraints Constraints) {
	size := b.resolveDimensions(constraints)

	provisionalWidth := availWidth
	if size.Width.IsNumber() {
		provisionalWidth = int(size.Width.Number())
	}
	provisionalHeight := availHeight
	if size.Height.IsNumber() {
		provisionalHeight = int(size.Height.Number())
	}
	contentWidth := max(0, provisionalWidth-b.HorizontalInsets())
	contentHeight := max(0, provisionalHeight-b.VerticalInsets())

	var child Box
	if len(b.children) > 0 && displaying(b.children[0]) {
		child = b.children[0]
		b.measureChild(child, contentWidth, contentHeight)
	}

	naturalWidth, naturalHeight := b.HorizontalInsets(), b.VerticalInsets()
	if child != nil {
		layout := child.GetLayoutBox()
		naturalWidth += layout.Width
		naturalHeight += layout.Height
	}
	actualWidth := constrainNaturalSize(naturalWidth, availWidth, constraints.UnboundedWidth)
	actualHeight := constrainNaturalSize(naturalHeight, availHeight, constraints.UnboundedHeight)
	b.layoutBox.Width = max(0, resolveMeasuredSize(size.Width, availWidth, constraints.PrefersMaxWidth, actualWidth, constraints.UnboundedWidth))
	b.layoutBox.Height = max(0, resolveMeasuredSize(size.Height, availHeight, constraints.PrefersMaxHeight, actualHeight, constraints.UnboundedHeight))

	contentWidth = max(0, b.layoutBox.Width-b.HorizontalInsets())
	contentHeight = max(0, b.layoutBox.Height-b.VerticalInsets())
	if child != nil {
		b.measureChild(child, contentWidth, contentHeight)
		child.Base().layoutBox.X = b.InsetLeft()
		child.Base().layoutBox.Y = b.InsetTop()
		layout := child.GetLayoutBox()
		b.maxX = max(0, layout.Width-contentWidth)
		b.maxY = max(0, layout.Height-contentHeight)
	} else {
		b.maxX, b.maxY = 0, 0
	}
	if !b.scrollsHorizontally() {
		b.maxX = 0
	}
	if !b.scrollsVertically() {
		b.maxY = 0
	}
	oldTarget := b.target
	b.target = b.target.clamp(b.maxX, b.maxY)
	clampedOffset := b.offset.clamp(b.maxX, b.maxY)
	displayClamped := clampedOffset != b.offset
	if displayClamped {
		b.setDisplayedOffset(clampedOffset)
	}
	if b.offsetTransition != nil && (displayClamped || oldTarget != b.target) {
		b.offsetTransition.SetValue(clampedOffset)
		if b.Smooth() && clampedOffset != b.target {
			b.offsetTransition.SetTarget(b.target)
		}
	}
}

func (b *Scroll) measureChild(child Box, width, height int) {
	child.Calc(width, height, Constraints{
		ParentContentWidth:  width,
		ParentContentHeight: height,
		PrefersMaxWidth:     !b.scrollsHorizontally(),
		PrefersMaxHeight:    !b.scrollsVertically(),
		UnboundedWidth:      b.scrollsHorizontally(),
		UnboundedHeight:     b.scrollsVertically(),
	})
}

func (b *Scroll) Draw(canvas *Canvas) {
	b.BaseBox.draw(canvas, false)
	if len(b.children) == 0 || !displaying(b.children[0]) {
		return
	}
	width := max(0, b.layoutBox.Width-b.HorizontalInsets())
	height := max(0, b.layoutBox.Height-b.VerticalInsets())
	clipped := canvas.Clip(b.InsetLeft(), b.InsetTop(), width, height)
	child := b.children[0]
	layout := child.Base().layoutBox
	child.Draw(clipped.Offset(layout.X-b.offset.X, layout.Y-b.offset.Y))
}

func (b *Scroll) ScrollOffset() (x, y int) {
	return b.offset.X, b.offset.Y
}

func (b *Scroll) ScrollRange() (maxX, maxY int) {
	return b.maxX, b.maxY
}

func (b *Scroll) ScrollTo(x, y int) {
	b.scrollTo(x, y)
}

func (b *Scroll) ScrollBy(dx, dy int) {
	b.scrollTo(b.target.X+dx, b.target.Y+dy)
}

func (b *Scroll) scrollTo(x, y int) bool {
	if !b.scrollsHorizontally() {
		x = 0
	}
	if !b.scrollsVertically() {
		y = 0
	}
	target := (_ScrollPosition{X: x, Y: y}).clamp(b.maxX, b.maxY)
	if target == b.target {
		return false
	}
	b.target = target
	if b.offsetTransition != nil {
		b.offsetTransition.SetTarget(target)
	} else {
		b.setDisplayedOffset(target)
	}
	return true
}

func (b *Scroll) Smooth() bool {
	return b.offsetTransition != nil
}

func (b *Scroll) SetSmooth(smooth bool) {
	if b.Smooth() == smooth {
		return
	}
	if smooth {
		b.offsetTransition = b.newScrollTransition()
	} else {
		b.offsetTransition.SetValue(b.target)
		b.offsetTransition = nil
	}
}

func (b *Scroll) newScrollTransition() *Transition[_ScrollPosition] {
	return b.document.NewTransition(
		b.offset,
		TransitionOptions[_ScrollPosition]{
			Duration: scrollSmoothDuration,
			Easing:   EaseOut,
			Animator: func(from, to _ScrollPosition) func(float64) _ScrollPosition {
				x := NumberAnimator(float64(from.X), float64(to.X))
				y := NumberAnimator(float64(from.Y), float64(to.Y))
				return func(progress float64) _ScrollPosition {
					return _ScrollPosition{
						X: int(math.Round(x(progress))),
						Y: int(math.Round(y(progress))),
					}
				}
			},
			OnUpdate: b.setDisplayedOffset,
		},
	)
}

func (b *Scroll) setDisplayedOffset(position _ScrollPosition) {
	position = position.clamp(b.maxX, b.maxY)
	if position == b.offset {
		return
	}
	b.offset = position
	if b.document != nil {
		b.document.RequestPaint()
	}
	b.Dispatch(ScrollChange, ScrollChangeArgs{X: position.X, Y: position.Y})
}

func (b *Scroll) handleStickDown(event *Event) {
	dx, dy := 0, 0
	switch event.Stick.Name {
	case Left:
		dx = -b.step
	case Right:
		dx = b.step
	case Up:
		dy = -b.step
	case Down:
		dy = b.step
	default:
		return
	}
	if b.scrollTo(b.target.X+dx, b.target.Y+dy) {
		event.StopPropagation()
	}
}
