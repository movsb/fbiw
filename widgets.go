package fbiw

import (
	"embed"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"
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
	toggleTrackOffColor = ColorFromRGBA(101, 107, 118, 255)
	toggleTrackOnColor  = ColorFromRGBA(54, 183, 102, 255)
	toggleKnobColor     = ColorFromRGBA(255, 255, 255, 255)
)

const toggleAnimationDuration = 250 * time.Millisecond

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

	// 逻辑状态立即改变，滑块使用独立的显示进度。
	knobProgress float64
	painted      bool
	cancelTween  func()
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
		min(availWidth, intrinsicWidth+b.HorizontalInsets()),
	)
	b.layoutBox.Height = resolveSize(
		size.Height,
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
	b.painted = true

	trackColor := b.trackColor
	if b.checked {
		trackColor = b.checkedTrackColor
	}
	canvas.FillRect(trackX, trackY, trackWidth, trackHeight, trackColor)

	knobSize := min(trackHeight-inset*2, trackWidth-inset*2)
	travel := trackWidth - inset*2 - knobSize
	knobX := trackX + inset + int(math.Round(float64(travel)*b.knobProgress))
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
	b.animateKnob()
	if dispatch {
		b.Dispatch(ToggleChangeEvent, ToggleChangeArgs{Checked: checked})
	}
}

// 从当前显示位置转向新目标；先安排动画再派发状态事件，
// 使 OnChange 中再次切换状态时，可以正确取消本次动画。
func (b *Toggle) animateKnob() {
	if b.cancelTween != nil {
		b.cancelTween()
		b.cancelTween = nil
	}
	target := 0.0
	if b.checked {
		target = 1
	}
	doc := b.document
	// 初次显示直接呈现目标状态；是否挂载及相应的降级行为
	// 统一由 Document.Tween 负责，组件不读取文档或时钟状态。
	if !b.painted || b.knobProgress == target {
		b.knobProgress = target
		doc.RequestPaint()
		return
	}
	b.cancelTween = doc.Tween(TweenOptions{
		From:     b.knobProgress,
		To:       target,
		Duration: toggleAnimationDuration,
		Easing:   EaseOut,
		OnUpdate: func(value float64) {
			b.knobProgress = value
			doc.RequestPaint()
		},
		OnComplete: func() { b.cancelTween = nil },
	})
	doc.RequestPaint()
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
		case `checked-track-color`:
			b.checkedTrackColor = parsed
		case `knob-color`:
			b.knobColor = parsed
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

var (
	progressTrackColor = ColorFromRGBA(101, 107, 118, 255)
	progressValueColor = ColorFromRGBA(51, 88, 212, 255)
)

// ProgressBar 是一个使用 [0,1] 表示完成比例的确定进度条。
// 它只绘制进度条本身，不接受子节点，也不处理输入事件。
type ProgressBar struct {
	BaseBox

	value      float64
	trackColor Color
	valueColor Color
}

func init() {
	Define(`progress`, true, NewProgressBar)
}

func NewProgressBar(doc *Document) *ProgressBar {
	return &ProgressBar{
		BaseBox:    NewBaseBox(doc, `progress`),
		trackColor: progressTrackColor,
		valueColor: progressValueColor,
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
		min(availWidth, intrinsicWidth+b.HorizontalInsets()),
	)
	b.layoutBox.Height = resolveSize(
		size.Height,
		availHeight,
		false,
		min(availHeight, intrinsicHeight+b.VerticalInsets()),
	)
}

// Draw 先绘制完整轨道，再从左向右绘制已完成部分。
func (b *ProgressBar) Draw(canvas *Canvas) {
	b.BaseBox.draw(canvas, false)

	x := b.InsetLeft()
	y := b.InsetTop()
	width := b.layoutBox.Width - b.HorizontalInsets()
	height := b.layoutBox.Height - b.VerticalInsets()
	if width <= 0 || height <= 0 {
		return
	}

	canvas.FillRect(x, y, width, height, b.trackColor)
	valueWidth := int(math.Round(float64(width) * b.value))
	valueWidth = min(width, max(0, valueWidth))
	if valueWidth > 0 {
		canvas.FillRect(x, y, valueWidth, height, b.valueColor)
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
	b.document.RequestPaint()
	return nil
}

func (b *ProgressBar) SetProp(key, value string) error {
	switch key {
	case `value`:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf(`progress value 属性不是数值：%s`, value)
		}
		return b.SetValue(parsed)
	case `track-color`, `value-color`:
		parsed, err := ParseColor(value)
		if err != nil {
			return fmt.Errorf(`%s 属性不是颜色：%s`, key, value)
		}
		if key == `track-color` {
			b.trackColor = parsed
		} else {
			b.valueColor = parsed
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
	list  *Scroll `css:"#list"`
	empty Box     `css:"#empty"`
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
		min(availWidth, intrinsicWidth+b.HorizontalInsets()),
	)
	b.layoutBox.Height = resolveSize(
		size.Height,
		availHeight,
		false,
		min(availHeight, intrinsicHeight+b.VerticalInsets()),
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
