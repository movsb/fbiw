package fbiw

import (
	_ "embed"
	"errors"
	"fmt"
	"iter"
	"log"
	"math"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"

	_ "image/jpeg"
	_ "image/png"
)

type Box interface {
	Base() *BaseBox
	// 根据可用的宽度和高度计算自己实际的宽度和高度。
	// 如果 computed.{width,height} 有值，则直接用，
	// 表示被外界固定好了，而且不能覆盖。
	//   - 自己写：并把宽度和高度写到 layoutBox.{width, height}。
	//   - 父亲写：layoutBox.{x, y}。
	// 子元素在排版时，它需要知道自己是否应该默认占满父元素（类似html的div），
	// 第三个参数用来决定此行为。
	Calc(availableWidth, availableHeight int, constraints Constraints)
	// 根据自身的 layoutBox 直接画。
	// layoutBox 的 {x,y} 相对于父元素。
	// 所以除根元素外（因为它是0，0），其它在Draw之前都要调整canvas到offset，
	// 即：父元素在遍历子元素Draw的时候记得根据子元素的{x,y}作offset。
	Draw(canvas *Canvas)

	// 设置属性值。
	// 可以是CSS样式值、盒子自己提供的属性。
	SetProp(key, val string) error

	/// 慢慢地把所有盒子的基类已实现方法转移到这里。

	// 返回盒子当前的样式最终计算结果。
	//
	// 返回的是指针，不要尝试修改。
	GetComputedStyles() *Styles

	// 返回所属文档。
	Document() *Document
	// 返回孩子盒子列表。
	Children() []Box

	// 获取焦点。使其能接受键盘等事件处理。
	Activate()

	// 事件监听与分发。
	Listen(ty EventType, handler func(*Event), options EventOptions) func()
	Dispatch(event *Event)

	// 类名操作相关函数。
	ClassSet(class string)
	ClassAdd(class string)
	ClassRemove(class string)
	ClassContains(class string) bool
	ClassToggle(class string, force ...any)
}

type Constraints struct {
	PrefersMaxWidth  bool
	PrefersMaxHeight bool
}

var _ Box = (*BaseBox)(nil)

// 所有可布局盒子的基类。
//
// 注意：由于是直接内嵌的（非指针），而有些内部字段本身也需要引用此盒子本身，
// 这种情况无法初始化。这个步骤放到了 transformNode。
type BaseBox struct {
	ID  string
	Tag string

	// 因为class有内部变量需要初始化，不知道咋写，先暂时隐藏，并提供同名方法。
	class Class

	// 不同于form元素的name，这个name不用于css。
	// 不要求唯一，用于保存业务数据。
	Name string
	// 用于存放用户任意数据。
	dataset map[string]any

	document    *Document
	Parent      Box
	PrevSibling Box
	NextSibling Box
	children    []Box

	// 每一个盒子都是事件容器对象。
	// 但是盒子是否可处理事件本身与focusable不直接有关。
	_EventTarget

	// 来自内联样式的值和样式计算的最终结果。
	inlineStyles Styles
	// 注意，样式计算结束后，computedStyles不应该再被修改（除presetWidth百分比计算）。
	computedStyles Styles

	// 排版后的位置。
	// XY 相对于父亲可用区域的偏移，父亲写。
	// WH 是保存计算后真正使用的值。不要把这个值写回computedStyles，
	// 否则如何内容变化后重新排版不知道这个值是哪算用户设置还是上次的计算结果，
	// 于是导致计算错误。
	layoutBox Rect
}

// 创建一个基类盒子。
//
// 返回的不是指针。
// 所以不要在这里初始化内部循环引用（比如： eventTarget.box）。
func NewBaseBox(doc *Document, tagName string) BaseBox {
	return BaseBox{document: doc, Tag: tagName}
}

type Rect struct {
	X, Y          int
	Width, Height int
}

func (b *BaseBox) Base() *BaseBox {
	return b
}

// 返回当前的布局大小。
//
// 只应参考 Width 和 Height。X、Y 目前是相对于父元素的，不太有参考意义。
func (b *BaseBox) GetLayoutBox() Rect {
	return b.layoutBox
}
func (b *BaseBox) GetComputedStyles() *Styles {
	return &b.computedStyles
}

func (b *BaseBox) ClassSet(class string) {
	b.class.Set(class)
	b.classChanged()
}
func (b *BaseBox) ClassContains(class string) bool {
	return b.class.Contains(class)
}
func (b *BaseBox) ClassAdd(class string) {
	b.class.Add(class)
	b.classChanged()
}
func (b *BaseBox) ClassRemove(class string) {
	b.class.Remove(class)
	b.classChanged()
}
func (b *BaseBox) ClassToggle(class string, force ...any) {
	b.class.Toggle(class, force...)
	b.classChanged()
}

func (b *BaseBox) Activate() {
	b.document.activate(b)
}

// 祖先回溯。从父亲到祖宗。
func (b *BaseBox) Ancestors() iter.Seq[Box] {
	return func(yield func(Box) bool) {
		for p := b.Parent; p != nil; p = p.Base().Parent {
			if !yield(p) {
				break
			}
		}
	}
}
func (b *BaseBox) ancestorsForward() iter.Seq[Box] {
	var boxes []Box
	for p := b.Parent; p != nil; p = p.Base().Parent {
		boxes = append(boxes, p)
	}
	return func(yield func(Box) bool) {
		for _, box := range slices.Backward(boxes) {
			if !yield(box) {
				break
			}
		}
	}
}

func (b *BaseBox) Document() *Document {
	return b.document
}

func (b *BaseBox) Children() []Box {
	return b.children
}

func (b *BaseBox) lastChild() Box {
	if n := len(b.children); n > 0 {
		return b.children[n-1]
	}
	return nil
}

func (b *BaseBox) AppendChild(child Box) {
	if child == nil {
		panic(`Child is nil`)
	}
	if child.Base()._EventTarget.box == nil {
		panic(`事件对象未完成初始化:` + reflect.TypeOf(child).String())
	}

	prevLastChild := b.lastChild()
	b.children = append(b.children, child)
	child.Base().Parent = b

	if prevLastChild != nil {
		prevLastChild.Base().NextSibling = child
		child.Base().PrevSibling = prevLastChild
	}

	if b.document != nil {
		b.document.layoutDirty = true
		b.document.style(b, true)
	}
}

type PropertySetter interface {
	SetProp(key string, val string) error
}

func (b *BaseBox) SetProp(key string, val string) error {
	reInherit, reLayout, rePaint, err := b.inlineStyles.Set(key, val)
	if err == nil {
		// 文档解析过程中也会调用进来，所以需要判断。
		if b.document.root != nil {
			b.document.style(b, reInherit)
		}
		if reLayout {
			b.document.layoutDirty = true
		}
		if rePaint {
			b.document.paintDirty = true
		}
		return nil
	} else {
		if !errors.Is(err, ErrUnknownStyleProperty) {
			return err
		}
	}

	switch key {
	default:
		return fmt.Errorf(`不认识的属性：%s`, key)
	case `id`:
		// 改ID也会影响样式选择，所以需要重新排版
		b.ID = val
		b.document.layoutDirty = true
		if b.document.root != nil {
			b.document.style(b, true)
		}
	case `class`:
		// 改class也会影响样式选择，所以需要重新排版
		// 会自动调用classChanged
		b.ClassSet(val)
	case `name`:
		b.Name = val
	}

	return nil
}

// 获取用户数据。
// 如果Get不到，直接崩溃。
func (b *BaseBox) GetData(name string) any {
	data, ok := b.dataset[name]
	if !ok {
		log.Panicf(`找不到此用户数据: %v`, name)
	}
	return data
}

// 保存任意自定义用户数据。
//
// 如果指定的名字已经存在，会直接覆盖。
func (b *BaseBox) SetData(name string, value any) {
	if b.dataset == nil {
		b.dataset = map[string]any{}
	}
	b.dataset[name] = value
}

func (b *BaseBox) classChanged() {
	b.document.layoutDirty = true
	if b.document.root != nil {
		b.document.style(b, true)
	}
}

// 如果宽度指定了百分比，其百分比是相对于父元素的，不能等到其它元素占用（并减去）后再计算。
func (b *BaseBox) presetWidth(parentTotalAvailWidth int) {
	if b.computedStyles.Width.IsPercentage() {
		// 百分比暂时优先级更高，所以如果窗口大小变了。b.Width会怎样？
		// 因为百分比才是真实的初始化，Width原本是没有的。
		w := int(float32(b.computedStyles.Width.Number) / 100 * float32(parentTotalAvailWidth))
		// 这里比较特殊：把计算值写回参考值中了。
		// 正常来说，这个在排版（非样式计算）过程中是只读的，真正的值应该写到 layoutBox。
		// 但是由于每次计算这个值都会因为百分比变化，没有因为单次排版而被固定，所以看起来没有问题？
		b.computedStyles.Width = NumberValue(w)
	}
}

func (b *BaseBox) NcWidth() int {
	return b.ncWidth()
}

func (b *BaseBox) ncWidth() int {
	return b.computedStyles.BorderWidth.Number + b.computedStyles.Padding.Number
}

// TODO 重构：把所有元素的calc方法统一到这里分发。
// 对于 <block> 或 display=block，用 blocking formatting context
// 对于 <inline> 或 display=inline，用 inline context
// 对于 <stack> 或 display=stack，用 stack context
// 对于 display=none，不参与排版
// 对于 <用户自定义>，参考 css.display。
//
// 只针对没有自己实现 Calc 方法的元素而言。如果自己实现了 Calc 方法（比如 Scroll），
// 行为不受此约束。
func (b *BaseBox) Calc(availWidth, availHeight int, constraints Constraints) {
	// 兼容
	if b := b.computedStyles.Display; b.IsBool() && !b.Bool {
		return
	}

	display := b.computedStyles.Display.String

	if b.Tag == `block` || display == `block` {
		blockCalc(b, availWidth, availHeight, constraints)
	} else if b.Tag == `inline` || display == `inline` {
		inlineCalc(b, availWidth, availHeight, constraints)
	} else {
		// 其它自己不实现的通通按inline来。
		inlineCalc(b, availWidth, availHeight, constraints)
	}
}

func (b *BaseBox) Draw(canvas *Canvas) {
	b.draw(canvas, true)
}

// 所有盒子通用的画法。
// 包括：Outline、Border、Background、Children。
func (b *BaseBox) draw(canvas *Canvas, drawChildren bool) {
	borderWidth := b.computedStyles.BorderWidth.Number
	layoutWidth := b.layoutBox.Width
	layoutHeight := b.layoutBox.Height

	if outlineWidth := b.computedStyles.OutlineWidth.Number; outlineWidth > 0 {
		if outlineColor := b.computedStyles.OutlineColor; outlineColor.IsColor() && !outlineColor.Color.None() {
			// Outline（外边框）是不算在盒子本身的width和height内的，
			// 所以要负向（左上）偏移到父元素。
			canvas := canvas.Offset(-outlineWidth, -outlineWidth)
			// 同时，宽度和高度有要向右下偏移。
			width := layoutWidth + outlineWidth*2
			height := layoutHeight + outlineWidth*2
			canvas.DrawBorder(outlineColor.Color, width, height, outlineWidth)
		}
	}

	// 默认都是 border-box，所以以实际的宽和高为准。
	if bcv := b.computedStyles.BorderColor; borderWidth > 0 && !bcv.Empty() && !bcv.Color.None() {
		canvas.DrawBorder(bcv.Color, layoutWidth, layoutHeight, borderWidth)
	}

	if src := b.computedStyles.BackgroundImage.String; src != `` {
		width := layoutWidth - borderWidth*2
		height := layoutHeight - borderWidth*2
		canvas := canvas.Offset(borderWidth, borderWidth)
		img, _ := b.document._loadImage(src, width, height, true)
		if len(img.Pixels) > 0 {
			canvas.DrawImage(img, width, height)
		} else {
			b.document.loadImageAsync(src, width, height, func(di DecodedImage, err error) {
				b.document.RequestPaint()
			})
		}
	} else if bcv := b.computedStyles.BackgroundColor; !bcv.Empty() && !bcv.Color.None() {
		canvas.Offset(borderWidth, borderWidth).FillRect(
			0, 0,
			layoutWidth-borderWidth*2,
			layoutHeight-borderWidth*2,
			bcv.Color,
		)
	}

	if drawChildren {
		for _, child := range b.children {
			if !displaying(child) {
				continue
			}
			layout := child.Base().layoutBox
			canvas := canvas.Offset(layout.X, layout.Y)
			child.Draw(canvas)
		}
	}
}

func displaying(b Box) bool {
	d := b.Base().computedStyles.Display
	return d.Empty() || (d.IsBool() && d.Bool)
}

// 纵向排版容器。
//
// 如果子元素没有指定宽度，则始终横向占满。
type Block struct {
	BaseBox
}

func NewBlock(doc *Document) *Block {
	return &Block{BaseBox: NewBaseBox(doc, `block`)}
}

func blockCalc(b *BaseBox, availWidth, availHeight int, constraints Constraints) {
	computed := &b.computedStyles

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iif(computed.Width.IsNumber(), computed.Width.Number, availWidth)
	boxMaxHeight := Iif(computed.Height.IsNumber(), computed.Height.Number, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - b.ncWidth()*2
	contentAvailHeight := boxMaxHeight - b.ncWidth()*2

	// 当前实际占用高度
	contentHeight := 0

	// 如果有 Spacer（未设定大小的），则留到后面均匀地铺满。
	zeroSpacers := []Box{}

	for _, child := range b.children {
		if !displaying(child) {
			continue
		}

		// 所有元素，如果没有特别指定宽度，则总是占满。
		// if child.Base().computedStyles.Width.Empty() {
		// child.Base().layoutBox.Width = contentAvailWidth
		// child.Base().computedStyles.Width = NumberValue(contentAvailWidth)
		// }

		if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Height.Empty() {
			zeroSpacers = append(zeroSpacers, spacer)
			contentHeight += spacer.ncWidth() * 2
		} else if child.Base().computedStyles.Spacer.Bool {
			zeroSpacers = append(zeroSpacers, child)
			contentHeight += child.Base().ncWidth() * 2
		} else {
			if text, ok := child.(*Text); ok {
				text.SegmentBlock(contentAvailWidth, contentAvailHeight-contentHeight)
			} else {
				child.Base().presetWidth(contentAvailWidth)
				child.Calc(contentAvailWidth, contentAvailHeight-contentHeight, Constraints{
					PrefersMaxWidth:  true,
					PrefersMaxHeight: false,
				})
			}
			contentHeight += child.Base().layoutBox.Height
		}
	}

	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgHeight := (contentAvailHeight - contentHeight) / len(zeroSpacers)
		contentHeight = contentAvailHeight
		for _, spacer := range zeroSpacers {
			height := spacer.Base().ncWidth()*2 + avgHeight
			// 如果是非spacer元素，则需要重新排版
			if _, ok := spacer.(*Spacer); !ok {
				spacer.Calc(contentAvailWidth, height, Constraints{
					PrefersMaxWidth:  true,
					PrefersMaxHeight: true,
				})
			} else {
				spacer.Base().layoutBox.Width = contentAvailWidth
				spacer.Base().layoutBox.Height = height
			}
		}
	}

	// if hv := b.computedStyles.Height; hv.IsNumber() && hv.Number-b.ncWidth()*2 > contentHeight {
	// 	contentHeight = hv.Number - b.ncWidth()*2
	// }

	// 此时已经可以确定容器本身的大小了。
	b.layoutBox.Width = resolveSize(computed.Width, availWidth, constraints.PrefersMaxWidth, boxMaxWidth)
	b.layoutBox.Height = resolveSize(computed.Height, availHeight, constraints.PrefersMaxHeight, b.ncWidth()*2+contentHeight)

	// 最后再重新对齐子元素

	// 先是垂直对齐。
	// 对于block来说，垂直方向不止一个元素，需要整体平移。
	offsetY := b.ncWidth()
	if align := computed.Align.String; align == `both` || align == `middle` {
		offsetY += (b.layoutBox.Height - b.ncWidth()*2 - contentHeight) / 2
	}

	// 然后是水平对齐。
	// 水平对齐需要对每一个子元素单独改（因为它们是在垂直方向排列的，不在一条水平线上）。
	alignCenter := computed.Align.String == `both` || computed.Align.String == `center`

	for _, child := range b.children {
		if !displaying(child) {
			continue
		}

		layout := &child.Base().layoutBox

		// 水平居中
		offsetX := b.ncWidth()
		if alignCenter {
			offsetX += (contentAvailWidth - layout.Width) / 2
		}
		layout.X = offsetX

		// 垂直居中，对所有元素同时偏移。
		layout.Y = offsetY
		offsetY += layout.Height
	}
}

type Button struct {
	BaseBox
}

func NewButton(doc *Document) *Button {
	return &Button{BaseBox: NewBaseBox(doc, `button`)}
}

type Inline struct {
	BaseBox
}

func NewInline(doc *Document) *Inline {
	return &Inline{BaseBox: NewBaseBox(doc, `inline`)}
}

func inlineCalc(b *BaseBox, availWidth, availHeight int, constraints Constraints) {
	computed := &b.computedStyles

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iif(computed.Width.IsNumber(), computed.Width.Number, availWidth)
	boxMaxHeight := Iif(computed.Height.IsNumber(), computed.Height.Number, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - b.ncWidth()*2
	contentAvailHeight := boxMaxHeight - b.ncWidth()*2

	// 当前实际占用宽度
	contentWidth := 0

	// 实际最高占用。
	contentMaxHeight := 0

	// 如果有 Spacer（未设定大小的），则均匀地铺满。
	zeroSpacers := []Box{}

	for _, child := range b.children {
		if !displaying(child) {
			continue
		}

		if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Width.Empty() {
			zeroSpacers = append(zeroSpacers, spacer)
			contentWidth += spacer.ncWidth() * 2
		} else if child.Base().computedStyles.Spacer.Bool {
			zeroSpacers = append(zeroSpacers, child)
			contentWidth += child.Base().ncWidth() * 2
		} else {
			if text, ok := child.(*Text); ok {
				// 只处理了一行，如果要wrap，才能继续处理。
				text.ClearStates()
				text.SegmentInline(contentAvailWidth-contentWidth, contentAvailHeight)
			} else {
				child.Base().presetWidth(contentAvailWidth)
				child.Calc(contentAvailWidth-contentWidth, contentAvailHeight, Constraints{
					PrefersMaxWidth:  false,
					PrefersMaxHeight: false,
				})
			}

			childWidth := child.Base().layoutBox.Width
			childHeight := child.Base().layoutBox.Height
			contentWidth += childWidth
			contentMaxHeight = max(contentMaxHeight, childHeight)
		}
	}

	// 指定了高度，且内容实际没有高度高，则扩展到指定高度。
	if hv := b.computedStyles.Height; hv.IsNumber() && hv.Number-b.ncWidth()*2 > contentMaxHeight {
		contentMaxHeight = hv.Number - b.ncWidth()*2
	}

	// 如果父元素希望最大，则在重新调整前直接使用。
	if constraints.PrefersMaxHeight {
		contentMaxHeight = max(contentAvailHeight, contentMaxHeight)
	}

	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgWidth := (contentAvailWidth - contentWidth) / len(zeroSpacers)
		contentWidth = contentAvailWidth
		for _, spacer := range zeroSpacers {
			width := spacer.Base().ncWidth()*2 + avgWidth
			// 如果是非spacer元素，则需要重新排版
			if _, ok := spacer.(*Spacer); !ok {
				spacer.Calc(width, contentMaxHeight, Constraints{
					PrefersMaxWidth:  true,
					PrefersMaxHeight: true,
				})
			} else {
				spacer.Base().layoutBox.Width = width
			}
		}
	}

	// 此时已经可以确定容器本身的大小了。
	b.layoutBox.Width = resolveSize(computed.Width, availWidth, constraints.PrefersMaxWidth, min(availWidth, contentWidth+b.ncWidth()*2))
	b.layoutBox.Height = resolveSize(computed.Height, availHeight, constraints.PrefersMaxHeight, min(availHeight, contentMaxHeight+b.ncWidth()*2))

	// 最后再重新对齐子元素。
	offsetX := b.ncWidth()

	// 先是水平对齐。
	// 对于inline来说，水平方向不止一个元素，需要整体平移。
	// BUG: inline 是可以跨行的。这里没有考虑多行元素的对齐。
	if align := computed.Align.String; align == `both` || align == `center` {
		offsetX += (b.layoutBox.Width - b.ncWidth()*2 - contentWidth) / 2
	}

	// 然后是垂直对齐。也只处理了单行元素。
	// 垂直对齐需要对每一个子元素单独改（因为它们是水平排列的，不在一条竖线上）。
	alignMiddle := computed.Align.String == `both` || computed.Align.String == `middle`

	for _, child := range b.children {
		if !displaying(child) {
			continue
		}

		layout := &child.Base().layoutBox
		layout.X = offsetX
		offsetX += layout.Width

		// 每个元素的起点均是内容可用区开始。
		offsetY := b.ncWidth()

		if alignMiddle {
			// BUG: 这样写有一个问题，如果子元素的高度超出了最大高度（？？？），
			// 因为现在没有裁剪功能……
			// 比如文字，这时候文字是基于 nc 往下超出 box 的。
			// 如果不想要这样，可以这样写：
			//     offsetY = (b.layoutBox.Height - layout.Height) / 2
			offsetY += (contentMaxHeight - layout.Height) / 2
		}

		layout.Y = offsetY
	}
}

func resolveSize(computed Value, available int, prefersAvailable bool, actual int) int {
	if computed.IsNumber() {
		return computed.Number
	}
	if prefersAvailable {
		return available
	}
	return actual
}

type Stack struct {
	BaseBox

	fill bool
}

func NewStack(doc *Document) *Stack {
	return &Stack{BaseBox: NewBaseBox(doc, `stack`)}
}

func (b *Stack) SetProp(key string, value string) error {
	switch key {
	case `fill`:
		if value == `` {
			value = `1`
		}
		b.fill = Must1(strconv.ParseBool(value))
		return nil
	default:
		return b.Base().SetProp(key, value)
	}
}

func (b *Stack) Calc(availWidth, availHeight int, constrains Constraints) {
	computed := &b.computedStyles

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iif(computed.Width.IsNumber(), computed.Width.Number, availWidth)
	boxMaxHeight := Iif(computed.Height.IsNumber(), computed.Height.Number, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - b.ncWidth()*2
	contentAvailHeight := boxMaxHeight - b.ncWidth()*2

	// 如果有 Spacer（未设定大小的），则同等大小地拼满。
	// zeroSpacers := []Box{}

	contentMaxHeight := 0

	for _, child := range b.children {
		if !displaying(child) {
			continue
		}

		// 如果没有设置尺寸，则总是占满。
		// if !child.Base().computedStyles.Width.IsNumber() {
		// child.Base().computedStyles.Width = NumberValue(contentAvailWidth)
		// }
		// TODO 如果父元素被设置了尺寸，总是给子元素设置同样的高度。
		// if computed.Height.IsNumber() && !child.Base().computedStyles.Height.IsNumber() {
		// contentAvailHeight 此时就等于设置的高度-2倍不可用区
		// child.Base().computedStyles.Height = NumberValue(contentAvailHeight)
		// }

		// if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Height.Empty() {
		// 	zeroSpacers = append(zeroSpacers, spacer)
		// } else if child.Base().computedStyles.Spacer.Bool {
		// 	zeroSpacers = append(zeroSpacers, child)
		// } else {

		if text, ok := child.(*Text); ok {
			text.SegmentBlock(contentAvailWidth, contentAvailHeight)
		} else {
			child.Base().presetWidth(contentAvailWidth)
			child.Calc(contentAvailWidth, contentAvailHeight, Constraints{
				PrefersMaxWidth:  b.fill,
				PrefersMaxHeight: b.fill,
			})
		}
		contentMaxHeight = max(contentMaxHeight, child.Base().layoutBox.Height)
		// }
	}

	// for _, child := range b.Children {
	// 	// 直接铺满？
	// 	// child.Base().layoutBox.Width = contentAvailWidth
	// 	child.Base().layoutBox.Height = contentMaxHeight
	// }
	// if hv := b.computedStyles.Height; hv.IsNumber() && hv.Number-b.ncWidth()*2 > contentMaxHeight {
	// 	contentMaxHeight = hv.Number - b.ncWidth()*2
	// }

	// if len(zeroSpacers) > 0 {
	// 	for _, spacer := range zeroSpacers {
	// 		spacer.Base().computedStyles.Height = NumberValue(contentMaxHeight)
	// 		if _, ok := spacer.(*Spacer); !ok {
	// 			spacer.Calc(contentAvailWidth, contentMaxHeight)
	// 		}
	// 	}
	// }

	// 最后再重新调整 Y
	offsetX := b.ncWidth()
	offsetY := b.ncWidth()
	for _, child := range b.children {
		if !displaying(child) {
			continue
		}
		child.Base().layoutBox.X = offsetX
		child.Base().layoutBox.Y = offsetY
	}

	b.layoutBox.Width = Iif(
		computed.Width.IsNumber(),
		computed.Width.Number,
		Iif(constrains.PrefersMaxWidth, availWidth, contentAvailWidth),
	)
	b.layoutBox.Height = Iif(
		computed.Height.IsNumber(),
		computed.Height.Number,
		Iif(constrains.PrefersMaxHeight, availHeight, contentMaxHeight),
	)
}

// 用来代替 margin 的使用。
//
// <spacer>是旧html标签，如果使用会被标红，写 `<spacer/>` html.Parse 会错乱。
type Spacer struct {
	BaseBox
}

func NewSpacer(doc *Document) *Spacer {
	return &Spacer{BaseBox: NewBaseBox(doc, `spacer`)}
}

// 一段奔跑/连续的文本数据/文本块。
//
// 这里还不会切割，只是解析结果。切割发生在排版过程中，是下一个阶段。
// 参考 Text.Calc
/*
比如：<text>hello<b>world</b></text>
得到：[
		{data:"hello",style:normal},
		{data:"world",style:bold},
	  ]
*/
// https://blog.twofei.com/2321/
type _TextRun struct {
	Data  string // 于文档解析时生成
	Owner Box    //
}

type _TextRunFragment struct {
	Run   *_TextRun
	Start int
	End   int

	layoutBox Rect
}
type _TextLine struct {
	Fragments []_TextRunFragment
	// 每个片段的face可能不一样
	MaxHeight int
}

type _TextParts struct {
	// 副本一份子节点，方便把纯文本节点也保存进来，
	// 这样可以维护原始顺序，而不用把纯文本节点挂
	// 在真实的dom树上。
	//
	// 注意：Box也保存这里，所以它的样式也在这里。
	children []any // string | Box
}

func (p *_TextParts) appendChildOrText(owner Box, child any) {
	p.children = append(p.children, child)
	if box, ok := child.(Box); ok {
		owner.Base().AppendChild(box)
	} else {
		if doc := owner.Document(); doc != nil {
			doc.layoutDirty = true
		}
	}
}

type Text struct {
	BaseBox

	// 形如 <text>before<b>123</b></text>
	// 会被解析成：
	//  - part: before
	//  - <b>
	//  -   part: 123
	//  - part: ""
	// 这样才能保留结构信息：指文本和元素节点的顺序。
	// 因为文本节点不保存到树上（因此也不能选中，和 浏览器行为一样）
	textParts _TextParts

	// 从 textParts 中拆出来的纯文本，用于计算排版。
	// 只能 <text> 有，bold 这些子元素的文本会合并到这里。
	// 参考 expandTextNodes 方法。
	// 用于 Calc。
	// 除非更新 text 节点，否则不要修改。
	textRuns []_TextRun

	// 当前使用到哪个 textRuns 了
	textRunIndex int
	// 当前使用到 Data 的哪部分了
	textRunDataIndex int

	// 上面的 textRuns 中的单个不一定能占据整行，所以会被拆成几个片段分成多行显示。
	// 用于 Draw。
	textLines        []_TextLine
	textLineMaxWidth int
}

func NewText(doc *Document) *Text {
	return &Text{BaseBox: NewBaseBox(doc, `text`)}
}

// 设置普通文本。
func (t *Text) SetText(text string) {
	t.textParts.children = nil
	t.children = nil
	t.AppendChild(text)
	t.expandTextNodes()
}

// 获取普通文件。
func (t *Text) GetText() string {
	sb := strings.Builder{}
	for _, run := range t.textRuns {
		sb.WriteString(run.Data)
	}
	return sb.String()
}

// 这个方法重写了基类的方法，只在 transform 中被调用。
func (t *Text) AppendChild(child any) {
	t.textParts.appendChildOrText(t, child)
}

// 把 <text> 的树形节点平铺展开方便排版。
func (t *Text) expandTextNodes() {
	t.textRuns = nil

	var processParts func(box Box)
	processParts = func(box Box) {
		var children []any
		switch typed := box.(type) {
		case *Text:
			children = typed.textParts.children
		case *BoldText:
			children = typed.textParts.children
		case *ItalicText:
			children = typed.textParts.children
		}
		for _, child := range children {
			// 如果支持图文混排，类型在这里case吗？
			switch typed := child.(type) {
			case string:
				t.textRuns = append(t.textRuns, _TextRun{
					Data:  typed,
					Owner: box,
				})
			default:
				processParts(child.(Box))
			}
		}
	}

	processParts(t)
}

// ≈ 给 block 的子元素 calc用的
// x y 在外面设置。
func (t *Text) SegmentBlock(availWidth, availHeight int) {
	t.ClearStates()
	for t.SegmentInline(availWidth, availHeight) {
	}
	t.layoutBox.Width = t.textLineMaxWidth + t.ncWidth()*2
	t.layoutBox.Height = t.BlockHeight() + t.ncWidth()*2
}

// 文本排版很特殊：
//   - 它要能自动折行
//   - 起点不一定从0开始（比如图文混排）（暂不支持）
//   - <text>内部有其它像是<b>之类的样式节点，但是是
//     连续的内容，不能分开排版，所以必须基于 textRuns。
//
// availWidth 在被父节点水平排版的时候可能是变化的（比如inline），
// 因此不能一次性排版完成所有行。
//
// 所以这个函数是每调用一次返回一行内容。
// 会有内部状态维护剩余未排版的 runs。
//
// 这个版本先简单处理、不太计性能。
//
// 返回是否还有行宽度、行高度，更多内容。
//
// calcPos 只表示当前行。
func (t *Text) SegmentInline(availWidth, availHeight int) bool {
	line := _TextLine{}
	width := 0

	for {
		if t.textRunIndex >= len(t.textRuns) {
			break
		}
		// 换下一个继续Run。
		if t.textRunDataIndex >= len(t.textRuns[t.textRunIndex].Data) {
			t.textRunIndex++
			t.textRunDataIndex = 0
			if t.textRunIndex >= len(t.textRuns) {
				break
			}
		}

		face := t.document.LoadFaceWithFallback(t.textRuns[t.textRunIndex].Owner)
		end, runWidth, err := face.Segment(
			t.textRuns[t.textRunIndex].Data[t.textRunDataIndex:],
			availWidth-width)
		if err != nil {
			// 好像啥也干不了
			return false
		}

		// 挤不了了，真的满了
		if runWidth == 0 {
			break
		}

		// 成功塞了一点东西
		line.Fragments = append(line.Fragments, _TextRunFragment{
			Run:       &t.textRuns[t.textRunIndex],
			Start:     t.textRunDataIndex,
			End:       t.textRunDataIndex + end,
			layoutBox: Rect{Width: runWidth, Height: face.TextHeight()},
		})
		line.MaxHeight = max(line.MaxHeight, face.TextHeight())

		// 更新到索引，下次循环会自动切换到一下，如果有必要。
		t.textRunDataIndex = t.textRunDataIndex + end

		width += runWidth
	}

	t.textLines = append(t.textLines, line)
	t.textLineMaxWidth = max(t.textLineMaxWidth, width)

	t.layoutBox.Width = width + t.ncWidth()*2
	t.layoutBox.Height = line.MaxHeight + t.ncWidth()*2

	return t.textRunIndex < len(t.textRuns)-1 ||
		t.textRunIndex == len(t.textRuns)-1 && t.textRunDataIndex < len(t.textRuns[t.textRunIndex].Data)
}

// 清空分行的内部状态。用于样式更新、内容更新后调用。
func (t *Text) ClearStates() {
	t.textRunIndex = 0
	t.textRunDataIndex = 0
	t.textLines = nil
	t.textLineMaxWidth = 0
}

func (t *Text) BlockHeight() int {
	h := 0
	for _, l := range t.textLines {
		h += l.MaxHeight
	}
	return h
}

func (t *Text) Draw(canvas *Canvas) {
	if t.ID == `query` {
		t.ID += ``
	}
	t.Base().draw(canvas, false)
	offsetY := t.ncWidth()
	for _, line := range t.textLines {
		offsetX := t.ncWidth()
		for _, fragment := range line.Fragments {
			rc := fragment.layoutBox
			owner := fragment.Run.Owner
			canvas := canvas.Offset(offsetX, offsetY)

			if cr := owner.Base().computedStyles.BackgroundColor; cr.IsColor() && !cr.Color.None() {
				canvas.FillRect(0, 0, rc.Width, rc.Height, cr.Color)
			}

			text := fragment.Run.Data[fragment.Start:fragment.End]
			canvas.drawStringDevice(text,
				t.document.LoadFaceWithFallback(owner),
				owner.Base().computedStyles.Color.Color,
				rc.Width, rc.Height,
			)

			offsetX += rc.Width
		}
		offsetY += line.MaxHeight
	}
}

type BoldText struct {
	BaseBox

	textParts _TextParts
}

func NewBoldText(doc *Document) *BoldText {
	return &BoldText{BaseBox: NewBaseBox(doc, `b`)}
}

func (t *BoldText) AppendChild(child any) {
	t.textParts.appendChildOrText(t, child)
}

type ItalicText struct {
	BaseBox

	textParts _TextParts
}

func NewItalicText(doc *Document) *ItalicText {
	return &ItalicText{BaseBox: NewBaseBox(doc, `i`)}
}

func (t *ItalicText) AppendChild(child any) {
	t.textParts.appendChildOrText(t, child)
}

type _ImageLoadingStatus uint8

const (
	imageLoadStatusNone      _ImageLoadingStatus = iota // 还没开始
	imageLoadStatusStarted                              // 已经开始，但是还没完成
	imageLoadStatusSucceeded                            // 完成，并且加载成功
	imageLoadStatusFailed                               // 完成，并且加载失败
)

type Image struct {
	BaseBox

	src string

	status _ImageLoadingStatus
	// 异步加载成功后写在这里。
	decodedImage DecodedImage
}

func NewImage(doc *Document) *Image {
	return &Image{BaseBox: NewBaseBox(doc, `img`)}
}

func (b *Image) SetProp(key string, val string) error {
	switch key {
	case `src`:
		// 防止触发重复刷新。
		if b.src == val {
			return nil
		}
		b.src = val
		b.status = imageLoadStatusNone
		if b.document != nil {
			b.document.RequestLayout()
		}
		return nil
	default:
		return b.BaseBox.SetProp(key, val)
	}
}

// 设置操作系统文件路径。
// 相对或者绝对均可。
func (b *Image) SetPath(path string) {
	u := (&url.URL{Scheme: `os`, Opaque: url.PathEscape(path)}).String()
	b.SetProp(`src`, u)
}

func (b *Image) Calc(availWidth, availHeight int, constraints Constraints) {
	b.layoutBox.Width = Iif(constraints.PrefersMaxWidth, availWidth, 0)
	b.layoutBox.Height = Iif(constraints.PrefersMaxHeight, availHeight, 0)

	if !b.computedStyles.Width.Empty() && !b.computedStyles.Height.Empty() {
		b.layoutBox.Width = b.computedStyles.Width.Number
		b.layoutBox.Height = b.computedStyles.Height.Number
	}

	if b.src == `` {
		return
	}

	switch b.status {
	case imageLoadStatusNone:
		if img, err := b.document.loadImageSync(b.src, b.layoutBox.Width, b.layoutBox.Height); err == nil {
			b.decodedImage = img
			b.status = imageLoadStatusSucceeded
			b.Calc(availWidth, availHeight, constraints)
			return
		} else {
			b.document.loadImageAsync(b.src,
				b.layoutBox.Width, b.layoutBox.Height,
				func(img DecodedImage, err error) {
					if err != nil {
						b.status = imageLoadStatusFailed
						return
					}
					b.decodedImage = img
					b.status = imageLoadStatusSucceeded
					b.document.RequestLayout()
				},
			)
			b.status = imageLoadStatusStarted
			return
		}
	case imageLoadStatusStarted:
		// 图片加载中，啥也不能干？
		return
	case imageLoadStatusSucceeded:
		width, height := 0, 0
		img := b.decodedImage

		scaleW := float32(img.Width) / float32(availWidth)
		scaleH := float32(img.Height) / float32(availHeight)
		if scaleW > scaleH {
			width = availWidth
			height = int(float32(img.Height) / float32(scaleW))
		} else {
			width = int(float32(img.Width) / float32(scaleH))
			height = availHeight
		}

		b.layoutBox.Width = Iif(constraints.PrefersMaxWidth, availWidth, width)
		b.layoutBox.Height = Iif(constraints.PrefersMaxHeight, availHeight, height)
	case imageLoadStatusFailed:
		return
	}
}

func (b *Image) Draw(canvas *Canvas) {
	b.Base().draw(canvas, false)

	w := b.layoutBox.Width
	h := b.layoutBox.Height

	if b.status == imageLoadStatusSucceeded {
		canvas.DrawImage(b.decodedImage, w, h)
	}
}

type Scroll struct {
	BaseBox

	gap int

	bind func(user any, index int)

	_ScrollState
}

type _ScrollState struct {
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

func NewScroll(doc *Document) *Scroll {
	scroll := &Scroll{
		BaseBox: NewBaseBox(doc, `scroll`),
		_ScrollState: _ScrollState{
			rows:       1,
			cols:       1,
			rowIndex:   -1,
			colIndex:   -1,
			itemOffset: 0,
		},
	}

	scroll.Listen(StickDownEvent, func(e *Event) {
		scroll.navigate(e)
	}, EventOptions{})

	return scroll
}

// TODO 取消重复计算，大小不变的情况下只需要计算一次。
func (b *Scroll) Calc(availWidth, availHeight int, constraints Constraints) {
	// computed := &b.computedStyles

	// if len(b.Children) <= 0 {
	// 	return
	// }

	// 只有确定了大小才能决定子元素的大小和布局。
	// if !(computed.Width.IsNumber() && computed.Height.IsNumber()) {
	// return
	// }

	var (
		contentAvailWidth  = availWidth - (b.ncWidth()*2 + (b.cols-1)*b.gap)
		contentAvailHeight = availHeight - (b.ncWidth()*2 + (b.rows-1)*b.gap)

		offsetX = b.ncWidth()
		offsetY = b.ncWidth()

		avgHeight = average(contentAvailHeight, b.rows)
		avgWidth  = average(contentAvailWidth, b.cols)
	)

	for i, child := range b.children {
		child.(*_ScrollChild).forceCalc(offsetX, offsetY, avgWidth, avgHeight)
		// 需要换行了
		if (i+1)%b.cols == 0 {
			offsetX = b.ncWidth()
			offsetY += b.gap
			offsetY += avgHeight
		} else {
			offsetX += b.gap
			offsetX += avgWidth
		}
	}

	b.layoutBox.Width = Iif(
		b.computedStyles.Width.IsNumber(),
		b.computedStyles.Width.Number,
		Iif(constraints.PrefersMaxWidth, availWidth, 0),
	)
	b.layoutBox.Height = Iif(
		b.computedStyles.Height.IsNumber(),
		b.computedStyles.Height.Number,
		Iif(constraints.PrefersMaxHeight, availHeight, 0),
	)

	// 可能还未初始化。
	if len(b.children) > 0 {
		b.adjust()
	}
}

// 为什么用浮点？
// 假设是 14 / 5，则一个元素只能等于 2
// 如果是浮点，则是 14 / 5 ≈ 2.8，round 到 3
func average(all, count int) int {
	return int(math.Round(float64(all) / float64(count)))
}

// 因为要实现虚拟draw方法，所以有它的存在。
type _ScrollChild struct {
	BaseBox

	scroll *Scroll

	user any

	// 在列表中的位置。
	// rowIndex*cols + colIndex + topIndex == item数据
	rowIndex int
	colIndex int
}

func _NewScrollChild(doc *Document) *_ScrollChild {
	box := &_ScrollChild{BaseBox: NewBaseBox(doc, `scroll-child`)}
	box._EventTarget.box = box
	return box
}

func (b *_ScrollChild) Draw(canvas *Canvas) {
	b.Base().Draw(canvas)
	// canvas.SaveToFile(fmt.Sprintf(`%d.png`, b.itemIndex()))
}

func (b *_ScrollChild) dataIndex() int {
	return b.rowIndex*b.scroll.cols + b.colIndex + b.scroll.itemOffset
}

func (b *_ScrollChild) forceCalc(x, y int, contentAvailWidth, avgHeight int) {
	base := b.Base()
	base.layoutBox.X = x
	base.layoutBox.Y = y
	base.layoutBox.Width = contentAvailWidth
	base.layoutBox.Height = avgHeight

	childContentAvailWidth := contentAvailWidth - b.ncWidth()*2
	childContentAvailHeight := avgHeight - b.ncWidth()*2

	child := base.children[0]
	base = child.Base()
	base.layoutBox.Width = childContentAvailWidth
	base.layoutBox.Height = childContentAvailHeight

	// 没有数据的项实际是被隐藏的，被隐藏的项不会参与计算。
	// 所以如果代码运行到了这里，那一定是出现了内部逻辑错误。
	if b.dataIndex() < b.scroll.count {
		// 提前绑定上去才能提供数据、提供计算支撑。
		// TODO 现在是处理 calc 中，如果限定了尺寸的话，
		// 其实是不需要此刻 bind 的，Draw 的时候 bind 才比较好。
		// 因为其它控件需要calc的时候此控件不一定需要。
		//
		// 而且，如果项目过多，可能导致bind触发过多的RequestPaint阻塞队列？
		// 队列满了的话，会不会死在这里？
		b.scroll.bind(b.user, b.dataIndex())
	}

	child.Calc(childContentAvailWidth, childContentAvailHeight, Constraints{
		PrefersMaxWidth:  true,
		PrefersMaxHeight: true,
	})
	child.Base().layoutBox.X = b.ncWidth()
	child.Base().layoutBox.Y = b.ncWidth()
}

func (b *Scroll) SetProp(key, value string) error {
	switch key {
	case `rows`:
		b.rows = Must1(strconv.Atoi(value))
		return nil
	case `cols`:
		b.cols = Must1(strconv.Atoi(value))
		return nil
	case `gap`:
		b.gap = Must1(strconv.Atoi(value))
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
func (b *Scroll) SetItems(count int, create func() (root Box, user any), bind func(user any, index int)) {
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
			wrapper := _NewScrollChild(b.document)
			wrapper.user = user
			wrapper.scroll = b
			wrapper.rowIndex = r
			wrapper.colIndex = c
			wrapper.AppendChild(box)
			b.AppendChild(wrapper)
		}
	}
}

func (b *Scroll) navigate(event *Event) {
	name := event.Stick.Name

	if !(name == Up || name == Down || name == Left || name == Right) {
		return
	}

	oldState := b._ScrollState
	if !b._ScrollState.navigate(name) {
		return
	}

	// 取消选中原来的
	if childIndex := oldState.rowIndex*oldState.cols + oldState.colIndex; childIndex >= 0 && childIndex <= len(b.children)-1 {
		b.children[childIndex].Base().ClassRemove(`selected`)
	}

	// 更新选中
	if childIndex := b.rowIndex*b.cols + b.colIndex; childIndex >= 0 && childIndex <= len(b.children)-1 {
		b.children[childIndex].Base().ClassAdd(`selected`)
	}

	// 如果物理列表没变（前面类名不会变化）、但是虚拟列表滚动了，
	// 则需要主动告知文档更新，否则不会重绘。
	if oldState.itemOffset != b.itemOffset {
		b.document.RequestPaint()
	}

	event.StopPropagation()
}

// navigate 计算一次导航后的选中状态。
// 返回值表示状态是否发生了变化。
func (b *_ScrollState) navigate(name KeyName) bool {
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
		if b.colIndex > 0 {
			b.colIndex--
		}
	case Right:
		// 左右可以翻页（只针对于1列的盒子）
		if b.rowIndex >= 0 {
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

func (b *_ScrollState) curDataRow() int {
	return b.rowIndex + b.itemOffset/b.cols
}

// [0,rows-1]
func (b *_ScrollState) maxDataRow() int {
	return b.count/b.cols + Iif(b.count%b.cols > 0, 1, 0) - 1
}

func (b *Scroll) adjust() {
	for r := range b.rows {
		for c := range b.cols {
			child := b.children[r*b.cols+c].(*_ScrollChild)
			display := child.dataIndex() <= b.count-1
			displayValue := child.computedStyles.Display
			if displayValue.Empty() || displayValue.Bool != display {
				// TODO 可以不用重新排版
				child.SetProp(`display`, fmt.Sprint(display))
			}
		}
	}
}

// 返回当前选中的数据索引。
// 如果没有选中，返回-1。
func (b *Scroll) DataIndex() int {
	if b.rowIndex < 0 {
		return -1
	}
	return b.rowIndex*b.cols + b.colIndex + b.itemOffset
}

// 暂时忽略错误。
func (b *Scroll) SetIndex(rowIndex, colIndex, dataIndexOffset int) {
	if rowIndex < 0 || rowIndex >= b.rows {
		return
	}
	if colIndex < 0 || colIndex >= b.cols {
		return
	}
	if rowIndex*b.cols+colIndex+dataIndexOffset >= b.count {
		return
	}

	b.rowIndex = rowIndex
	b.colIndex = colIndex
	b.itemOffset = dataIndexOffset

	childIndex := rowIndex*b.cols + b.colIndex
	b.children[childIndex].Base().ClassAdd(`selected`)

	b.document.RequestPaint()
}

// 返回数据总量。
func (b *Scroll) DataCount() int {
	return b.count
}

// 返回当前的可视行号（非数据行号）。
func (b *Scroll) RowIndex() int {
	return b.rowIndex
}

func (b *Scroll) DataRowIndex() int {
	return b.curDataRow()
}

// 取消选中当前的选中项。
func (b *Scroll) Deselect() {
	childIndex := b.rowIndex*b.cols + b.colIndex
	if childIndex >= 0 && childIndex <= len(b.children)-1 {
		b.children[childIndex].Base().ClassRemove(`selected`)
	}
	b.rowIndex = -1
	b.colIndex = 0
	// 好像可以不用归位？
	b.itemOffset = 0

	b.document.RequestPaint()
}

// 返回当前的选中状态信息，可用于后期恢复。
func (b *Scroll) GetState() any {
	return b._ScrollState
}

// 用于恢复之前的选中状态。
// 如果重新调用过 SetItems，此前的状态不再有效。
func (b *Scroll) SetState(state any) {
	st, ok := state.(_ScrollState)
	if !ok {
		panic(`无效状态`)
	}

	b._ScrollState = st
	childIndex := b.rowIndex*b.cols + b.colIndex
	if childIndex >= 0 && childIndex <= len(b.children)-1 {
		b.children[childIndex].Base().ClassAdd(`selected`)
	}

	b.document.RequestPaint()
}
