package fbiw

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

	// 不同于form元素的name，这个name不用于css。
	// 不要求唯一，用于保存业务数据。
	Name string

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
	if b.Document != nil {
		b.Document.layoutDirty = true
		b.Document.style(b, true)
	}
}

type Setter interface {
	Set(key string, val string) error
}

func (b *BaseBox) Set(key string, val string) error {
	reInherit, reLayout, rePaint, err := b.inlineStyles.Set(key, val)
	if err == nil {
		// 文档解析过程中也会调用进来，所以需要判断。
		if b.Document.root != nil {
			b.Document.style(b, reInherit)
		}
		if reLayout {
			b.Document.layoutDirty = true
		}
		if rePaint {
			b.Document.paintDirty = true
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
		b.Document.layoutDirty = true
		if b.Document.root != nil {
			b.Document.style(b, true)
		}
	case `class`:
		// 改class也会影响样式选择，所以需要重新排版
		// 会自动调用classChanged
		b.Class.Set(val)
	case `name`:
		b.Name = val
	}

	return nil
}

func (b *BaseBox) classChanged() {
	b.Document.layoutDirty = true
	if b.Document.root != nil {
		b.Document.style(b, true)
	}
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
		if !displaying(child) {
			continue
		}
		p := child.Base().calcPos
		child.Draw(canvas.Offset(p.X, p.Y))
	}
}

func displaying(b Box) bool {
	d := b.Base().computedStyles.Display
	return d.Empty() || (d.IsBool() && d.Bool)
}

type Block struct {
	BaseBox
}

func NewBlock(doc *Document) *Block {
	b := &Block{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `block`,
		},
	}
	b.Class.box = b
	return b
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
		if !displaying(child) {
			continue
		}
		if text, ok := child.(*Text); ok {
			text.SegmentBlock(contentAvailWidth, contentAvailHeight-contentHeight)
		} else {
			child.Base().presetWidth(contentAvailWidth)
			child.Calc(contentAvailWidth, contentAvailHeight-contentHeight)
		}
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
		if !displaying(child) {
			continue
		}
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
			// spacer本身有高度的，不能直接赋值。见测试 #12
			spacer.Base().calcPos.Height += avgHeight
		}
	}

	// 最后再重新调整 Y
	offsetY := ncWidth
	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
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
	b := &Button{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `button`,
		},
	}
	b.Class.box = b
	return b
}

type Inline struct {
	BaseBox
}

func NewInline(doc *Document) *Inline {
	b := &Inline{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `inline`,
		},
	}
	b.Class.box = b
	return b
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
		if !displaying(child) {
			continue
		}
		if text, ok := child.(*Text); ok {
			// 只处理了一行，如果要wrap，才能继续处理。
			text.ClearStates()
			text.SegmentInline(contentAvailWidth-contentWidth, contentAvailHeight)
		} else {
			child.Base().presetWidth(contentAvailWidth)
			child.Calc(contentAvailWidth-contentWidth, contentAvailHeight)
		}
		child.Base().calcPos.Y = ncWidth
		contentWidth += child.Base().calcPos.Width
		contentMaxHeight = max(contentMaxHeight, child.Base().calcPos.Height)
	}

	// 如果有 Spacer（未设定大小的），则均匀地铺满。
	zeroSpacers := []Box{}
	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
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
			spacer.Base().calcPos.Width += avgWidth
		}
	}

	// 最后再重新调整 X
	offsetX := ncWidth
	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
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

type Stack struct {
	BaseBox
}

func NewStack(doc *Document) *Stack {
	b := &Stack{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `stack`,
		},
	}
	b.Class.box = b
	return b
}

func (b *Stack) Calc(availWidth, availHeight int) {
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
		if !displaying(child) {
			continue
		}
		if text, ok := child.(*Text); ok {
			text.SegmentBlock(contentAvailWidth, contentAvailHeight-contentHeight)
		} else {
			child.Base().presetWidth(contentAvailWidth)
			child.Calc(contentAvailWidth, contentAvailHeight-contentHeight)
		}
		child.Base().calcPos.X = ncWidth
		contentHeight = max(contentHeight, child.Base().calcPos.Height)

		// 启用对齐。
		// 纵向排列的时候需要对每个元素进行设置。
		if computed.Align.String == `center` {
			child.Base().calcPos.X += (contentAvailWidth - child.Base().calcPos.Width) / 2
		}
	}

	// 如果有 Spacer（未设定大小的），则同等大小地拼满。
	zeroSpacers := []Box{}
	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
		if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Height.Empty() {
			zeroSpacers = append(zeroSpacers, spacer)
		} else if child.Base().computedStyles.Spacer.Bool {
			zeroSpacers = append(zeroSpacers, child)
		}
	}
	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgHeight := contentAvailHeight - contentHeight
		contentHeight = contentAvailHeight
		for _, spacer := range zeroSpacers {
			// spacer本身有高度的，不能直接赋值。见测试 #12
			spacer.Base().calcPos.Height += avgHeight
		}
	}

	// 最后再重新调整 Y
	offsetY := ncWidth
	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
		p := &child.Base().calcPos
		p.Y = offsetY
	}

	b.calcPos.Width = Iiif(
		b.computedWidth.IsNumber(), computed.Width.IsNumber(),
		b.computedWidth.Number, computed.Width.Number, boxMaxWidth)
	b.calcPos.Height = Iif(computed.Height.IsNumber(), computed.Height.Number, contentHeight+ncWidth*2)
}

// 用来代替 margin 的使用。
//
// <spacer>是旧html标签，如果使用会被标红，写 `<spacer/>` html.Parse 会错乱。
type Spacer struct {
	BaseBox
}

func NewSpacer(doc *Document) *Spacer {
	b := &Spacer{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `spacer`,
		},
	}
	b.Class.box = b
	return b
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

	calcPos Rect
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
		owner.Base().appendChild(box)
	} else {
		if doc := owner.Base().Document; doc != nil {
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
	b := &Text{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `text`,
		},
	}
	b.Class.box = b
	return b
}

// 设置普通文本。
func (t *Text) SetText(text string) {
	t.textParts.children = nil
	t.Children = nil
	t.appendChild(text)
	t.expandTextNodes()
}

// 这个方法重写了基类的方法，只在 transform 中被调用。
func (t *Text) appendChild(child any) {
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
	t.calcPos = Rect{
		Width:  t.textLineMaxWidth,
		Height: t.BlockHeight(),
	}
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

		face := t.Document.loadFaceWithFallback(t.textRuns[t.textRunIndex].Owner)
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
			Run:     &t.textRuns[t.textRunIndex],
			Start:   t.textRunDataIndex,
			End:     t.textRunDataIndex + end,
			calcPos: Rect{Width: runWidth, Height: face.TextHeight()},
		})
		line.MaxHeight = max(line.MaxHeight, face.TextHeight())

		// 更新到索引，下次循环会自动切换到一下，如果有必要。
		t.textRunDataIndex = t.textRunDataIndex + end

		width += runWidth
	}

	t.textLines = append(t.textLines, line)
	t.textLineMaxWidth = max(t.textLineMaxWidth, width)

	t.calcPos = Rect{Width: width, Height: line.MaxHeight}

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
	offsetY := 0
	for _, line := range t.textLines {
		offsetX := 0
		for _, fragment := range line.Fragments {
			rc := fragment.calcPos
			owner := fragment.Run.Owner
			canvas := canvas.Offset(offsetX, offsetY)

			if cr := owner.Base().computedStyles.BackgroundColor; cr.IsColor() && !cr.Color.None() {
				canvas.FillRect(0, 0, rc.Width, rc.Height, cr.Color)
			}

			text := fragment.Run.Data[fragment.Start:fragment.End]
			canvas.drawStringDevice(text,
				t.Document.loadFaceWithFallback(owner),
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
	b := &BoldText{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `b`,
			inlineStyles: Styles{
				FontBold: BoolValue(true),
			},
		},
	}
	b.Class.box = b
	return b
}

func (t *BoldText) appendChild(child any) {
	t.textParts.appendChildOrText(t, child)
}

type ItalicText struct {
	BaseBox

	textParts _TextParts
}

func NewItalicText(doc *Document) *ItalicText {
	b := &ItalicText{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `i`,
			inlineStyles: Styles{
				FontItalic: BoolValue(true),
			},
		},
	}
	b.Class.box = b
	return b
}

func (t *ItalicText) appendChild(child any) {
	t.textParts.appendChildOrText(t, child)
}

type Image struct {
	BaseBox

	Src string
}

func NewImage(doc *Document) *Image {
	b := &Image{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `image`,
		},
	}
	b.Class.box = b
	return b
}

func (b *Image) Set(key string, val string) error {
	switch key {
	case `src`:
		b.Src = val
		return nil
	default:
		return b.BaseBox.Set(key, val)
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
	if b.Src == `` {
		return
	}

	img, err := b.Document.loadImage(b.Src)
	if err != nil {
		return
	}

	canvas.DrawImage(img, b.calcPos.Width, b.calcPos.Height)
}
