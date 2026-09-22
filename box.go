package fbiw

import (
	"cmp"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"iter"
	"log"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "image/jpeg"
	"image/png"
	_ "image/png"
)

type Box interface {
	Base() *BaseBox

	// 返回文档中由 id="xxx" 指定的ID。
	//
	// 理论上应该整个文档内唯一。
	GetID() string

	// 返回此节点的标签名（区分大小写）。
	//
	// 例如 block、inline、text、img。
	GetTag() string

	// 返回文档中由 name="xxx" 指定的名字。
	//
	// 不同于form元素的name，这个name不用于css。
	// 不要求唯一，用于保存业务数据。
	GetName() string

	// 根据可用的宽度和高度计算自己实际的宽度和高度。
	// 优先采用 constraints.FixedWidth/FixedHeight，其次采用样式宽高。
	// 固定布局尺寸不能写回 computedStyles。
	//   - 自己写：并把宽度和高度写到 layoutBox.{width, height}。
	//   - 父亲写：layoutBox.{x, y}。
	// 子元素在排版时，它需要知道自己是否应该默认占满父元素（类似html的div），
	// 第三个参数用来决定此行为。
	Calc(availableWidth, availableHeight int, constraints Constraints)
	// 根据自身的 layoutBox 直接画。
	// layoutBox 的 {x,y} 相对于父元素。
	// 所以除根元素外（因为它是0，0），其在它Draw之前都要调整canvas到offset，
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

	// 返回当前的布局大小。
	//
	// 只应参考 Width 和 Height。X、Y 目前是相对于父元素的，不太有参考意义。
	GetLayoutBox() Rect
	// 由外部强制设置布局盒子尺寸。
	SetLayoutBox(layout Rect)

	// 自身是否处理显示状态。
	//
	// 不会判断祖先显示关系。
	// 即便任一祖先不显示，而自身显示，结果仍然是显示。
	IsDisplaying() bool

	// 返回所属文档。
	Document() *Document
	// 返回父亲盒子。
	Parent() Box
	// 返回孩子盒子列表。
	Children() []Box

	// 获取焦点。使其能接受键盘等事件处理。
	Activate()

	// 事件监听与分发。
	Listen(ty EventType, handler func(*Event)) func()
	ListenOptions(ty EventType, handler func(*Event), options EventOptions) func()
	Dispatch(ty EventType, data any)

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

	// UnboundedWidth/Height 表示该轴只提供排版参考尺寸，不限制自然内容尺寸。
	// 百分比仍使用 ParentContentWidth/Height 解析；显式尺寸和 Fixed* 仍然优先。
	// 无界轴不会因为 PrefersMax*、Spacer 或 flex-grow 被扩展到“全部可用空间”。
	UnboundedWidth  bool
	UnboundedHeight bool

	// 百分比参考父容器的完整内容区，不是兄弟元素占用后的剩余空间。
	ParentContentWidth  int
	ParentContentHeight int

	// 父布局指定的最终 border-box 尺寸。只接受 NumberLength 或空值；
	// 空值表示不强制，NumberLength(0) 表示明确的零尺寸。
	FixedWidth, FixedHeight Length
}

var _ Box = (*BaseBox)(nil)

// 所有可布局盒子的基类。
//
// 注意：由于是直接内嵌的（非指针），而有些内部字段本身也需要引用此盒子本身，
// 这种情况无法初始化。这个步骤放到了 transformNode。
type BaseBox struct {
	id   string
	tag  string
	name string

	// 用于存放用户任意数据。
	dataset map[string]any

	// 因为class有内部变量需要初始化，不知道咋写，先暂时隐藏，并提供同名方法。
	class Class

	document *Document
	parent   Box
	// PrevSibling Box
	// NextSibling Box
	children []Box

	// 每一个盒子都是事件容器对象。
	// 但是盒子是否可处理事件本身与focusable不直接有关。
	_EventTarget

	// 来自内联样式的值和样式计算的最终结果。
	inlineStyles      Styles
	inlineThemeColors map[styleProperty]Declaration
	// 样式计算结束后只读；布局阶段不得改写百分比等样式值。
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
	return BaseBox{
		document:       doc,
		tag:            tagName,
		computedStyles: Styles{Display: true},
	}
}

type Rect struct {
	X, Y          int
	Width, Height int
}

func (b *BaseBox) Base() *BaseBox {
	return b
}

func (b *BaseBox) GetID() string {
	return b.id
}
func (b *BaseBox) GetTag() string {
	return b.tag
}
func (b *BaseBox) GetName() string {
	return b.name
}

func (b *BaseBox) GetLayoutBox() Rect {
	return b.layoutBox
}

// SetLayoutBox lets container widgets assign their own and their children's
// final geometry without exposing BaseBox's layout storage.
func (b *BaseBox) SetLayoutBox(layout Rect) {
	b.layoutBox = layout
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
		for p := b.Parent(); p != nil; p = p.Parent() {
			if !yield(p) {
				break
			}
		}
	}
}
func (b *BaseBox) ancestorsForward() iter.Seq[Box] {
	var boxes []Box
	for p := b.Parent(); p != nil; p = p.Parent() {
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

func (b *BaseBox) Parent() Box {
	return b.parent
}

func (b *BaseBox) Children() []Box {
	return b.children
}

// func (b *BaseBox) lastChild() Box {
// 	if n := len(b.children); n > 0 {
// 		return b.children[n-1]
// 	}
// 	return nil
// }

func (b *BaseBox) AppendChild(child Box) {
	if child == nil {
		panic(`Child is nil`)
	}
	if child.Base()._EventTarget.box == nil {
		panic(`事件对象未完成初始化:` + reflect.TypeOf(child).String())
	}

	// prevLastChild := b.lastChild()
	b.children = append(b.children, child)
	child.Base().parent = b

	// if prevLastChild != nil {
	// 	prevLastChild.Base().NextSibling = child
	// 	child.Base().PrevSibling = prevLastChild
	// }

	if b.document != nil {
		b.document.layoutDirty = true
		b.document.style(b, true)
	}
}

type PropertySetter interface {
	SetProp(key string, val string) error
}

func (b *BaseBox) SetProp(key string, val string) error {
	inlineValue := val
	var themeDeclaration *Declaration
	if isColorProperty(key) {
		name, referenced, err := parseThemeColorReference(val)
		if err != nil {
			return err
		}
		if referenced {
			if b.document == nil {
				return fmt.Errorf(`内联主题颜色需要文档`)
			}
			_, theme := b.document.appTheme()
			if _, ok := theme.ResolveColor(name); !ok {
				return fmt.Errorf(`当前主题未定义颜色 %s`, name)
			}
			inlineValue = `none`
			declaration := Declaration{Name: key, Value: val}
			themeDeclaration = &declaration
		}
	}

	reInherit, reLayout, rePaint, err := b.inlineStyles.Set(key, inlineValue)
	if err == nil {
		property := stylePropertyByName(key)
		if themeDeclaration != nil {
			if b.inlineThemeColors == nil {
				b.inlineThemeColors = map[styleProperty]Declaration{}
			}
			b.inlineThemeColors[property] = *themeDeclaration
		} else if b.inlineThemeColors != nil {
			delete(b.inlineThemeColors, property)
		}
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
		b.id = val
		b.document.layoutDirty = true
		if b.document.root != nil {
			b.document.style(b, true)
		}
	case `class`:
		// 改class也会影响样式选择，所以需要重新排版
		// 会自动调用classChanged
		b.ClassSet(val)
	case `name`:
		b.name = val
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

// 本次布局解析后的宽高，不缓存、不回写样式。
// 保留 Length 的未指定状态，以区分 auto 与显式的 0。
type resolvedDimensions struct {
	Width, Height Length
}

// 如果 Length 值是百分比，从参考值中解析出最终结果。
func ResolveLayoutLength(value Length, reference int) Length {
	if value.IsPercentage() {
		return NumberLength(int64(max(0, reference)) * value.Number() / 100)
	}
	return value
}

// 解决自己的大小（不一定能成功解决，得看有没有指定）。
//
//   - 优先使用限制中固定的尺寸（常由flex box限定）
//   - 然后使用自身指定的尺寸
func (b *BaseBox) resolveDimensions(constraints Constraints) resolvedDimensions {
	size := resolvedDimensions{
		Width:  ResolveLayoutLength(b.computedStyles.Width, constraints.ParentContentWidth),
		Height: ResolveLayoutLength(b.computedStyles.Height, constraints.ParentContentHeight),
	}
	if constraints.FixedWidth.IsNumber() {
		size.Width = NumberLength(max(0, constraints.FixedWidth.Number()))
	}
	if constraints.FixedHeight.IsNumber() {
		size.Height = NumberLength(max(0, constraints.FixedHeight.Number()))
	}
	return size
}

func (b *BaseBox) paddingTop() int {
	return b.computedStyles.Padding.PaddingTop()
}

func (b *BaseBox) paddingRight() int {
	return b.computedStyles.Padding.PaddingRight()
}

func (b *BaseBox) paddingBottom() int {
	return b.computedStyles.Padding.PaddingBottom()
}

func (b *BaseBox) paddingLeft() int {
	return b.computedStyles.Padding.PaddingLeft()
}

func (b *BaseBox) InsetTop() int {
	return b.computedStyles.BorderWidth + b.paddingTop()
}

func (b *BaseBox) InsetRight() int {
	return b.computedStyles.BorderWidth + b.paddingRight()
}

func (b *BaseBox) InsetBottom() int {
	return b.computedStyles.BorderWidth + b.paddingBottom()
}

func (b *BaseBox) InsetLeft() int {
	return b.computedStyles.BorderWidth + b.paddingLeft()
}

func (b *BaseBox) HorizontalInsets() int {
	return b.InsetLeft() + b.InsetRight()
}

func (b *BaseBox) VerticalInsets() int {
	return b.InsetTop() + b.InsetBottom()
}

// TODO 重构：把所有元素的calc方法统一到这里分发。
// <block> 使用纵向布局，<inline> 使用横向布局，<flex> 使用弹性布局。
// display 只控制显示/隐藏，不能覆盖盒子类型。其它通用盒子默认横向布局。
//
// 只针对没有自己实现 Calc 方法的元素而言。如果自己实现了 Calc 方法（比如 List），
// 行为不受此约束。
func (b *BaseBox) Calc(availWidth, availHeight int, constraints Constraints) {
	if !b.IsDisplaying() {
		return
	}

	if b.tag == `block` {
		blockCalc(b, availWidth, availHeight, constraints)
	} else if b.tag == `inline` {
		inlineCalc(b, availWidth, availHeight, constraints)
	} else if b.tag == `flex` {
		flexCalc(b, availWidth, availHeight, constraints)
	} else {
		// 其它自己不实现的通通按inline来。
		inlineCalc(b, availWidth, availHeight, constraints)
	}
}

// 所有盒子通用的画法。
// 包括：Outline、Border、Background、Children。
func (b *BaseBox) Draw(canvas *Canvas) {
	b.DrawOptions(canvas, BaseBoxDrawOptions{})
}

type BaseBoxDrawOptions struct {
	NoBorder   bool
	NoChildren bool
}

func (b *BaseBox) DrawOptions(canvas *Canvas, options BaseBoxDrawOptions) {
	borderWidth := b.computedStyles.BorderWidth
	if options.NoBorder {
		borderWidth = 0
	}
	layoutWidth := b.layoutBox.Width
	layoutHeight := b.layoutBox.Height

	if outlineWidth := b.computedStyles.OutlineWidth; outlineWidth > 0 {
		if outlineColor := b.computedStyles.OutlineColor; b.computedStyles.has(propertyOutlineColor) && !outlineColor.IsNone() {
			// Outline（外边框）是不算在盒子本身的width和height内的，
			// 所以要负向（左上）偏移到父元素。
			canvas := canvas.Offset(-outlineWidth, -outlineWidth)
			// 同时，宽度和高度有要向右下偏移。
			width := layoutWidth + outlineWidth*2
			height := layoutHeight + outlineWidth*2
			canvas.DrawBorder(outlineColor, width, height, outlineWidth)
		}
	}

	// 默认都是 border-box，所以以实际的宽和高为准。
	if bcv := b.computedStyles.BorderColor; borderWidth > 0 && b.computedStyles.has(propertyBorderColor) && !bcv.IsNone() {
		canvas.DrawBorder(bcv, layoutWidth, layoutHeight, borderWidth)
	}

	if src := b.computedStyles.BackgroundImage; src != `` {
		width := layoutWidth - borderWidth*2
		height := layoutHeight - borderWidth*2
		canvas := canvas.Offset(borderWidth, borderWidth)

		// 背景图片暂时只显示首帧（如果是GIF的话），像素本来就低，太丑了。
		result, err := b.document.loadImageSync(nil, src, width, height, ImageDecodeOptions{TrimTransparentBorder: true})
		var img DecodedImage
		switch value := result.(type) {
		case DecodedImage:
			img = value
		case *_AnimatedGIF:
			img = value.frames[0]
		}

		if err == nil && len(img.Pixels) > 0 {
			canvas.DrawImage(img)
		} else {
			b.document.loadImageAsync(nil, src, width, height,
				ImageDecodeOptions{TrimTransparentBorder: true},
				func(_ any, err error) {
					if err == nil {
						b.document.RequestPaint()
					}
				},
			)
		}
	} else if bcv := b.computedStyles.BackgroundColor; b.computedStyles.has(propertyBackgroundColor) && !bcv.IsNone() {
		canvas.Offset(borderWidth, borderWidth).FillRect(
			0, 0,
			layoutWidth-borderWidth*2,
			layoutHeight-borderWidth*2,
			bcv,
		)
	}

	if !options.NoChildren {
		for _, child := range b.children {
			if !child.IsDisplaying() {
				continue
			}
			layout := child.Base().layoutBox
			canvas := canvas.Offset(layout.X, layout.Y)
			child.Draw(canvas)
		}
	}
}

func (b *BaseBox) IsDisplaying() bool {
	styles := &b.Base().computedStyles
	// 保留零值 Styles 的默认显示语义，只有显式设置 false 才隐藏。
	return !styles.has(propertyDisplay) || styles.Display
}

type IntrinsicWidths struct {
	Min       int
	Preferred int
}

// MeasureIntrinsicWidths measures a box without imposing table-specific
// policy. It is available to external container widgets implementing their
// own content-driven layout.
func MeasureIntrinsicWidths(child Box, referenceWidth, referenceHeight int) IntrinsicWidths {
	minimum := 0
	if text, ok := child.(*Text); ok {
		for i := range text.textRuns {
			run := &text.textRuns[i]
			faces := text.document.LoadFaces(run.Owner)
			for data := run.Data; len(data) > 0; {
				_, size := utf8.DecodeRuneInString(data)
				_, width, err := SegmentText(data[:size], 1<<24, faces)
				if err == nil {
					minimum = max(minimum, width)
				}
				data = data[size:]
			}
		}
		minimum += text.HorizontalInsets()
		if width := ResolveLayoutLength(text.computedStyles.Width, referenceWidth); width.IsNumber() {
			minimum = max(minimum, int(width.Number()))
		}
	} else {
		child.Calc(1, referenceHeight, Constraints{
			ParentContentWidth:  max(0, referenceWidth),
			ParentContentHeight: max(0, referenceHeight),
			UnboundedHeight:     true,
		})
		minimum = child.GetLayoutBox().Width
	}
	child.Calc(max(1, referenceWidth), referenceHeight, Constraints{
		ParentContentWidth:  max(0, referenceWidth),
		ParentContentHeight: max(0, referenceHeight),
		UnboundedWidth:      true,
		UnboundedHeight:     true,
	})
	return IntrinsicWidths{Min: minimum, Preferred: max(minimum, child.GetLayoutBox().Width)}
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
	size := b.resolveDimensions(constraints)
	computed := &b.computedStyles

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iif(size.Width.IsNumber(), int(size.Width.Number()), availWidth)
	boxMaxHeight := Iif(size.Height.IsNumber(), int(size.Height.Number()), availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - b.HorizontalInsets()
	contentAvailHeight := boxMaxHeight - b.VerticalInsets()

	// 当前实际占用高度
	contentHeight := 0
	// 子元素实际占用的最大宽度。PrefersMaxWidth=false 时，Block 据此收缩。
	contentMaxWidth := 0

	// 如果有 Spacer（未设定大小的），则留到后面均匀地铺满。
	zeroSpacers := []Box{}

	for _, child := range b.children {
		if !child.IsDisplaying() {
			continue
		}

		if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Height.Empty() && !constraints.UnboundedHeight {
			zeroSpacers = append(zeroSpacers, spacer)
			contentHeight += spacer.VerticalInsets()
		} else if child.Base().computedStyles.Spacer && !constraints.UnboundedHeight {
			zeroSpacers = append(zeroSpacers, child)
			contentHeight += child.Base().VerticalInsets()
		} else {
			childAvailHeight := contentAvailHeight - contentHeight
			if constraints.UnboundedHeight {
				childAvailHeight = contentAvailHeight
			}
			if text, ok := child.(*Text); ok {
				text.Calc(contentAvailWidth, childAvailHeight, Constraints{
					ParentContentWidth:  max(0, contentAvailWidth),
					ParentContentHeight: max(0, contentAvailHeight),
					UnboundedWidth:      constraints.UnboundedWidth,
					UnboundedHeight:     constraints.UnboundedHeight,
				})
			} else {
				child.Calc(contentAvailWidth, childAvailHeight, Constraints{
					ParentContentWidth:  max(0, contentAvailWidth),
					ParentContentHeight: max(0, contentAvailHeight),
					PrefersMaxWidth:     constraints.PrefersMaxWidth,
					PrefersMaxHeight:    false,
					UnboundedWidth:      constraints.UnboundedWidth,
					UnboundedHeight:     constraints.UnboundedHeight,
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
			height := spacer.Base().VerticalInsets() + avgHeight
			// 如果是非spacer元素，则需要重新排版
			if _, ok := spacer.(*Spacer); !ok {
				spacer.Calc(contentAvailWidth, height, Constraints{
					ParentContentWidth:  max(0, contentAvailWidth),
					ParentContentHeight: max(0, contentAvailHeight),
					PrefersMaxWidth:     true,
					PrefersMaxHeight:    true,
				})
			} else {
				spacer.Base().layoutBox.Width = contentAvailWidth
				spacer.Base().layoutBox.Height = height
			}
		}
	}

	for _, child := range b.children {
		if child.IsDisplaying() {
			contentMaxWidth = max(contentMaxWidth, child.Base().layoutBox.Width)
		}
	}

	// if hv := b.computedStyles.Height; hv.IsNumber() && hv.Number()-b.verticalInsets() > contentHeight {
	// 	contentHeight = hv.Number() - b.verticalInsets()
	// }

	// 此时已经可以确定容器本身的大小了。
	actualWidth := b.HorizontalInsets() + contentMaxWidth
	if !constraints.UnboundedWidth {
		actualWidth = min(availWidth, actualWidth)
	}
	b.layoutBox.Width = resolveMeasuredSize(size.Width, availWidth, constraints.PrefersMaxWidth, actualWidth, constraints.UnboundedWidth)
	b.layoutBox.Height = resolveMeasuredSize(size.Height, availHeight, constraints.PrefersMaxHeight, b.VerticalInsets()+contentHeight, constraints.UnboundedHeight)

	// 最后再重新对齐子元素

	// 先是垂直对齐。
	// 对于block来说，垂直方向不止一个元素，需要整体平移。
	offsetY := b.InsetTop()
	if align := computed.Align; align == `both` || align == `middle` {
		offsetY += (b.layoutBox.Height - b.VerticalInsets() - contentHeight) / 2
	}

	// 然后是水平对齐。
	// 水平对齐需要对每一个子元素单独改（因为它们是在垂直方向排列的，不在一条水平线上）。
	alignCenter := computed.Align == `both` || computed.Align == `center`

	for _, child := range b.children {
		if !child.IsDisplaying() {
			continue
		}

		layout := &child.Base().layoutBox

		// 水平居中
		offsetX := b.InsetLeft()
		if alignCenter {
			offsetX += (contentAvailWidth - layout.Width) / 2
		}
		layout.X = offsetX

		// 垂直居中，对所有元素同时偏移。
		layout.Y = offsetY
		offsetY += layout.Height
	}
}

type Inline struct {
	BaseBox
}

func NewInline(doc *Document) *Inline {
	return &Inline{BaseBox: NewBaseBox(doc, `inline`)}
}

func inlineCalc(b *BaseBox, availWidth, availHeight int, constraints Constraints) {
	size := b.resolveDimensions(constraints)
	computed := &b.computedStyles

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iif(size.Width.IsNumber(), int(size.Width.Number()), availWidth)
	boxMaxHeight := Iif(size.Height.IsNumber(), int(size.Height.Number()), availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - b.HorizontalInsets()
	contentAvailHeight := boxMaxHeight - b.VerticalInsets()

	// 当前实际占用宽度
	contentWidth := 0

	// 实际最高占用。
	contentMaxHeight := 0

	// 如果有 Spacer（未设定大小的），则均匀地铺满。
	zeroSpacers := []Box{}

	for _, child := range b.children {
		if !child.IsDisplaying() {
			continue
		}

		if spacer, ok := child.(*Spacer); ok && spacer.computedStyles.Width.Empty() && !constraints.UnboundedWidth {
			zeroSpacers = append(zeroSpacers, spacer)
			contentWidth += spacer.HorizontalInsets()
		} else if child.Base().computedStyles.Spacer && !constraints.UnboundedWidth {
			zeroSpacers = append(zeroSpacers, child)
			contentWidth += child.Base().HorizontalInsets()
		} else {
			if text, ok := child.(*Text); ok {
				remainingWidth := contentAvailWidth - contentWidth
				if text.marquee.axis != `` || constraints.UnboundedWidth || constraints.UnboundedHeight {
					// marquee 需要完整测量内容、确定自己的视口并更新动画。
					// 普通 inline 文本仍保留原来只切一行的布局语义。
					text.Calc(remainingWidth, contentAvailHeight, Constraints{
						ParentContentWidth:  max(0, remainingWidth),
						ParentContentHeight: max(0, contentAvailHeight),
						UnboundedWidth:      constraints.UnboundedWidth,
						UnboundedHeight:     constraints.UnboundedHeight,
					})
				} else {
					// 只处理了一行，如果要wrap，才能继续处理。
					text.clearStates()
					text.SegmentInline(remainingWidth, contentAvailHeight)
				}
			} else {
				child.Calc(contentAvailWidth-contentWidth, contentAvailHeight, Constraints{
					ParentContentWidth:  max(0, contentAvailWidth),
					ParentContentHeight: max(0, contentAvailHeight),
					PrefersMaxWidth:     false,
					PrefersMaxHeight:    false,
					UnboundedWidth:      constraints.UnboundedWidth,
					UnboundedHeight:     constraints.UnboundedHeight,
				})
			}

			childWidth := child.Base().layoutBox.Width
			childHeight := child.Base().layoutBox.Height
			contentWidth += childWidth
			contentMaxHeight = max(contentMaxHeight, childHeight)
		}
	}

	// 指定了高度，且内容实际没有高度高，则扩展到指定高度。
	if hv := size.Height; hv.IsNumber() && int(hv.Number())-b.VerticalInsets() > contentMaxHeight {
		contentMaxHeight = int(hv.Number()) - b.VerticalInsets()
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
			width := spacer.Base().HorizontalInsets() + avgWidth
			// 如果是非spacer元素，则需要重新排版
			if _, ok := spacer.(*Spacer); !ok {
				spacer.Calc(width, contentMaxHeight, Constraints{
					ParentContentWidth:  max(0, contentAvailWidth),
					ParentContentHeight: max(0, contentAvailHeight),
					PrefersMaxWidth:     true,
					PrefersMaxHeight:    true,
				})
			} else {
				spacer.Base().layoutBox.Width = width
			}
		}
	}

	// 此时已经可以确定容器本身的大小了。
	actualWidth := contentWidth + b.HorizontalInsets()
	actualHeight := contentMaxHeight + b.VerticalInsets()
	if !constraints.UnboundedWidth {
		actualWidth = min(availWidth, actualWidth)
	}
	if !constraints.UnboundedHeight {
		actualHeight = min(availHeight, actualHeight)
	}
	b.layoutBox.Width = resolveMeasuredSize(size.Width, availWidth, constraints.PrefersMaxWidth, actualWidth, constraints.UnboundedWidth)
	b.layoutBox.Height = resolveMeasuredSize(size.Height, availHeight, constraints.PrefersMaxHeight, actualHeight, constraints.UnboundedHeight)

	// 最后再重新对齐子元素。
	offsetX := b.InsetLeft()

	// 先是水平对齐。
	// 对于inline来说，水平方向不止一个元素，需要整体平移。
	// BUG: inline 是可以跨行的。这里没有考虑多行元素的对齐。
	if align := computed.Align; align == `both` || align == `center` {
		offsetX += (b.layoutBox.Width - b.HorizontalInsets() - contentWidth) / 2
	}

	// 然后是垂直对齐。也只处理了单行元素。
	// 垂直对齐需要对每一个子元素单独改（因为它们是水平排列的，不在一条竖线上）。
	alignMiddle := computed.Align == `both` || computed.Align == `middle`

	for _, child := range b.children {
		if !child.IsDisplaying() {
			continue
		}

		layout := &child.Base().layoutBox
		layout.X = offsetX
		offsetX += layout.Width

		// 每个元素的起点均是内容可用区开始。
		offsetY := b.InsetTop()

		if alignMiddle {
			// 按最终内容区居中。即使子元素高于内容区，也应向上、向下
			// 等量溢出，而不是贴在上内边距上。
			contentHeight := b.layoutBox.Height - b.VerticalInsets()
			offsetY += (contentHeight - layout.Height) / 2
		}

		layout.Y = offsetY
	}
}

// 单行弹性布局容器。
type Flex struct {
	BaseBox
}

func NewFlex(doc *Document) *Flex {
	return &Flex{BaseBox: NewBaseBox(doc, `flex`)}
}

// 单行弹性布局：先测量基础尺寸，再分配正的剩余空间，最后对齐。
// 暂不实现 shrink、basis、wrap；尺寸不足时允许溢出，不产生负的分配尺寸。
//
// 可以把它理解为三个问题，按顺序求解：
//  1. 每个孩子本来需要多大？容器主轴上还剩多少空间？
//  2. 分配剩余空间后，孩子变成多大？容器交叉轴需要多大？
//  3. 大小确定后，各个孩子应该放在哪里？
//
// 不能只遍历一次：例如横排文本分到了更多宽度，行数可能减少，高度也会变；
// 而父容器如果高度由内容决定，就必须等文本重新断行后才能确定高度。
// 当前 Calc 同时做测量和布局，所以这里通过必要时再次调用 Calc 完成这些阶段，
// 不是引入另一份可修改的样式，也不是通过不断迭代来求收敛。
func flexCalc(b *BaseBox, availWidth, availHeight int, constraints Constraints) {
	// 第一阶段：统一坐标轴，准备本轮测量的可用空间。
	// size 是当前容器自身的尺寸要求（父布局强制值优先，其次为解析后的样式），
	// 不是孩子的尺寸，也还不是当前容器的最终 layoutBox。
	size := b.resolveDimensions(constraints)
	styles := &b.computedStyles
	column := styles.FlexDirection == `column`

	// 后面的算法只谈 main（主轴，排列方向）和 cross（交叉轴）：
	//   row：main=宽/X，cross=高/Y；
	//   column：main=高/Y，cross=宽/X。
	// axes 在 row 下原样返回，在 column 下交换两个值。交换两次会还原，
	// 所以既能把 (width,height) 转成 (main,cross)，也能反过来转换；坐标同理。
	axes := func(width, height int) (int, int) {
		if column {
			return height, width
		}
		return width, height
	}
	mainStyle, crossStyle := size.Width, size.Height
	preferMain, preferCross := constraints.PrefersMaxWidth, constraints.PrefersMaxHeight
	unboundedMain, unboundedCross := constraints.UnboundedWidth, constraints.UnboundedHeight
	if column {
		mainStyle, crossStyle = size.Height, size.Width
		preferMain, preferCross = preferCross, preferMain
		unboundedMain, unboundedCross = unboundedCross, unboundedMain
	}

	// 这几组变量的区别：
	//   avail*：父布局提供给当前容器的可用 border-box 空间。
	//   inset*：当前容器在该轴两侧的 padding + border 总和。
	//   cap*：用于测量孩子的内容区空间；此时还没有根据内容收缩容器。
	// resolveSize(..., true, 0) 表示有明确尺寸就用它，否则先采用全部可用空间。
	// cap 不是严格上限：孩子仍可用显式尺寸超出它。本实现没有 shrink。
	availMain, availCross := axes(max(0, availWidth), max(0, availHeight))
	insetMain, insetCross := axes(b.HorizontalInsets(), b.VerticalInsets())
	capMain := max(0, resolveSize(mainStyle, availMain, true, 0)-insetMain)
	capCross := max(0, resolveSize(crossStyle, availCross, true, 0)-insetCross)
	contentWidth, contentHeight := axes(capMain, capCross)

	// 所有孩子的百分比都参考同一个完整内容区，不能扣掉前面兄弟占用的空间。
	// 这里没有设置 PrefersMax*，让孩子先报告内容/显式尺寸，而不是要求它填满。
	// 这份百分比参考在本次 flexCalc 内保持不变；后续 Fixed* 是分配结果，
	// 不能拿分配给单个孩子的空间重新充当百分比参考。
	// 内容自适应容器中百分比尺寸的循环依赖，目前仍采用这个简化规则处理。
	childConstraints := Constraints{
		ParentContentWidth:  contentWidth,
		ParentContentHeight: contentHeight,
		UnboundedWidth:      constraints.UnboundedWidth,
		UnboundedHeight:     constraints.UnboundedHeight,
	}
	type item struct {
		box     Box
		main    int     // 初始为测得的基础尺寸；grow 分配后原地更新为目标主轴尺寸。
		grow    float64 // 本项分配占剩余空间的权重，0 表示不增长。
		align   string  // 已合并 align-self / align-items / 默认值的最终对齐方式。
		stretch bool    // 只有要求 stretch 且未显式指定交叉轴尺寸，才允许拉伸。
	}

	// 第二阶段：测量所有可见孩子的基础尺寸。
	// 这里的“基础尺寸”是现有 Calc 在上述可用空间下给出的结果，
	// 并不是浏览器的 min-content/max-content 或完整 flex-basis 算法。
	items := make([]item, 0, len(b.children))
	gap := max(0, styles.Gap)
	baseMain, maxGrow := 0, 0.0
	for _, child := range b.children {
		if !child.IsDisplaying() {
			continue
		}
		child.Calc(contentWidth, contentHeight, childConstraints)
		layout := child.GetLayoutBox()
		main, _ := axes(layout.Width, layout.Height)
		main = max(0, main)

		cs := child.GetComputedStyles()
		grow := cs.FlexGrow
		// 字符串解析已经校验过；这里还防御直接调用样式 setter 的非法值。
		if grow < 0 || math.IsNaN(grow) || math.IsInf(grow, 0) {
			grow = 0
		}
		align := cs.AlignSelf
		// 优先用孩子自己的 align-self；auto 才回退到父容器的 align-items。
		if align == `` || align == `auto` {
			align = styles.AlignItems
		}
		if align == `` {
			align = `stretch`
		}
		crossLength := cs.Height
		if column {
			crossLength = cs.Width
		}
		// 判断的是原始样式是否“未指定”，而不是测量结果是否为 0。
		// height=0 和 height=50% 都是明确要求，不能因为 stretch 将它们覆盖。

		items = append(items, item{
			child, main, grow, align,
			align == `stretch` && crossLength.Empty(),
		})
		baseMain += main
		maxGrow = max(maxGrow, grow)
	}
	// gap 只出现在相邻可见孩子之间：N 个孩子有 N-1 个 gap，空容器为 0。
	baseMain += max(0, len(items)-1) * gap

	// 第三阶段：确定容器主轴尺寸，再按 grow 分配正剩余空间。
	// boxMain 是包括 padding/border 的最终尺寸，contentMain 才能分给孩子。
	// 明确尺寸优先；否则 preferMain 决定填满还是按内容收缩。
	// 因而 grow 本身不会让一个内容收缩容器主动扩展到全部可用空间。
	actualMain := baseMain + insetMain
	if !unboundedMain {
		actualMain = min(availMain, actualMain)
	}
	boxMain := max(0, resolveMeasuredSize(mainStyle, availMain, preferMain, actualMain, unboundedMain))
	contentMain := max(0, boxMain-insetMain)
	free := max(0, contentMain-baseMain)
	// free 只取正数：基础尺寸已经放不下时，不会“负增长”或缩小孩子。
	// 例如内容宽 100，两个基础宽度 10、20，gap=10，则 free=60；
	// grow=1、2 时各增加 20、40，最终宽度为 30、60，而不是 1:2 均分 100。
	//
	// 先让所有权重除以最大权重，比例不变，但避免多个极大浮点数相加溢出。
	// 此处把 grow 当相对权重：有正权重就分完 free，不采用 CSS 中权重和小于 1
	// 时可能只分配部分剩余空间的规则。
	totalGrow := 0.0
	if maxGrow > 0 {
		for _, it := range items {
			totalGrow += it.grow / maxGrow
		}
	}
	allocated, cumulative := 0, 0.0
	// 不逐项计算并截断份额，而是计算“到当前项为止累计应分多少”。
	// 例如 100 像素按三个相等权重分配：累计值为 33、66、100，
	// 相邻累计值之差就是 33、33、34。这样余下的 1 像素不会丢掉。
	// allocated 只记录增长部分；it.main 还包含最初的基础尺寸。
	for i := range items {
		it := &items[i]
		if it.grow > 0 && free > 0 {
			cumulative += it.grow / maxGrow
			next := min(free, int(float64(free)*(cumulative/totalGrow)))
			it.main += next - allocated
			allocated = next
		}
	}

	// 第四阶段：将改变后的主轴尺寸交给孩子，重新取得交叉轴尺寸。
	// 例如 row 中孩子宽度从 14 增到 84，必须让文本重新断行、嵌套容器重新排版，
	// 不能只修改孩子 layoutBox.Width，否则内部内容仍按旧宽度排列。
	maxCross := 0
	for _, it := range items {
		layout := it.box.GetLayoutBox()
		measuredMain, _ := axes(layout.Width, layout.Height)
		// 未改变尺寸时复用测量结果，避免嵌套 Flex 重复遍历整棵子树。
		if measuredMain != it.main {
			cc := childConstraints
			// 固定值比孩子的样式宽高优先，但不改写样式。这里只固定主轴，
			// 让交叉轴仍能报告内容需要的尺寸，稍后再处理 stretch。
			if column {
				cc.FixedHeight = NumberLength(it.main)
			} else {
				cc.FixedWidth = NumberLength(it.main)
			}
			width, height := axes(it.main, capCross)
			it.box.Calc(width, height, cc)
			layout = it.box.GetLayoutBox()
		}
		_, cross := axes(layout.Width, layout.Height)
		maxCross = max(maxCross, cross)
	}

	// 单行布局的自然交叉轴尺寸是孩子们的最大值，不是它们的总和。
	// 到此才能决定内容自适应的容器高度（column 时为宽度）。
	actualCross := maxCross + insetCross
	if !unboundedCross {
		actualCross = min(availCross, actualCross)
	}
	boxCross := max(0, resolveMeasuredSize(crossStyle, availCross, preferCross, actualCross, unboundedCross))
	contentCross := max(0, boxCross-insetCross)
	b.layoutBox.Width, b.layoutBox.Height = axes(boxMain, boxCross)

	// 第五阶段：容器交叉轴大小确定后，处理需要 stretch 的孩子。
	// 必须再次 Calc，让孩子内部内容也看到新的交叉轴尺寸；同时固定两个轴，
	// 防止拉伸时丢失第三阶段分配好的主轴尺寸。
	// 此阶段不再反向增大容器，也不重新分配 grow，避免父子尺寸互相追逐。
	for _, it := range items {
		if !it.stretch {
			continue
		}
		layout := it.box.GetLayoutBox()
		_, cross := axes(layout.Width, layout.Height)
		if cross == contentCross {
			continue
		}
		width, height := axes(it.main, contentCross)
		cc := childConstraints
		cc.FixedWidth, cc.FixedHeight = NumberLength(width), NumberLength(height)
		it.box.Calc(width, height, cc)
	}

	// 第六阶段：尺寸已确定，只写坐标，不再测量。
	// remaining 与 free 不同：它是 grow 分配完以后还剩多少空间，可为负数。
	// 有 grow 时通常为 0；无 grow 时交给 justify-content；为负则表示溢出。
	remaining := contentMain - baseMain - allocated
	startMain, startCross := axes(b.InsetLeft(), b.InsetTop())
	// position 是前面所有孩子的主轴尺寸 + 固定 gap 的累计值；
	// extra 是 justify-content 额外加入的偏移。两者分开，gap 不会被重复计算。
	position := 0
	for i, it := range items {
		// 仍用累计偏移而不是逐段截断，space-* 的像素误差不会越积越多。
		// N 个孩子、剩余空间 R，各模式的第 i 项额外偏移为：
		//   between：R*i/(N-1)，首尾贴边；只有一个孩子时靠起点。
		//   around：R*(2*i+1)/(2*N)，两端空白为项间空白的一半。
		//   evenly：R*(i+1)/(N+1)，两端与项间空白相同。
		// 空容器不会进入循环，因此 around/evenly 没有除零问题。
		// R<0 时不制造负的“均匀间距”：between 靠起点，around/evenly 居中溢出；
		// end/center 则保留负偏移，分别让内容向起点侧溢出或两侧居中溢出。
		extra := 0
		switch styles.JustifyContent {
		case `end`, `flex-end`:
			extra = remaining
		case `center`:
			extra = remaining / 2
		case `space-between`:
			if len(items) > 1 {
				extra = max(0, remaining) * i / (len(items) - 1)
			}
		case `space-around`:
			if remaining < 0 {
				extra = remaining / 2
			} else {
				extra = remaining * (2*i + 1) / (2 * len(items))
			}
		case `space-evenly`:
			if remaining < 0 {
				extra = remaining / 2
			} else {
				extra = remaining * (i + 1) / (len(items) + 1)
			}
		}
		layout := &it.box.Base().layoutBox
		_, cross := axes(layout.Width, layout.Height)
		crossOffset := 0
		// 交叉轴对齐是逐个孩子计算；主轴对齐则需要考虑整组孩子。
		// start/stretch 的偏移均为 0，stretch 的尺寸调整已经在上一阶段完成。
		// 孩子比容器更大时允许负偏移，保证 end/center 的溢出方向仍然正确。
		switch it.align {
		case `end`, `flex-end`:
			crossOffset = contentCross - cross
		case `center`:
			crossOffset = (contentCross - cross) / 2
		}
		layout.X, layout.Y = axes(startMain+position+extra, startCross+crossOffset)
		// 坐标加上父容器左/上 inset，最后从抽象轴转换回实际 X/Y。
		// 最后一项之后虽然也累加 gap，但后面不再使用 position，不影响尺寸。
		position += it.main + gap
	}
}

// computed > available > actual
func resolveSize(computed Length, available int, prefersAvailable bool, actual int) int {
	if computed.IsNumber() {
		return int(computed.Number())
	}
	if prefersAvailable {
		return available
	}
	return actual
}

func resolveMeasuredSize(computed Length, available int, prefersAvailable bool, actual int, unbounded bool) int {
	if unbounded {
		prefersAvailable = false
	}
	return resolveSize(computed, available, prefersAvailable, actual)
}

func constrainNaturalSize(actual, available int, unbounded bool) int {
	if unbounded {
		return actual
	}
	return min(actual, available)
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
	if !b.IsDisplaying() {
		return
	}
	size := b.resolveDimensions(constrains)

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iif(size.Width.IsNumber(), int(size.Width.Number()), availWidth)
	boxMaxHeight := Iif(size.Height.IsNumber(), int(size.Height.Number()), availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - b.HorizontalInsets()
	contentAvailHeight := boxMaxHeight - b.VerticalInsets()

	// 如果有 Spacer（未设定大小的），则同等大小地拼满。
	// zeroSpacers := []Box{}

	contentMaxWidth := 0
	contentMaxHeight := 0

	for _, child := range b.children {
		if !child.IsDisplaying() {
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
			text.Calc(contentAvailWidth, contentAvailHeight, Constraints{
				ParentContentWidth:  max(0, contentAvailWidth),
				ParentContentHeight: max(0, contentAvailHeight),
				UnboundedWidth:      constrains.UnboundedWidth,
				UnboundedHeight:     constrains.UnboundedHeight,
			})
		} else {
			child.Calc(contentAvailWidth, contentAvailHeight, Constraints{
				ParentContentWidth:  max(0, contentAvailWidth),
				ParentContentHeight: max(0, contentAvailHeight),
				PrefersMaxWidth:     b.fill,
				PrefersMaxHeight:    b.fill,
				UnboundedWidth:      constrains.UnboundedWidth,
				UnboundedHeight:     constrains.UnboundedHeight,
			})
		}
		contentMaxWidth = max(contentMaxWidth, child.Base().layoutBox.Width)
		contentMaxHeight = max(contentMaxHeight, child.Base().layoutBox.Height)
		// }
	}

	// for _, child := range b.Children {
	// 	// 直接铺满？
	// 	// child.Base().layoutBox.Width = contentAvailWidth
	// 	child.Base().layoutBox.Height = contentMaxHeight
	// }
	// if hv := b.computedStyles.Height; hv.IsNumber() && hv.Number()-b.verticalInsets() > contentMaxHeight {
	// 	contentMaxHeight = hv.Number() - b.verticalInsets()
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
	offsetX := b.InsetLeft()
	offsetY := b.InsetTop()
	for _, child := range b.children {
		if !child.IsDisplaying() {
			continue
		}
		child.Base().layoutBox.X = offsetX
		child.Base().layoutBox.Y = offsetY
	}

	actualWidth := contentAvailWidth
	if constrains.UnboundedWidth {
		actualWidth = b.HorizontalInsets() + contentMaxWidth
	}
	b.layoutBox.Width = resolveMeasuredSize(size.Width, availWidth, constrains.PrefersMaxWidth, actualWidth, constrains.UnboundedWidth)
	b.layoutBox.Height = resolveMeasuredSize(size.Height, availHeight, constrains.PrefersMaxHeight, contentMaxHeight, constrains.UnboundedHeight)
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

func (l _TextLine) Width() int {
	width := 0
	for _, fragment := range l.Fragments {
		width += fragment.layoutBox.Width
	}
	return width
}

func (l _TextLine) horizontalOffset(contentWidth int, align string) int {
	if align != `center` && align != `both` {
		return 0
	}
	return max(0, contentWidth-l.Width()) / 2
}

type _TextParts struct {
	// 副本一份子节点，方便把纯文本节点也保存进来，
	// 这样可以维护原始顺序，而不用把纯文本节点挂
	// 在真实的dom树上。
	//
	// 注意：Box也保存这里，所以它的样式也在这里。
	children []any // string | Box
}

// _TextMarquee 保存 Text 自动滚动的配置与运行时状态。
type _TextMarquee struct {
	axis       string
	speed      float64
	pause      time.Duration
	count      int
	completed  int
	running    bool
	offset     float64
	direction  float64
	last       time.Time
	pauseUntil time.Time
	cancel     func()
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

	// 对于多行文本，表示当前绘制行的垂直滚动偏移行数。
	textDrawLineOffset int

	// 固定轴向尺寸且内容溢出时自动逐像素往返滚动。
	marquee _TextMarquee
}

func NewText(doc *Document) *Text {
	return &Text{
		BaseBox: NewBaseBox(doc, `text`),
		marquee: _TextMarquee{
			speed:     60,
			pause:     time.Second,
			running:   true,
			direction: 1,
		},
	}
}

// SetMarqueeRunning 启动或停止自动滚动。停止时回到起点；再次启动时从
// 起点的停留阶段开始。内容未溢出或未配置 marquee 时不会申请动画帧。
func (t *Text) SetMarqueeRunning(running bool) {
	if t.marquee.running == running {
		if !running {
			t.resetMarqueePosition()
		} else if t.marquee.count > 0 && t.marquee.completed >= t.marquee.count {
			t.resetMarqueePosition()
			t.updateMarquee()
			t.document.RequestPaint()
		}
		return
	}
	t.marquee.running = running
	if running {
		t.updateMarquee()
	} else {
		t.stopMarquee()
		t.resetMarqueePosition()
	}
	t.document.RequestPaint()
}

func (t *Text) resetMarqueePosition() {
	t.marquee.offset = 0
	t.marquee.direction = 1
	t.marquee.completed = 0
}

// 有时候会发现文本莫名其妙被重置了，改为函数可方便观察。
func (t *Text) setDrawLineOffset(offset int) {
	t.textDrawLineOffset = offset
}

func (t *Text) MarqueeRunning() bool {
	return t.marquee.running
}

func (t *Text) SetProp(key, value string) error {
	switch key {
	case `marquee`:
		if value != `horizontal` && value != `vertical` && value != `` {
			return fmt.Errorf(`marquee 属性只支持 horizontal 或 vertical：%s`, value)
		}
		t.marquee.axis = value
		if t.marquee.axis == `` {
			t.stopMarquee()
			t.resetMarqueePosition()
		}
		t.document.RequestLayout()
		return nil
	case `marquee-speed`:
		speed, err := strconv.ParseFloat(value, 64)
		if err != nil || speed <= 0 {
			return fmt.Errorf(`marquee-speed 属性必须是正数：%s`, value)
		}
		t.marquee.speed = speed
		t.stopMarquee()
		t.document.RequestLayout()
		return nil
	case `marquee-pause`:
		milliseconds, err := strconv.Atoi(value)
		if err != nil || milliseconds < 0 {
			return fmt.Errorf(`marquee-pause 属性必须是非负整数毫秒：%s`, value)
		}
		t.marquee.pause = time.Duration(milliseconds) * time.Millisecond
		t.stopMarquee()
		t.document.RequestLayout()
		return nil
	case `marquee-count`:
		count, err := strconv.Atoi(value)
		if err != nil || count < 0 {
			return fmt.Errorf(`marquee-count 属性必须是非负整数：%s`, value)
		}
		t.marquee.count = count
		t.stopMarquee()
		t.resetMarqueePosition()
		t.document.RequestLayout()
		return nil
	default:
		return t.Base().SetProp(key, value)
	}
}

// 设置普通文本。
func (t *Text) SetText(text string) {
	t.textParts.children = nil
	t.children = nil
	// 替换整段内容时从头开始显示。普通的重新排版不应该重置这个
	// 偏移，否则无关的布局刷新也会把长文本滚回顶部。
	t.setDrawLineOffset(0)
	t.resetMarqueePosition()
	t.stopMarquee()
	t.AppendChild(text)
	t.expandTextNodes()
}

func (t *Text) SetTextFormat(format string, args ...any) {
	t.SetText(fmt.Sprintf(format, args...))
}

// SetRich 使用从 1 开始编号的 {$n} 位置参数设置富文本。模板只支持
// <b> 和 <i> 标签；参数始终作为普通文本转义，不能注入标签。同一个参数
// 可以重复使用，{{ 用于输出字面的 {。
//
// 替换是原子的：模板或富文本解析失败时，Text 的原内容保持不变。
func (t *Text) SetRich(tmpl string, args ...any) error {
	content, err := interpolateRichText(tmpl, args)
	if err != nil {
		return err
	}

	box, err := parseBox(t.document, strings.NewReader(`<text>`+content+`</text>`))
	if err != nil {
		return fmt.Errorf(`富文本解析失败：%w`, err)
	}
	parsed := box.(*Text)

	for _, child := range t.children {
		child.Base().parent = nil
		// child.Base().PrevSibling = nil
		// child.Base().NextSibling = nil
	}

	t.textParts = parsed.textParts
	t.children = parsed.children
	for _, child := range t.children {
		child.Base().parent = t.Base()
	}

	t.setDrawLineOffset(0)
	t.resetMarqueePosition()
	t.stopMarquee()
	t.expandTextNodes()
	if t.document != nil {
		// 文档样式在加载时已经校验；与 BaseBox.AppendChild 保持一致，
		// 运行时挂接新节点时重新应用样式并请求布局。
		_ = t.document.style(t, true)
		t.document.RequestLayout()
	}
	return nil
}

func interpolateRichText(tmpl string, args []any) (string, error) {
	var out strings.Builder
	used := make([]bool, len(args))

	for index := 0; index < len(tmpl); {
		if tmpl[index] != '{' {
			out.WriteByte(tmpl[index])
			index++
			continue
		}
		if index+1 < len(tmpl) && tmpl[index+1] == '{' {
			out.WriteByte('{')
			index += 2
			continue
		}
		if index+1 >= len(tmpl) || tmpl[index+1] != '$' {
			out.WriteByte('{')
			index++
			continue
		}

		end := index + 2
		digitsStart := end
		for end < len(tmpl) && '0' <= tmpl[end] && tmpl[end] <= '9' {
			end++
		}
		if digitsStart == end {
			return ``, fmt.Errorf(`富文本参数占位符缺少编号，位置 %d`, index)
		}
		if end >= len(tmpl) || tmpl[end] != '}' {
			return ``, fmt.Errorf(`富文本参数占位符未正确闭合，位置 %d`, index)
		}

		position, err := strconv.Atoi(tmpl[digitsStart:end])
		if err != nil {
			return ``, fmt.Errorf(`富文本参数编号无效，位置 %d：%w`, index, err)
		}
		if position == 0 {
			return ``, fmt.Errorf(`富文本参数编号必须从 1 开始，位置 %d`, index)
		}
		if position > len(args) {
			return ``, fmt.Errorf(`富文本参数 {$%d} 越界：仅传入 %d 个参数`, position, len(args))
		}

		appendEscapedRichText(&out, fmt.Sprint(args[position-1]))
		used[position-1] = true
		index = end + 1
	}

	for index, referenced := range used {
		if !referenced {
			return ``, fmt.Errorf(`富文本参数 {$%d} 未被使用`, index+1)
		}
	}
	return out.String(), nil
}

func appendEscapedRichText(out *strings.Builder, text string) {
	for i := range len(text) {
		switch text[i] {
		case '&':
			out.WriteString(`&amp;`)
		case '<':
			out.WriteString(`&lt;`)
		case '>':
			out.WriteString(`&gt;`)
		case '\'':
			out.WriteString(`&#39;`)
		case '"':
			out.WriteString(`&#34;`)
		default:
			out.WriteByte(text[i])
		}
	}
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
//
// TODO 没有缓存计算结果，应避免重复计算。
func (t *Text) SegmentBlock(availWidth, availHeight int) {
	t.segmentBlock(availWidth, availHeight, resolvedDimensions{t.computedStyles.Width, t.computedStyles.Height}, false, false)
}

// Flex 等父布局可指定文本的最终尺寸，而不修改文本样式。
func (t *Text) Calc(availWidth, availHeight int, constraints Constraints) {
	t.segmentBlock(availWidth, availHeight, t.resolveDimensions(constraints), constraints.UnboundedWidth, constraints.UnboundedHeight)
}

func (t *Text) segmentBlock(availWidth, availHeight int, size resolvedDimensions, unboundedWidth, unboundedHeight bool) {
	t.clearStates()

	// availHeight 应该内部没有使用，至少会使用一行行高。
	// availWidth 即使小于一个字符宽度（包括负数），SegmentInline 也会
	// 返回 false，避免在没有消费字符的情况下死循环。
	segmentWidth, widthStyle := availWidth, size.Width
	if t.marquee.axis == `horizontal` || (unboundedWidth && size.Width.Empty()) {
		// 水平滚动需要保留内容的固有宽度，不能按可视宽度自动折行。
		// fixed.Int26_6 的整数部分约有 25 位，保留一位余量避免转换溢出。
		segmentWidth, widthStyle = 1<<24, Length{}
	}
	for t.segmentInline(segmentWidth, availHeight, widthStyle) {
	}

	// 文本的宽度肯定是限制在可用宽度内的，目前超宽的始终折行。
	if w := size.Width; w.IsNumber() {
		t.layoutBox.Width = int(w.Number())
	} else if t.marquee.axis == `horizontal` {
		// 水平 marquee 的固有内容宽度用于计算滚动距离，盒子本身则
		// 使用父布局提供的可视宽度。列表项因此不必重复声明槽位宽度。
		t.layoutBox.Width = max(0, availWidth)
	} else {
		t.layoutBox.Width = t.textLineMaxWidth + t.HorizontalInsets()
	}

	// 但是高度就有可能超出盒子的高度了。
	if h := size.Height; h.IsNumber() {
		t.layoutBox.Height = int(h.Number())
	} else {
		// 文本高度随字体变化太麻烦，这里不应该简单取min值。取了min值后如果box高度不够，
		// 居中还是按小的居中，结果就是没有效果。如果按大的来，虽然会占用一点padding，但是至少是真的在中间。
		//
		// 对于单行文本来说，允许其大小超过 availHeight，这样其盒子的高度始终是
		// 自己的真实高度，竖直居中的时候才能正确计算中心点。
		//
		// 而如果是多行文本，虽然也能正确居中，但是……添加滚动也许是更好的做法？
		//
		// [TestSegmentBlockKeepsLineHeightWhenAvailableHeightIsSmaller]
		if len(t.textLines) <= 1 || unboundedHeight {
			t.layoutBox.Height = t.blockHeight() + t.VerticalInsets()
		} else {
			t.layoutBox.Height = min(t.blockHeight()+t.VerticalInsets(), availHeight)
		}
	}

	// 宽度或样式变化可能改变总行数。尽量保留原来的滚动位置，
	// 但不能让偏移落到新的文本末尾之外。
	t.clampDrawLineOffset()
	t.updateMarquee()
}

func (t *Text) stopMarquee() {
	if t.marquee.cancel != nil {
		t.marquee.cancel()
		t.marquee.cancel = nil
	}
	t.marquee.last = time.Time{}
	t.marquee.pauseUntil = time.Time{}
}

func (t *Text) updateMarquee() {
	contentWidth := t.layoutBox.Width - t.HorizontalInsets()
	contentHeight := t.layoutBox.Height - t.VerticalInsets()
	overflows := t.marquee.axis == `horizontal` && t.textLineMaxWidth > contentWidth ||
		t.marquee.axis == `vertical` && t.blockHeight() > contentHeight
	exhausted := t.marquee.count > 0 && t.marquee.completed >= t.marquee.count
	if !t.marquee.running || exhausted || t.marquee.axis == `` || !overflows || t.document == nil || t.document.app == nil {
		t.stopMarquee()
		if !overflows {
			t.resetMarqueePosition()
		}
		return
	}
	if t.marquee.cancel != nil {
		return
	}
	var frame func(time.Time)
	frame = func(now time.Time) {
		t.marquee.cancel = nil
		if t.marquee.last.IsZero() {
			t.marquee.pauseUntil = now.Add(t.marquee.pause)
		} else if !now.Before(t.marquee.pauseUntil) {
			currentContentWidth := t.layoutBox.Width - t.HorizontalInsets()
			currentContentHeight := t.layoutBox.Height - t.VerticalInsets()
			maxOffset := float64(t.blockHeight() - currentContentHeight)
			if t.marquee.axis == `horizontal` {
				maxOffset = float64(t.textLineMaxWidth - currentContentWidth)
			}
			t.marquee.offset += t.marquee.direction * t.marquee.speed * now.Sub(t.marquee.last).Seconds()
			if t.marquee.offset >= maxOffset {
				t.marquee.offset = maxOffset
				t.marquee.direction = -1
				t.marquee.pauseUntil = now.Add(t.marquee.pause)
			} else if t.marquee.offset <= 0 {
				t.marquee.offset = 0
				t.marquee.direction = 1
				t.marquee.completed++
				t.marquee.pauseUntil = now.Add(t.marquee.pause)
			}
			t.document.RequestPaint()
		}
		t.marquee.last = now
		if t.marquee.count > 0 && t.marquee.completed >= t.marquee.count {
			t.stopMarquee()
			return
		}
		t.marquee.cancel = t.document.RequestAnimationFrame(frame)
	}
	t.marquee.cancel = t.document.RequestAnimationFrame(frame)
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
//
// TODO 没有缓存计算结果，应避免重复计算。
func (t *Text) SegmentInline(availWidth, availHeight int) bool {
	return t.segmentInline(availWidth, availHeight, t.computedStyles.Width)
}

func (t *Text) segmentInline(availWidth, availHeight int, widthStyle Length) bool {
	line := _TextLine{}
	cannotFitFirstCharacter := false

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := Iif(widthStyle.IsNumber(), int(widthStyle.Number()), availWidth)
	// boxMaxHeight := Iif(computed.Height.IsNumber(), computed.Height.Number(), availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - t.HorizontalInsets()
	// contentAvailHeight := boxMaxHeight - t.verticalInsets()

	// 当前行已经使用的宽度
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

		var (
			currentRun  = &t.textRuns[t.textRunIndex]
			widthRemain = contentAvailWidth - width

			// 虽然确实支持多个字体fallback，但是行高却始终使用第一个字体的高度。
			faces = t.document.LoadFaces(currentRun.Owner)
		)

		// 如果有换行符，需要提前结束。
		newLinePos := strings.IndexByte(currentRun.Data[t.textRunDataIndex:], '\n')
		if newLinePos == 0 {
			// 换行符不保存，直接丢弃。
			t.textRunDataIndex++
			// 如果是空行，则可能没有行高。
			line.MaxHeight = max(line.MaxHeight, faces[0].TextHeight())
			break
		}

		// 有换行符且不在行首，处理最多到换行符前的内容。
		maxDataIndex := t.textRunDataIndex + len(currentRun.Data[t.textRunDataIndex:])
		if newLinePos != -1 {
			maxDataIndex = t.textRunDataIndex + newLinePos
		}

		end, runWidth, err := SegmentText(currentRun.Data[t.textRunDataIndex:maxDataIndex], widthRemain, faces)
		if err != nil {
			// 好像啥也干不了
			return false
		}

		// 当前行放不下更多字符。如果空行连第一个字符也放不下，则停止
		// 后续分段；不能返回“还有内容”，否则 SegmentBlock 会在没有推进
		// textRunDataIndex 的情况下反复调用并进入死循环。
		if end == 0 {
			// TODO 有可能下一个就是换行符，如果不处理，可能导致下次分行的时候产生一个意外的空行。
			cannotFitFirstCharacter = len(line.Fragments) == 0
			break
		}

		// 成功塞了一点东西
		line.Fragments = append(line.Fragments, _TextRunFragment{
			Run:       currentRun,
			Start:     t.textRunDataIndex,
			End:       t.textRunDataIndex + end,
			layoutBox: Rect{Width: runWidth, Height: faces[0].TextHeight()},
		})

		// 每次的字体可能不同，需要持续更新行高。
		line.MaxHeight = max(line.MaxHeight, faces[0].TextHeight())

		// 更新到索引，下次循环会自动切换到一下，如果有必要。
		t.textRunDataIndex += end

		width += runWidth
	}

	t.textLines = append(t.textLines, line)
	t.textLineMaxWidth = max(t.textLineMaxWidth, width)

	t.layoutBox.Width = width + t.HorizontalInsets()

	// 如果是空内容，行高也不应该为零。
	// 假定为当前字体的行高。
	if line.MaxHeight == 0 {
		line.MaxHeight = t.document.LoadFaces(t)[0].TextHeight()
	}
	// 此处的高度是单行的文本高度+非可用区的高度。
	// 如果是多行文本，此高度需要去重计算。
	t.layoutBox.Height = line.MaxHeight + t.VerticalInsets()

	return !cannotFitFirstCharacter && (t.textRunIndex < len(t.textRuns)-1 ||
		t.textRunIndex == len(t.textRuns)-1 && t.textRunDataIndex < len(t.textRuns[t.textRunIndex].Data))
}

// 清空分行的内部状态。滚动偏移是绘制状态，不属于排版中间状态，
// 因此重新排版时应当保留。
func (t *Text) clearStates() {
	t.textRunIndex = 0
	t.textRunDataIndex = 0
	t.textLines = nil
	t.textLineMaxWidth = 0
}

func (t *Text) clampDrawLineOffset() {
	maxOffset := max(0, len(t.textLines)-1)
	offset := min(max(0, t.textDrawLineOffset), maxOffset)
	t.setDrawLineOffset(offset)
}

func (t *Text) blockHeight() int {
	h := 0
	for _, l := range t.textLines {
		h += l.MaxHeight
	}
	return h
}

func (t *Text) Draw(canvas *Canvas) {
	t.Base().DrawOptions(canvas, BaseBoxDrawOptions{
		NoChildren: true,
	})

	if len(t.textLines) <= 0 {
		return
	}

	contentMaxHeight := t.layoutBox.Height - t.VerticalInsets()

	// 文本盒子总体的宽度。用于计算每行的水平居中位置。
	contentWidth := t.layoutBox.Width - t.HorizontalInsets()

	verticalMarquee := t.marquee.axis == `vertical` && t.blockHeight() > contentMaxHeight
	horizontalMarquee := t.marquee.axis == `horizontal` && t.textLineMaxWidth > contentWidth
	if verticalMarquee || horizontalMarquee {
		// 连续滚动会让首尾行暂时跨越边界，因此只允许内容区域内的
		// 像素落到 framebuffer。
		clipped := canvas.Clip(t.InsetLeft(), t.InsetTop(), contentWidth, contentMaxHeight)
		startY := t.InsetTop()
		if verticalMarquee {
			startY -= int(t.marquee.offset)
		} else {
			clipped = clipped.Offset(-int(t.marquee.offset), 0)
		}
		t.drawTextLines(clipped, startY, contentWidth)
		return
	}

	drawOffsetY := t.InsetTop()

	for lineNo, line := range t.textLines {
		if lineNo < t.textDrawLineOffset {
			continue
		}
		// 多行文本只绘制能完整放进内容区域的行，避免底部出现半行。
		// 单行文本即使因高度或 padding 配置有误而放不下，也仍然绘制；
		// 相比内容略微溢出，整段文字变成空白更难发现和理解。
		usedHeight := drawOffsetY - t.InsetTop()
		if !shouldDrawTextLine(len(t.textLines), usedHeight, line.MaxHeight, contentMaxHeight) {
			break
		}

		// 如果是水平居中。
		drawOffsetX := t.InsetLeft() + line.horizontalOffset(contentWidth, t.computedStyles.Align)

		for _, fragment := range line.Fragments {
			rc := fragment.layoutBox
			owner := fragment.Run.Owner
			canvas := canvas.Offset(drawOffsetX, drawOffsetY)

			if cr := owner.Base().computedStyles.BackgroundColor; owner.Base().computedStyles.has(propertyBackgroundColor) && !cr.IsNone() {
				canvas.FillRect(0, 0, rc.Width, rc.Height, cr)
			}

			text := fragment.Run.Data[fragment.Start:fragment.End]
			canvas.DrawString(text,
				t.document.LoadFaces(owner),
				owner.Base().computedStyles.Color,
			)

			drawOffsetX += rc.Width
		}

		drawOffsetY += line.MaxHeight
	}
}

func (t *Text) drawTextLines(canvas *Canvas, drawOffsetY, contentWidth int) {
	for _, line := range t.textLines {
		drawOffsetX := t.InsetLeft() + line.horizontalOffset(contentWidth, t.computedStyles.Align)
		for _, fragment := range line.Fragments {
			rc := fragment.layoutBox
			owner := fragment.Run.Owner
			fragmentCanvas := canvas.Offset(drawOffsetX, drawOffsetY)

			if cr := owner.Base().computedStyles.BackgroundColor; owner.Base().computedStyles.has(propertyBackgroundColor) && !cr.IsNone() {
				fragmentCanvas.FillRect(0, 0, rc.Width, rc.Height, cr)
			}
			fragmentCanvas.DrawString(
				fragment.Run.Data[fragment.Start:fragment.End],
				t.document.LoadFaces(owner),
				owner.Base().computedStyles.Color,
			)
			drawOffsetX += rc.Width
		}
		drawOffsetY += line.MaxHeight
	}
}

func shouldDrawTextLine(lineCount, usedHeight, lineHeight, contentMaxHeight int) bool {
	return lineCount == 1 || usedHeight+lineHeight <= contentMaxHeight
}

// 向下（内容向上）滚动一行。
func (t *Text) ScrollLineDown() bool {
	if t.textDrawLineOffset >= len(t.textLines)-1 {
		return false
	}

	t.textDrawLineOffset++
	t.document.RequestPaint()
	return true
}

// 向上（内容向下）滚动一行。
func (t *Text) ScrollLineUp() bool {
	if t.textDrawLineOffset <= 0 {
		return false
	}

	t.textDrawLineOffset--
	t.document.RequestPaint()
	return true
}

// 向右翻一页。
func (t *Text) PageRight() bool {
	if t.textDrawLineOffset >= len(t.textLines)-1 {
		return false
	}

	contentHeight := t.layoutBox.Height - t.VerticalInsets()

	height := 0
	lineCount := 0

	// 计算当前这一页实际显示了多少完整行。
	for i := t.textDrawLineOffset; i < len(t.textLines); i++ {
		lineHeight := t.textLines[i].MaxHeight

		if height+lineHeight > contentHeight {
			break
		}

		height += lineHeight
		lineCount++
	}

	// 极端情况：连一行都放不进去，也至少向下走一行。
	if lineCount == 0 {
		lineCount = 1
	}

	newOffset := min(
		t.textDrawLineOffset+lineCount,
		len(t.textLines)-1,
	)

	if newOffset == t.textDrawLineOffset {
		return false
	}

	t.textDrawLineOffset = newOffset
	t.document.RequestPaint()

	return true
}

// 向左翻一页。
func (t *Text) PageLeft() bool {
	if t.textDrawLineOffset <= 0 {
		return false
	}

	contentHeight := t.layoutBox.Height - t.VerticalInsets()

	height := 0
	newOffset := t.textDrawLineOffset

	// 从当前第一行向前找，尽可能装满一整页。
	for newOffset > 0 {
		lineHeight := t.textLines[newOffset-1].MaxHeight

		if height+lineHeight > contentHeight {
			break
		}

		height += lineHeight
		newOffset--
	}

	// 极端情况下连一行都装不下，也至少向上走一行。
	if newOffset == t.textDrawLineOffset {
		newOffset--
	}

	t.textDrawLineOffset = newOffset
	t.document.RequestPaint()

	return true
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

// 缩放完成才是最终Draw的形态。
type _ImageLoadingStatus uint8

const (
	imageLoadStatusNone     _ImageLoadingStatus = iota // 还没开始
	imageLoadStatusDecoding                            // 解码中
	imageLoadStatusDecoded                             // 完成，并且加载成功
	imageLoadStatusScaling                             // 缩放中
	imageLoadStatusScaled                              // 完成，并且已缩放
	imageLoadStatusFailed                              // 完成，并且加载失败
)

type _FsysAndPath struct {
	fsys fs.FS
	path string
}

type Image struct {
	BaseBox

	// 默认情况下，src总是来源于文档关联的文件系统（doc.fsys），和资源文件是一起的。
	// SetPath 那边可能设置为带scheme的URL。
	//
	// TODO [关于 HTML 元素的 Attribute 的转义问题 - 陪她去流浪](https://blog.twofei.com/1056/)
	src _FsysAndPath

	// 如果在异步加载的过程中修改了src（比如虚拟滚动重新绑定的时候），
	// 则异步结果其实是不再有效的，应该作废。但是后面还可能会使用到，
	// 所以暂时不取消异步加载（也即不使用Context提前结束加载过程，让其继续缓存着），
	// 所以引入版本号，版本号变化意味着数据不再有效。
	loadVersion uint32

	status _ImageLoadingStatus

	// 异步加载成功后写在这里。
	// 如果是仅解析成功，只包含尺寸信息。
	decodedImage DecodedImage

	// GIF相关
	gif       *_AnimatedGIF
	gifFrame  int
	gifCancel func()

	// 如果失败？
	err     error
	tmpFile *_ImageTempFile

	// 旋转相关参数
	rotationOverflow bool
	rotation         float64
	rotationCancel   func()
	// 缩放相关参数
	scale float64
}

type _ImageTempFile struct {
	path    string
	cleanup runtime.Cleanup
}

func NewImage(doc *Document) *Image {
	return &Image{BaseBox: NewBaseBox(doc, `img`), scale: 1}
}

// SetRotation 设置图片绕中心顺时针旋转的角度。必须在 UI 主线程调用。
// 不改变布局；按最近一次 Rotate 的 Overflow 设置裁剪，默认为组件范围内。
func (b *Image) SetRotation(degrees float64) {
	if math.IsNaN(degrees) || math.IsInf(degrees, 0) {
		panic("SetRotation: 无效的角度。")
	}
	b.rotation = math.Mod(degrees, 360)
	b.document.RequestPaint()
}

// SetScale 设置绕图片中心的缩放倍数，必须是大于零的有限数值。
// 不改变布局和命中区域，缩放可与旋转叠加；遵守当前 Overflow 配置。
func (b *Image) SetScale(scale float64) {
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		panic("SetScale: 无效的倍数。")
	}
	b.scale = scale
	b.document.RequestPaint()
}

// RotationOptions 描述匀速中心旋转。
type RotationOptions struct {
	// 为每圈时长，必须大于零；
	Duration time.Duration

	// 圈数，零表示无限循环，不能为负。
	Iterations int

	// Reverse 为 true 时逆时针旋转，默认顺时针。
	Reverse bool

	// 允许图片画到组件范围外，默认 false。始终遵守父容器
	// 与屏幕裁剪，不改变布局或命中区域；停止或结束后保留此设置。
	Overflow bool
}

// Rotate 从当前角度开始旋转，替换该图片之前的旋转动画。
// 返回可重复调用的停止函数，停止后保持当前角度。
// 注册、停止和更新均在 UI 主线程执行。后台暂停回调但不暂停时间。
func (b *Image) Rotate(options RotationOptions) func() {
	if options.Duration <= 0 || options.Iterations < 0 {
		panic("Rotate: 无效的时长或圈数。")
	}
	if b.rotationCancel != nil {
		b.rotationCancel()
	}
	initial := b.rotation
	turn := 360.0
	if options.Reverse {
		turn = -turn
	}
	iterations := options.Iterations
	if iterations == 0 {
		iterations = -1
	}
	cancel := b.document.Animate(AnimationOptions{
		Duration:   options.Duration,
		Iterations: iterations,
		OnUpdate: func(progress float64) {
			if progress == 1 {
				b.SetRotation(initial)
				return
			}
			b.SetRotation(initial + turn*progress)
		},
		OnComplete: func() { b.rotationCancel = nil },
	})
	b.rotationOverflow = options.Overflow
	b.document.RequestPaint()
	b.rotationCancel = cancel
	return cancel
}

func (b *Image) SetProp(key string, val string) error {
	switch key {
	case `src`:
		b.setSrc(nil, val)
		return nil
	default:
		return b.BaseBox.SetProp(key, val)
	}
}

// 重新设置图片文件路径。
//
// fsys可以为空，此时表示使用文档关联的资源包文件系统。
//
// 也可以使用 os.DirFS 来表示操作系统的文件系统路径。
// TODO <img src="..."> 这里理论上不允许使用操作系统路径，但是目前没有阻止此类构造。
func (b *Image) SetPath(fsys fs.FS, path string) {
	b.setSrc(fsys, path)
}

// 直接设置文件系统路径。
func (b *Image) SetOSPath(path string) {
	dir, base := filepath.Split(path)
	b.SetPath(os.DirFS(cmp.Or(dir, `.`)), base)
}

func (b *Image) setSrc(fsys fs.FS, path string) {
	new := _FsysAndPath{fsys, path}
	if b.src == new {
		return
	}
	b.src = new

	b.stopGIF()
	b.gif = nil
	b.gifFrame = 0

	// 令所有为旧 src 启动的异步加载失效。不能只在回调里比较
	// src，因为 src 可能经历 A -> B -> A，此时第一次 A 的回调
	// 仍然已经过期。
	b.loadVersion++
	b.status = imageLoadStatusNone
	b.decodedImage = DecodedImage{}
	b.err = nil
	if old := b.tmpFile; old != nil {
		old.cleanup.Stop()
		os.Remove(old.path)
		b.tmpFile = nil
	}
	b.document.RequestLayout()
}

// 显示指定的内存已解码图片。
func (b *Image) SetImage(img image.Image) {
	go func() {
		fp, err := os.CreateTemp(``, `fbiw-img-*`)
		if err != nil {
			log.Println(err)
			return
		}
		defer fp.Close()
		if err := png.Encode(fp, img); err != nil {
			os.Remove(fp.Name())
			log.Println(err)
			return
		}
		path := fp.Name()
		b.document.Async(func() {
			dir, base := filepath.Split(path)
			if dir == `` {
				dir = `.`
			}
			b.SetPath(os.DirFS(dir), base)
			cleanup := runtime.AddCleanup(b, func(path string) {
				os.Remove(path)
			}, path)
			b.tmpFile = &_ImageTempFile{path: path, cleanup: cleanup}
		})
	}()
}

func (b *Image) setLoadedImage(result any, scaled bool) {
	switch value := result.(type) {
	case DecodedImage:
		b.gif = nil
		b.decodedImage = value
	case *_AnimatedGIF:
		b.gif = value
		b.decodedImage = value.frames[0]
	default:
		panic(fmt.Sprintf("unexpected image result: %T", result))
	}
	if scaled {
		b.startGIF()
	}
}

func (b *Image) Calc(availWidth, availHeight int, constraints Constraints) {
	size := b.resolveDimensions(constraints)
	// 图片内容的固有尺寸不能覆盖父布局分配的尺寸（包括显式的零）。
	defer func() {
		if constraints.FixedWidth.IsNumber() {
			b.layoutBox.Width = int(size.Width.Number())
		}
		if constraints.FixedHeight.IsNumber() {
			b.layoutBox.Height = int(size.Height.Number())
		}
	}()
	b.layoutBox.Width = Iif(constraints.PrefersMaxWidth && !constraints.UnboundedWidth, availWidth, 0)
	b.layoutBox.Height = Iif(constraints.PrefersMaxHeight && !constraints.UnboundedHeight, availHeight, 0)

	if !size.Width.Empty() && !size.Height.Empty() {
		b.layoutBox.Width = int(size.Width.Number())
		b.layoutBox.Height = int(size.Height.Number())
	}
	if constraints.FixedWidth.IsNumber() {
		b.layoutBox.Width = int(size.Width.Number())
	}
	if constraints.FixedHeight.IsNumber() {
		b.layoutBox.Height = int(size.Height.Number())
	}

	if b.src.path == `` {
		return
	}

	switch b.status {
	case imageLoadStatusNone:
		// 如果有缓存的大小信息，直接用。
		// 这里总是以零大小加载，不缩放，才能获取到原始大小信息。
		if result, err := b.document.loadImageSync(b.src.fsys, b.src.path, 0, 0, ImageDecodeOptions{TrimTransparentBorder: true}); err == nil {
			b.setLoadedImage(result, false)
			b.status = imageLoadStatusDecoded
			b.Calc(availWidth, availHeight, constraints)
			return
		}

		src, version := b.src, b.loadVersion
		b.status = imageLoadStatusDecoding
		b.document.loadImageAsync(b.src.fsys, b.src.path, 0, 0, ImageDecodeOptions{TrimTransparentBorder: true},
			func(result any, err error) {
				// src属于防御性校验，用来防止在包内直接修改却忘记同步递增版本号。
				// 理论上不应该判断（版本号变化src一定变化）。
				if b.loadVersion != version || b.src != src {
					return
				}
				if err != nil {
					b.err = err
					b.status = imageLoadStatusFailed
					b.document.RequestLayout()
					return
				}
				b.setLoadedImage(result, false)
				b.status = imageLoadStatusDecoded
				b.document.RequestLayout()
			},
		)
		return
	case imageLoadStatusDecoding:
		break
	case imageLoadStatusDecoded:
		// 图片数据加载成功，获得了真实尺寸，重新布局。
		fittingWidth, fittingHeight := 0, 0

		if b.layoutBox.Width == 0 || b.layoutBox.Height == 0 {
			fittingWidth = b.decodedImage.Width
			fittingHeight = b.decodedImage.Height
		} else {
			switch fill := b.computedStyles.Fill; fill {
			case FillStretch:
				fittingWidth = b.layoutBox.Width
				fittingHeight = b.layoutBox.Height
			case FillContain, FillCover:
				scaleX := float64(b.layoutBox.Width) / float64(b.decodedImage.Width)
				scaleY := float64(b.layoutBox.Height) / float64(b.decodedImage.Height)
				scale := Iif(fill == FillContain, min(scaleX, scaleY), max(scaleX, scaleY))
				if fill == FillCover {
					// cover 必须在两个方向上都覆盖容器，向上取整
					// 可避免浮点误差令某一边少一个像素。
					fittingWidth = int(math.Ceil(float64(b.decodedImage.Width) * scale))
					fittingHeight = int(math.Ceil(float64(b.decodedImage.Height) * scale))
				} else {
					fittingWidth = int(float64(b.decodedImage.Width) * scale)
					fittingHeight = int(float64(b.decodedImage.Height) * scale)
				}
			case FillNone:
				fittingWidth = b.decodedImage.Width
				fittingHeight = b.decodedImage.Height
			case FillScaleDown:
				scaleX := float64(b.layoutBox.Width) / float64(b.decodedImage.Width)
				scaleY := float64(b.layoutBox.Height) / float64(b.decodedImage.Height)
				scale := min(scaleX, scaleY)
				if scale < 1 {
					fittingWidth = int(float64(b.decodedImage.Width) * scale)
					fittingHeight = int(float64(b.decodedImage.Height) * scale)
				} else {
					fittingWidth = b.decodedImage.Width
					fittingHeight = b.decodedImage.Height
				}
			}
		}

		if fittingWidth == 0 || fittingHeight == 0 {
			b.status = imageLoadStatusFailed
			return
		}
		if result, err := b.document.loadImageSync(b.src.fsys, b.src.path, fittingWidth, fittingHeight, ImageDecodeOptions{TrimTransparentBorder: true}); err == nil {
			b.setLoadedImage(result, true)
			b.status = imageLoadStatusScaled
			b.Calc(availWidth, availHeight, constraints)
			return
		}

		src, version := b.src, b.loadVersion
		b.status = imageLoadStatusScaling
		b.document.loadImageAsync(src.fsys, src.path, fittingWidth, fittingHeight, ImageDecodeOptions{TrimTransparentBorder: true},
			func(result any, err error) {
				if b.loadVersion != version || b.src != src {
					return
				}
				if err != nil {
					b.status = imageLoadStatusFailed
					b.err = err
					b.document.RequestPaint()
					return
				}
				b.setLoadedImage(result, true)
				b.status = imageLoadStatusScaled
				b.document.RequestPaint()
			},
		)
		return
	case imageLoadStatusScaled:
		if b.layoutBox.Width == 0 {
			b.layoutBox.Width = b.decodedImage.Width
		}
		if b.layoutBox.Height == 0 {
			b.layoutBox.Height = b.decodedImage.Height
		}
	case imageLoadStatusFailed:
		return
	}
}

func (b *Image) Draw(canvas *Canvas) {
	b.Base().DrawOptions(canvas, BaseBoxDrawOptions{NoChildren: true})

	switch b.status {
	case imageLoadStatusScaled:
		if b.rotation != 0 || b.rotationOverflow || b.scale != 1 {
			b.drawImageRotated(canvas)
		} else {
			b.drawImageNormal(canvas)
		}
	case imageLoadStatusFailed:
		if b.err != nil {
			// 暂时！没有换行，没有border、padding……
			canvas.DrawString(b.err.Error(), b.document.LoadFaces(b), ColorFromRGBA(0xFF, 0, 0, 0xFF))
		}
	}
}

// TODO 没处理border和padding
// 图片的宽高不一定等于容器。contain 会在容器内居中；
// cover/none 可能超出容器，此时从图片中心裁出可见部分。
func (b *Image) drawImageNormal(canvas *Canvas) {
	imageWidth := b.decodedImage.Width
	imageHeight := b.decodedImage.Height
	visibleWidth := min(imageWidth, max(0, b.layoutBox.Width))
	visibleHeight := min(imageHeight, max(0, b.layoutBox.Height))
	if visibleWidth <= 0 || visibleHeight <= 0 {
		return
	}
	srcX := max(0, (imageWidth-b.layoutBox.Width)/2)
	srcY := max(0, (imageHeight-b.layoutBox.Height)/2)
	dstX := max(0, (b.layoutBox.Width-imageWidth)/2)
	dstY := max(0, (b.layoutBox.Height-imageHeight)/2)
	canvas.Offset(dstX, dstY).DrawImageRegion(
		b.decodedImage, srcX, srcY, visibleWidth, visibleHeight,
	)
}

func (b *Image) drawImageRotated(canvas *Canvas) {
	if b.layoutBox.Width <= 0 || b.layoutBox.Height <= 0 {
		return
	}
	clipped := canvas
	if !b.rotationOverflow {
		if canvas.clipBounds().Intersect(image.Rect(canvas.x, canvas.y, canvas.x+b.layoutBox.Width, canvas.y+b.layoutBox.Height)).Empty() {
			return
		}
		clipped = canvas.Clip(0, 0, b.layoutBox.Width, b.layoutBox.Height)
	}
	if b.rotation == 0 && b.scale == 1 {
		canvas.Offset((b.layoutBox.Width-b.decodedImage.Width)/2,
			(b.layoutBox.Height-b.decodedImage.Height)/2).DrawImage(b.decodedImage)
		return
	}
	clipped.drawImageTransformedCenter(b.decodedImage, b.rotation, b.scale,
		float64(canvas.x)+float64(b.layoutBox.Width)/2,
		float64(canvas.y)+float64(b.layoutBox.Height)/2,
	)
}

func (b *Image) startGIF() {
	b.stopGIF()
	if b.gif == nil || len(b.gif.frames) < 2 || b.document.app == nil {
		return
	}
	version := b.loadVersion
	var advance func()
	advance = func() {
		if b.loadVersion != version || b.gif == nil {
			return
		}
		b.gifFrame = (b.gifFrame + 1) % len(b.gif.frames)
		b.decodedImage = b.gif.frames[b.gifFrame]
		b.document.RequestPaint()
		b.gifCancel = b.document.SetTimeout(b.gif.delays[b.gifFrame], advance)
	}
	b.gifCancel = b.document.SetTimeout(b.gif.delays[b.gifFrame], advance)
}

func (b *Image) stopGIF() {
	if b.gifCancel != nil {
		b.gifCancel()
		b.gifCancel = nil
	}
}
