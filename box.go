package fbiw

import (
	_ "embed"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"strconv"

	_ "image/jpeg"
	_ "image/png"
)

type Box interface {
	Base() *BaseBox
	// 根据可用的宽度和高度计算自己实际的宽度和高度。
	// 如果 computed.{width,height} 有值，则直接用，
	// 表示被外界固定好了，而且不能覆盖。
	// 自己写：并把宽度和高度写到 computed.{width, height}。
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

	inlineStyles   Styles
	computedStyles Styles

	// 相对于父亲可用区域的偏移，父亲写。
	x, y int
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

func (b *BaseBox) AppendChild(child Box) {
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
		b.computedStyles.Width = NumberValue(w)
	}
}

func (b *BaseBox) ncWidth() int {
	return b.computedStyles.BorderWidth.Number + b.computedStyles.Padding.Number
}

func (b *BaseBox) Calc(availWidth, availHeight int) {}

func (b *BaseBox) Draw(canvas *Canvas) {
	borderWidth := b.computedStyles.BorderWidth.Number
	computedWidth := b.computedStyles.Width.Number
	computedHeight := b.computedStyles.Height.Number

	// 默认都是 border-box，所以以实际的宽和高为准。
	if bcv := b.computedStyles.BorderColor; borderWidth > 0 && !bcv.Empty() && !bcv.Color.None() {
		canvas.DrawBorder(bcv.Color, computedWidth, computedHeight, borderWidth)
	}

	if src := b.computedStyles.BackgroundImage.String; src != `` {
		canvas.Offset(borderWidth, borderWidth).DrawImage(
			DropLast1(b.Document.loadImage(src, 0, 0)),
			computedWidth-borderWidth*2,
			computedHeight-borderWidth*2,
		)
	} else if bcv := b.computedStyles.BackgroundColor; !bcv.Empty() && !bcv.Color.None() {
		canvas.Offset(borderWidth, borderWidth).FillRect(
			0, 0,
			computedWidth-borderWidth*2,
			computedHeight-borderWidth*2,
			bcv.Color,
		)
	}

	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
		canvas := canvas.Offset(child.Base().x, child.Base().y)
		child.Draw(canvas)
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

	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}

		// 所有元素，如果没有特别指定宽度，则总是占满。
		if child.Base().computedStyles.Width.Empty() {
			child.Base().computedStyles.Width = NumberValue(contentAvailWidth)
		}

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
				child.Calc(contentAvailWidth, contentAvailHeight-contentHeight)
			}
			contentHeight += child.Base().computedStyles.Height.Number
		}
	}

	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgHeight := (contentAvailHeight - contentHeight) / len(zeroSpacers)
		contentHeight = contentAvailHeight
		for _, spacer := range zeroSpacers {
			height := spacer.Base().ncWidth()*2 + avgHeight
			spacer.Base().computedStyles.Height = NumberValue(height)
			// 如果是非spacer元素，则需要重新排版
			if _, ok := spacer.(*Spacer); !ok {
				spacer.Calc(contentAvailWidth, height)
			}
		}
	}

	// if hv := b.computedStyles.Height; hv.IsNumber() && hv.Number-b.ncWidth()*2 > contentHeight {
	// 	contentHeight = hv.Number - b.ncWidth()*2
	// }

	// 最后再重新调整 XY
	offsetY := b.ncWidth()
	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
		child.Base().x = b.ncWidth()
		if computed.Align.String == `center` {
			child.Base().x += (contentAvailWidth - child.Base().computedStyles.Width.Number) / 2
		}
		child.Base().y = offsetY
		offsetY += child.Base().computedStyles.Height.Number
	}

	if !computed.Width.IsNumber() {
		computed.Width = NumberValue(boxMaxWidth)
	}
	if !computed.Height.IsNumber() {
		computed.Height = NumberValue(b.ncWidth()*2 + contentHeight)
	}
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

	for _, child := range b.Children {
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
				child.Calc(contentAvailWidth-contentWidth, contentAvailHeight)
			}

			childWidth := child.Base().computedStyles.Width.Number
			childHeight := child.Base().computedStyles.Height.Number
			contentWidth += childWidth
			contentMaxHeight = max(contentMaxHeight, childHeight)
		}
	}

	// 指定了高度，且内容实际没有高度高，则扩展到指定高度。
	if hv := b.computedStyles.Height; hv.IsNumber() && hv.Number-b.ncWidth()*2 > contentMaxHeight {
		contentMaxHeight = hv.Number - b.ncWidth()*2
	}

	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgWidth := (contentAvailWidth - contentWidth) / len(zeroSpacers)
		contentWidth = contentAvailWidth
		for _, spacer := range zeroSpacers {
			width := spacer.Base().ncWidth()*2 + avgWidth
			spacer.Base().computedStyles.Width = NumberValue(width)
			// 如果是非spacer元素，则需要重新排版
			if _, ok := spacer.(*Spacer); !ok {
				spacer.Calc(width, contentMaxHeight)
				// 重新调整后高度可能变了。
				// contentMaxHeight = max(contentMaxHeight, spacer.Base().computedStyles.Height.Number)
			}
		}
	}

	// 最后再重新调整 XY
	offsetX := b.ncWidth()
	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
		child.Base().y = b.ncWidth()
		if computed.Align.String == `middle` {
			child.Base().y += (contentMaxHeight - child.Base().computedStyles.Height.Number) / 2
		}
		child.Base().x = offsetX
		offsetX += child.Base().computedStyles.Width.Number
	}

	if !computed.Width.IsNumber() {
		computed.Width = NumberValue(contentWidth)
	}
	if !computed.Height.IsNumber() {
		computed.Height = NumberValue(contentMaxHeight)
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

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iif(computed.Width.IsNumber(), computed.Width.Number, availWidth)
	boxMaxHeight := Iif(computed.Height.IsNumber(), computed.Height.Number, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - b.ncWidth()*2
	contentAvailHeight := boxMaxHeight - b.ncWidth()*2

	// 如果有 Spacer（未设定大小的），则同等大小地拼满。
	// zeroSpacers := []Box{}

	contentMaxHeight := 0

	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}

		// 如果没有设置尺寸，则总是占满。
		if !child.Base().computedStyles.Width.IsNumber() {
			child.Base().computedStyles.Width = NumberValue(contentAvailWidth)
		}
		// 如果父元素被设置了尺寸，总是给子元素设置同样的高度。
		if computed.Height.IsNumber() && !child.Base().computedStyles.Height.IsNumber() {
			// contentAvailHeight 此时就等于设置的高度-2倍不可用区
			child.Base().computedStyles.Height = NumberValue(contentAvailHeight)
		}

		// if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Height.Empty() {
		// 	zeroSpacers = append(zeroSpacers, spacer)
		// } else if child.Base().computedStyles.Spacer.Bool {
		// 	zeroSpacers = append(zeroSpacers, child)
		// } else {
		if text, ok := child.(*Text); ok {
			text.SegmentBlock(contentAvailWidth, contentAvailHeight)
		} else {
			child.Base().presetWidth(contentAvailWidth)
			child.Calc(contentAvailWidth, contentAvailHeight)
		}
		contentMaxHeight = max(contentMaxHeight, child.Base().computedStyles.Height.Number)
		// }
	}

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
	for _, child := range b.Children {
		if !displaying(child) {
			continue
		}
		child.Base().x = offsetX
		child.Base().y = offsetY
	}

	if !computed.Width.IsNumber() {
		computed.Width = NumberValue(contentAvailWidth)
	}
	if !computed.Height.IsNumber() {
		computed.Height = NumberValue(contentMaxHeight)
	}
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
		owner.Base().AppendChild(box)
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
	t.AppendChild(text)
	t.expandTextNodes()
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
	t.computedStyles.Width = NumberValue(t.textLineMaxWidth)
	t.computedStyles.Height = NumberValue(t.BlockHeight())
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

	t.computedStyles.Width = NumberValue(width)
	t.computedStyles.Height = NumberValue(line.MaxHeight)

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

func (t *BoldText) AppendChild(child any) {
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

func (t *ItalicText) AppendChild(child any) {
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
			Tag:      `img`,
		},
	}
	b.Class.box = b
	return b
}

func (b *Image) Set(key string, val string) error {
	switch key {
	case `src`:
		b.Src = val
		if b.Document != nil {
			b.Document.layoutDirty = true
		}
		return nil
	default:
		return b.BaseBox.Set(key, val)
	}
}

// 设置操作系统文件路径。
// 相对或者绝对均可。
func (b *Image) SetPath(path string) {
	u := (&url.URL{Scheme: `os`, Opaque: url.PathEscape(path)}).String()
	b.Set(`src`, u)
}

func (b *Image) Calc(availWidth, availHeight int) {
	if !b.computedStyles.Width.Empty() && !b.computedStyles.Height.Empty() {
		return
	}

	if availWidth == 0 || availHeight == 0 {
		return
	}

	if b.Src == `` {
		return
	}

	img, err := b.Document.loadImage(b.Src, 0, 0)
	if err != nil {
		return
	}

	width, height := 0, 0

	scaleW := float32(img.Width) / float32(availWidth)
	scaleH := float32(img.Height) / float32(availHeight)
	if scaleW > scaleH {
		width = availWidth
		height = int(float32(img.Height) / float32(scaleW))
	} else {
		width = int(float32(img.Width) / float32(scaleH))
		height = availHeight
	}

	b.computedStyles.Width = NumberValue(width)
	b.computedStyles.Height = NumberValue(height)
}

func (b *Image) Draw(canvas *Canvas) {
	if b.Src == `` {
		return
	}

	w := b.computedStyles.Width.Number
	h := b.computedStyles.Height.Number

	img, err := b.Document.loadImage(b.Src, w, h)
	if err != nil {
		return
	}

	canvas.DrawImage(img, w, h)
}

type Scroll struct {
	BaseBox

	// 列表的行数。
	rows int
	// 列表的列数。
	cols int
	gap  int

	count int
	bind  func(user any, index int)

	// child selection index
	rowIndex int
	colIndex int

	// 虚拟滚动的item顶部起始元素。
	itemOffset int
}

func NewScroll(doc *Document) *Scroll {
	b := &Scroll{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `scroll`,
		},
		rows:       1,
		cols:       1,
		rowIndex:   -1,
		colIndex:   -1,
		itemOffset: 0,
	}
	b.Class.box = b
	return b
}

// TODO 取消重复计算，大小不变的情况下只需要计算一次。
func (b *Scroll) Calc(availWidth, availHeight int) {
	computed := &b.computedStyles

	if len(b.Children) <= 0 {
		return
	}

	// 只有确定了大小才能决定子元素的大小和布局。
	if !(computed.Width.IsNumber() && computed.Height.IsNumber()) {
		return
	}

	var (
		contentAvailWidth  = computed.Width.Number - (b.ncWidth()*2 + (b.cols-1)*b.gap)
		contentAvailHeight = computed.Height.Number - (b.ncWidth()*2 + (b.rows-1)*b.gap)

		offsetX = b.ncWidth()
		offsetY = b.ncWidth()

		avgHeight = contentAvailHeight / b.rows
		avgWidth  = contentAvailWidth / b.cols
	)

	for i, child := range b.Children {
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

	b.adjust()
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
	b := &_ScrollChild{
		BaseBox: BaseBox{
			Document: doc,
			Tag:      `scroll-child`,
		},
	}
	b.Class.box = b
	return b
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
	base.x = x
	base.y = y
	base.computedStyles.Width = NumberValue(contentAvailWidth)
	base.computedStyles.Height = NumberValue(avgHeight)

	childContentAvailWidth := contentAvailWidth - b.ncWidth()*2
	childContentAvailHeight := avgHeight - b.ncWidth()*2

	child := base.Children[0]
	base = child.Base()
	base.computedStyles.Width = NumberValue(childContentAvailWidth)
	base.computedStyles.Height = NumberValue(childContentAvailHeight)

	// 没有数据的项实际是被隐藏的，被隐藏的项不会参与计算。
	// 所以如果代码运行到了这里，那一定是出现了内部逻辑错误。
	if b.dataIndex() < b.scroll.count {
		// 提前绑定上去才能提供数据、提供计算支撑。
		b.scroll.bind(b.user, b.dataIndex())
	}

	child.Calc(childContentAvailWidth, childContentAvailHeight)
	child.Base().x = b.ncWidth()
	child.Base().y = b.ncWidth()
}

func (b *Scroll) Set(key, value string) error {
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
		return b.BaseBox.Set(key, value)
	}
}

func (b *Scroll) SetItems(count int, create func() (root Box, user any), bind func(user any, index int)) {
	b.Children = nil
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
			wrapper := _NewScrollChild(b.Document)
			wrapper.Base().Set(`border-width`, `3`)
			wrapper.user = user
			wrapper.scroll = b
			wrapper.rowIndex = r
			wrapper.colIndex = c
			wrapper.AppendChild(box)
			b.AppendChild(wrapper)
		}
	}
}

func (b *Scroll) Navigate(name KeyName) {
	// 目前只支持上下滚动
	if !(name == Up || name == Down || name == Left || name == Right) {
		return
	}

	var (
		oldRowIndex   = b.rowIndex
		oldColIndex   = b.colIndex
		oldItemOffset = b.itemOffset
	)

	// 计算新的索引
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
			return
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
		if b.colIndex > 0 {
			b.colIndex--
		}
	case Right:
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

	// 如果没有变化，什么也不要做，减少刷新
	if (oldRowIndex == b.rowIndex && oldColIndex == b.colIndex) && oldItemOffset == b.itemOffset {
		return
	}

	// 取消选中原来的
	if childIndex := oldRowIndex*b.cols + oldColIndex; childIndex >= 0 && childIndex <= len(b.Children)-1 {
		b.Children[childIndex].Base().Class.Remove(`selected`)
	}

	// 更新选中
	if childIndex := b.rowIndex*b.cols + b.colIndex; childIndex >= 0 && childIndex <= len(b.Children)-1 {
		b.Children[childIndex].Base().Class.Add(`selected`)
	}

	// 如果物理列表没变（前面类名不会变化）、但是虚拟列表滚动了，
	// 则需要主动告知文档更新，否则不会重绘。
	if oldItemOffset != b.itemOffset {
		b.Document.paintDirty = true
	}
}

func (b *Scroll) curDataRow() int {
	return b.rowIndex + b.itemOffset/b.cols
}

// [0,rows-1]
func (b *Scroll) maxDataRow() int {
	return b.count/b.cols + Iif(b.count%b.cols > 0, 1, 0) - 1
}

func (b *Scroll) adjust() {
	for r := range b.rows {
		for c := range b.cols {
			child := b.Children[r*b.cols+c].(*_ScrollChild)
			display := child.dataIndex() <= b.count-1
			displayValue := child.computedStyles.Display
			if displayValue.Empty() || displayValue.Bool != display {
				// TODO 可以不用重新排版
				child.Set(`display`, fmt.Sprint(display))
			}
		}
	}
}

// 返回当前选中的数据索引。
func (b *Scroll) Index() int {
	if b.rowIndex < 0 {
		return -1
	}
	return b.rowIndex*b.cols + b.colIndex + b.itemOffset
}

func (b *Scroll) Count() int {
	return b.count
}

func (b *Scroll) Deselect() {
	childIndex := b.rowIndex*b.cols + b.colIndex
	if childIndex >= 0 && childIndex <= len(b.Children)-1 {
		b.Children[childIndex].Base().Class.Remove(`selected`)
	}
	b.rowIndex = -1
	b.colIndex = 0
	// 好像可以不用归位？
	b.itemOffset = 0
}
