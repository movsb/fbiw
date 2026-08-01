package main

import (
	_ "embed"
	"errors"
	"fmt"
	"iter"

	_ "image/jpeg"
	_ "image/png"
)

type Box interface {
	Base() *BaseBox
	// 根据可用的宽度和高度计算自己实际的宽度和高度。
	// 自己写：并把宽度和高度写到 calcPos{width, height}。
	// 父亲写：{x, y}。
	Calc(availableWidth, availableHeight int)
	// 根据自身的 calcPos 直接画。
	// calcPos 的 {x,y} 相对于父元素。
	// 所以除根元素外（因为它是0，0），其它在Draw之前都要调整canvas到offset，
	// 即：父元素在遍历子元素Draw的时候记得根据子元素的{x,y}作offset。
	Draw(canvas *Canvas)
}

type BaseBox struct {
	ID    string
	Tag   string
	Class Class

	Dirty bool

	Document *Document
	Parent   Box
	Children []Box

	// Calc后的坐标信息。
	// TODO 实际上应该写回 computedStyles.
	calcPos Rect

	inlineStyles   Styles
	computedStyles Styles

	// 如果 computedStyles 设置了 width 百分比，
	// 这里用来临时保存 Calc 计算的 width。
	computedWidth Value
}

type Rect struct {
	X, Y          int
	Width, Height int
}

func (b *BaseBox) Base() *BaseBox {
	return b
}

func (b *BaseBox) SetNoDirty() {
	b.Dirty = false
}

func (b *BaseBox) IsDirty() bool {
	if b.Dirty {
		return true
	}
	for _, c := range b.Children {
		if c.Base().IsDirty() {
			return true
		}
	}
	return false
}

func (b *BaseBox) Ancestors() iter.Seq[Box] {
	return func(yield func(Box) bool) {
		for p := b.Parent; p != nil; p = p.Base().Parent {
			if !yield(p) {
				break
			}
		}
	}
}

func (b *BaseBox) appendChild(child Box) {
	b.Children = append(b.Children, child)
	child.Base().Parent = b
}

type AttributeApplier interface {
	ApplyAttributes(key string, val string) error
}

func (b *BaseBox) ApplyAttributes(key string, val string) error {
	if err := b.inlineStyles.Set(key, val); err == nil {
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
		b.ID = val
	case `class`:
		b.Class.Set(val)
	}

	return nil
}

// 如果宽度指定了百分比，其百分比是相对于父元素的，不能等到其它元素占用（并减去）后再计算。
func (b *BaseBox) presetWidth(parentTotalAvailWidth int) {
	if b.computedStyles.Width.IsPercentage() {
		// 百分比暂时优先级更高，所以如果窗口大小变了。b.Width会怎样？
		// 因为百分比才是真实的初始化，Width原本是没有的。
		w := int(float32(b.computedStyles.Width.Number) / 100 * float32(parentTotalAvailWidth))
		b.computedWidth = NumberValue(w)
	}
}

func (b *BaseBox) Calc(availWidth, availHeight int) {
	b.calcPos = Rect{
		0, 0,
		b.computedStyles.Width.Number,
		b.computedStyles.Height.Number,
	}
}

func (b *BaseBox) Draw(canvas *Canvas) {
	defer b.SetNoDirty()

	borderWidth := b.computedStyles.BorderWidth.Number

	// 默认都是 border-box，所以以实际的宽和高为准。
	if borderWidth > 0 && !b.computedStyles.BorderColor.Empty() && !b.computedStyles.BorderColor.Color.None() {
		canvas.DrawBorder(
			b.computedStyles.BorderColor.Color,
			b.calcPos.Width, b.calcPos.Height,
			borderWidth,
		)
	}

	if src := b.computedStyles.BackgroundImage.String; src != `` {
		canvas.Offset(borderWidth, borderWidth).DrawImage(
			DropLast1(b.Document.loadImage(src)),
			b.calcPos.Width-borderWidth*2,
			b.calcPos.Height-borderWidth*2,
		)
	} else if !b.computedStyles.BackgroundColor.Empty() && !b.computedStyles.BackgroundColor.Color.None() {
		canvas.Offset(borderWidth, borderWidth).FillRect(
			0, 0,
			b.calcPos.Width-borderWidth*2,
			b.calcPos.Height-borderWidth*2,
			b.computedStyles.BackgroundColor.Color,
		)
	}

	for _, child := range b.Children {
		p := child.Base().calcPos
		child.Draw(canvas.Offset(p.X, p.Y))
	}
}

type Block struct {
	BaseBox
}

func NewBlock(doc *Document) *Block {
	return &Block{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `block`,
		},
	}
}

func (b *Block) Calc(availWidth, availHeight int) {
	computed := &b.computedStyles

	// 自身不可用区域。
	ncWidth := computed.BorderWidth.Number + computed.Padding.Number

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iiif(
		b.computedWidth.IsNumber(), computed.Width.IsNumber(),
		b.computedWidth.Number, computed.Width.Number, availWidth,
	)
	boxMaxHeight := Iif(computed.Width.IsNumber(), computed.Width.Number, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - ncWidth*2
	contentAvailHeight := boxMaxHeight - ncWidth*2

	// 当前实际占用高度
	contentHeight := 0

	for _, child := range b.Children {
		child.Base().presetWidth(contentAvailWidth)
		child.Calc(contentAvailWidth, contentAvailHeight-contentHeight)
		child.Base().calcPos.X = ncWidth
		contentHeight += child.Base().calcPos.Height

		// 启用对齐。
		// 纵向排列的时候需要对每个元素进行设置。
		if computed.Align.String == `center` {
			child.Base().calcPos.X += (contentAvailWidth - child.Base().calcPos.Width) / 2
		}
	}

	// 如果有 Spacer（未设定大小的），则均匀地铺满。
	zeroSpacers := []Box{}
	for _, child := range b.Children {
		if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Height.Empty() {
			zeroSpacers = append(zeroSpacers, spacer)
		} else if child.Base().computedStyles.Spacer.Bool {
			zeroSpacers = append(zeroSpacers, child)
		}
	}
	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgHeight := (contentAvailHeight - contentHeight) / len(zeroSpacers)
		contentHeight = contentAvailHeight
		for _, spacer := range zeroSpacers {
			spacer.Base().calcPos.Height = avgHeight
		}
	}

	// 最后再重新调整 Y
	offsetY := ncWidth
	for _, child := range b.Children {
		p := &child.Base().calcPos
		p.Y = offsetY
		offsetY += p.Height
	}

	b.calcPos.Width = Iiif(
		b.computedWidth.IsNumber(), computed.Width.IsNumber(),
		b.computedWidth.Number, computed.Width.Number, boxMaxWidth)
	b.calcPos.Height = Iif(computed.Height.IsNumber(), computed.Height.Number, contentHeight+ncWidth*2)
}

type Button struct {
	BaseBox
}

func NewButton(doc *Document) *Button {
	return &Button{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `button`,
		},
	}
}

func (b *Button) ApplyAttributes(key string, val string) error {
	switch key {
	default:
		return b.BaseBox.ApplyAttributes(key, val)
	}
}

type Inline struct {
	BaseBox
}

func NewInline(doc *Document) *Inline {
	return &Inline{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `inline`,
		},
	}
}

func (b *Inline) Calc(availWidth, availHeight int) {
	computed := &b.computedStyles

	// 自身不可用区域。
	ncWidth := computed.BorderWidth.Number + computed.Padding.Number

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iiif(
		b.computedWidth.IsNumber(),
		computed.Width.IsNumber(),
		b.computedWidth.Number,
		computed.Width.Number,
		availWidth)
	boxMaxHeight := Iif(
		computed.Height.IsNumber(),
		computed.Height.Number,
		availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - ncWidth*2
	contentAvailHeight := boxMaxHeight - ncWidth*2

	// 当前实际占用宽度
	contentWidth := 0

	// 实际最高占用。
	contentMaxHeight := 0

	for _, child := range b.Children {
		child.Base().presetWidth(contentAvailWidth)
		child.Calc(contentAvailWidth-contentWidth, contentAvailHeight)
		child.Base().calcPos.Y = ncWidth
		contentWidth += child.Base().calcPos.Width
		contentMaxHeight = max(contentMaxHeight, child.Base().calcPos.Height)
	}

	// 如果有 Spacer（未设定大小的），则均匀地铺满。
	zeroSpacers := []Box{}
	for _, child := range b.Children {
		if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Width.Empty() {
			zeroSpacers = append(zeroSpacers, spacer)
		} else if child.Base().computedStyles.Spacer.Bool {
			zeroSpacers = append(zeroSpacers, child)
		}
	}
	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgWidth := (contentAvailWidth - contentWidth) / len(zeroSpacers)
		contentWidth = contentAvailWidth
		for _, spacer := range zeroSpacers {
			spacer.Base().calcPos.Width = avgWidth
		}
	}

	// 最后再重新调整 X
	offsetX := ncWidth
	for _, child := range b.Children {
		p := &child.Base().calcPos
		p.X = offsetX
		offsetX += p.Width
	}

	b.calcPos.Width = Iiif(
		b.computedWidth.IsNumber(),
		computed.Width.IsNumber(),
		b.computedWidth.Number,
		computed.Width.Number,
		contentWidth+ncWidth*2)
	b.calcPos.Height = Iif(
		computed.Height.IsNumber(),
		computed.Height.Number,
		contentMaxHeight+ncWidth*2)

	// 如果是垂直居中，则重新调整Y
	if computed.Align.String == `middle` {
		contentHeight := Iif(
			computed.Height.IsNumber(),
			computed.Height.Number-ncWidth*2,
			contentMaxHeight)
		for _, child := range b.Children {
			child.Base().calcPos.Y += (contentHeight - child.Base().calcPos.Height) / 2
		}
	}
}

func (b *Inline) ApplyAttributes(key string, val string) error {
	switch key {
	default:
		return b.BaseBox.ApplyAttributes(key, val)
	}
}

// 用来代替 margin 的使用。
//
// <spacer>是旧html标签，如果使用会被标红，且 html.Parse 错乱。
type Spacer struct {
	BaseBox
}

func NewSpacer(doc *Document) *Spacer {
	return &Spacer{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `space`,
		},
	}
}

type Text struct {
	BaseBox
	Data string

	// calc后保存的face，draw可以直接用，避免重复计算
	face FontFace
}

func NewText(doc *Document) *Text {
	return &Text{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `text`,
		},
	}
}

func (t *Text) Calc(availWidth, availHeight int) {
	if t.Data == `` {
		t.calcPos = Rect{}
		return
	}

	t.face = t.Document.loadFaceWithFallback(t)
	textHeight := t.face.TextHeight()

	var (
		maxWidth = 0
		lines    = 0
		text     = t.Data
	)

	for {
		index, width, err := t.face.Segment(text, availWidth)
		if err != nil {
			t.calcPos = Rect{}
			return
		}
		maxWidth = max(maxWidth, width)
		lines++
		text = text[index:]
		if len(text) == 0 {
			break
		}
	}

	t.calcPos.Width = maxWidth
	t.calcPos.Height = textHeight * lines
}

func (t *Text) Draw(canvas *Canvas) {
	t.Base().SetNoDirty()

	textHeight := t.face.TextHeight()
	offsetY := 0

	for text := t.Data; ; {
		index, _, err := t.face.Segment(text, t.calcPos.Width)
		if err != nil {
			return
		}
		canvas.Offset(0, offsetY).drawString(
			text[:index], t.face,
			t.computedStyles.Color.Color,
			// 这里的参数其实不对，但是也不关紧要。
			t.calcPos.Width, t.calcPos.Height,
		)
		text = text[index:]
		offsetY += textHeight
		if len(text) == 0 {
			break
		}
	}
}

type Image struct {
	BaseBox

	Src string
}

func NewImage(doc *Document) *Image {
	return &Image{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `image`,
		},
	}
}

func (b *Image) ApplyAttributes(key string, val string) error {
	switch key {
	case `src`:
		b.Src = val
		return nil
	default:
		return b.BaseBox.ApplyAttributes(key, val)
	}
}

func (b *Image) Calc(availWidth, availHeight int) {
	if !b.computedStyles.Width.Empty() && !b.computedStyles.Height.Empty() {
		b.calcPos.Width = b.computedStyles.Width.Number
		b.calcPos.Height = b.computedStyles.Height.Number
		return
	}

	if b.Src == `` {
		b.calcPos = Rect{}
		return
	}

	img, err := b.Document.loadImage(b.Src)
	if err != nil {
		b.calcPos = Rect{}
		return
	}

	b.calcPos.Width = img.Width
	b.calcPos.Height = img.Height
}

func (b *Image) Draw(canvas *Canvas) {
	defer b.SetNoDirty()

	if b.Src == `` {
		return
	}

	img, err := b.Document.loadImage(b.Src)
	if err != nil {
		return
	}

	canvas.DrawImage(img, b.calcPos.Width, b.calcPos.Height)
}
