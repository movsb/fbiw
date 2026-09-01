package fbiw

import (
	"bufio"
	"errors"
	"fmt"
	"image/color"
	"io"
	"iter"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	_ "embed"
)

type styleProperty uint64

const (
	propertyAlign styleProperty = 1 << iota
	propertyBackgroundColor
	propertyBackgroundImage
	propertyBorderColor
	propertyBorderWidth
	propertyOutlineWidth
	propertyOutlineColor
	propertyColor
	propertyHeight
	propertyPadding
	propertyWidth
	propertyFontFamily
	propertyFontSize
	propertyFontBold
	propertyFontItalic
	propertySpacer
	propertyDisplay
	propertyFill
)

// 用于保存节点的样式值。
//
// 值可以直接读取，但是如果需要写入，应该调用应对的 Set*。
type Styles struct {
	bits uint64

	// 子元素的水平/垂直对齐方式。
	//   - 默认("")水平居左，“center”居中。
	//   - 默认("")垂直居顶，“middle”居中。
	//   - “both”两者均居中。
	Align string

	BackgroundColor Color
	BackgroundImage string
	BorderColor     Color
	BorderWidth     int
	OutlineWidth    int
	OutlineColor    Color
	Color           Color
	Height          Length
	Padding         Padding
	Width           Length

	FontFamily string // font-family
	FontSize   Length // font-size
	FontBold   bool   // bold
	FontItalic bool   // italic

	// 是否当作Spacer可变大小布局。
	Spacer bool

	// 显示方式。兼容 true/false，并支持 none/block/inline。
	// 此属性虽非继承属性，但是子盒子即便为true但父盒子为false时，
	// 此子盒子仍然不会被显示。所以不能通过判断子盒子的display是否
	// 为true来判断子盒子是否正处于显示状态。
	Display DisplayMode

	// 填充方式。
	Fill Fill
}

func (s *Styles) has(property styleProperty) bool {
	return s.bits&uint64(property) != 0
}

func (s *Styles) mark(property styleProperty) {
	s.bits |= uint64(property)
}

func (s *Styles) SetAlign(value string) { s.Align = value; s.mark(propertyAlign) }
func (s *Styles) SetBackgroundColor(value Color) {
	s.BackgroundColor = value
	s.mark(propertyBackgroundColor)
}
func (s *Styles) SetBackgroundImage(value string) {
	s.BackgroundImage = value
	s.mark(propertyBackgroundImage)
}
func (s *Styles) SetBorderColor(value Color)   { s.BorderColor = value; s.mark(propertyBorderColor) }
func (s *Styles) SetBorderWidth(value int)     { s.BorderWidth = value; s.mark(propertyBorderWidth) }
func (s *Styles) SetOutlineWidth(value int)    { s.OutlineWidth = value; s.mark(propertyOutlineWidth) }
func (s *Styles) SetOutlineColor(value Color)  { s.OutlineColor = value; s.mark(propertyOutlineColor) }
func (s *Styles) SetColor(value Color)         { s.Color = value; s.mark(propertyColor) }
func (s *Styles) SetHeight(value Length)       { s.Height = value; s.mark(propertyHeight) }
func (s *Styles) SetPadding(value Padding)     { s.Padding = value; s.mark(propertyPadding) }
func (s *Styles) SetWidth(value Length)        { s.Width = value; s.mark(propertyWidth) }
func (s *Styles) SetFontFamily(value string)   { s.FontFamily = value; s.mark(propertyFontFamily) }
func (s *Styles) SetFontSize(value Length)     { s.FontSize = value; s.mark(propertyFontSize) }
func (s *Styles) SetFontBold(value bool)       { s.FontBold = value; s.mark(propertyFontBold) }
func (s *Styles) SetFontItalic(value bool)     { s.FontItalic = value; s.mark(propertyFontItalic) }
func (s *Styles) SetSpacer(value bool)         { s.Spacer = value; s.mark(propertySpacer) }
func (s *Styles) SetDisplay(value DisplayMode) { s.Display = value; s.mark(propertyDisplay) }
func (s *Styles) SetFill(value Fill)           { s.Fill = value; s.mark(propertyFill) }

func stylePropertyByName(name string) styleProperty {
	switch name {
	case `align`, `Align`:
		return propertyAlign
	case `background-color`, `BackgroundColor`:
		return propertyBackgroundColor
	case `background-image`, `BackgroundImage`:
		return propertyBackgroundImage
	case `border-color`, `BorderColor`:
		return propertyBorderColor
	case `border-width`, `BorderWidth`:
		return propertyBorderWidth
	case `outline-width`, `OutlineWidth`:
		return propertyOutlineWidth
	case `outline-color`, `OutlineColor`:
		return propertyOutlineColor
	case `color`, `Color`:
		return propertyColor
	case `height`, `Height`:
		return propertyHeight
	case `padding`, `Padding`:
		return propertyPadding
	case `width`, `Width`:
		return propertyWidth
	case `font-family`, `FontFamily`:
		return propertyFontFamily
	case `font-size`, `FontSize`:
		return propertyFontSize
	case `bold`, `font-bold`, `FontBold`:
		return propertyFontBold
	case `italic`, `font-italic`, `FontItalic`:
		return propertyFontItalic
	case `spacer`, `Spacer`:
		return propertySpacer
	case `display`, `Display`:
		return propertyDisplay
	case `fill`, `Fill`:
		return propertyFill
	default:
		return 0
	}
}

// 可替换对象内容的填充方式。
// [object-fit CSS property - CSS | MDN](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/object-fit)
type Fill uint8

const (
	// The replaced content is sized to fill the element's content box. This is the initial value.
	// The entire object will completely fill the box. If the object's aspect ratio does not
	// match the aspect ratio of its box, then the object will be stretched to fit.
	// 会使图片变形。
	FillStretch Fill = iota // 拉伸，默认
	// The replaced content is scaled to maintain its aspect ratio while fitting within the element's content box.
	// The entire object is made to fill the box, while preserving its aspect ratio,
	// so the object will be "letter-boxed" or "pillar-boxed" if its aspect ratio does not match the aspect ratio of the box.
	// 在容器内完整显示。大的会缩小、小的会放大。
	FillContain
	// The replaced content is sized to maintain its aspect ratio while filling the element's entire content box.
	// If the object's aspect ratio does not match the aspect ratio of its box, then the object will be clipped to fit.
	// 放大图片，会容器被填满，但是图片会被裁剪。
	FillCover
	// The replaced content is not resized.
	// 保持图片原始大小。大图会超出范围。
	FillNone
	// The content is sized as if none or contain were specified, whichever would result in a smaller concrete object size.
	// 优先保持图片原始大小，但是如果大小超过容器，会缩小到容器大小。
	FillScaleDown
)

type DisplayMode uint8

const (
	DisplayUnset DisplayMode = iota
	DisplayVisible
	DisplayNone
	DisplayBlock
	DisplayInline
)

func (d DisplayMode) Visible() bool {
	return d != DisplayNone
}

//go:embed assets/defaults.css
var _defaultsStyle string

var DefaultStyles = Must1((StyleParser{}).ParseStyle(_defaultsStyle))

// 直接传入的是结构体字段，原始名字，没有小写、没有中划线。
func shouldInherit(name string) bool {
	switch name {
	case `Color`:
		return true
	case `FontFamily`, `FontSize`, `FontBold`, `FontItalic`:
		return true
	default:
		return false
	}
}

var ErrUnknownStyleProperty = errors.New(`未知样式属性`)

// 设置样式。
//
//   - 影响继承会导致重新计算自己以及所有后代的样式。
//   - 影响布局会导致整个文档重新布局（并重绘）。
//   - 影响绘制导致整个文档重绘（但不一定重新布局）。
//
// TODO 值未变是否可以affect*=false？
// 比如 display，这个外面设置得比较多。
func (s *Styles) Set(name string, raw string) (affectInherit, affectLayout, affectPaint bool, outErr error) {
	var current, update any

	affectInherit, affectLayout, affectPaint, current, update, outErr = s.parseProperty(name, raw)
	if outErr == nil {
		assignStyleProperty(current, update)
		s.mark(stylePropertyByName(name))
	}

	return
}

func assignStyleProperty(current, update any) {
	reflect.ValueOf(current).Elem().Set(reflect.ValueOf(update))
}

var namedFontSizeScale = map[string]int{
	`xx-small`: 45,
	`x-small`:  60,
	`small`:    85,
	`medium`:   100,
	`large`:    115, // h3
	`x-large`:  130, // h2
	`xx-large`: 145, // h1
}

// 后面计算样式覆盖的时候会有优先级的覆盖考虑，所以不能直接覆盖。
func (s *Styles) parseProperty(name string, raw string) (
	affectInherit, affectLayout, affectPaint bool,
	current, update any,
	outErr error,
) {
	parseNumberOrPercentage := func(raw string) (Length, error) {
		if before, ok := strings.CutSuffix(raw, `%`); ok {
			n, err := strconv.Atoi(before)
			return PercentageLength(n), err
		} else {
			n, err := strconv.Atoi(before)
			return NumberLength(n), err
		}
	}
	parseFontSize := func(raw string) (Length, error) {
		if before, ok := strings.CutSuffix(raw, `rem`); ok {
			n, err := strconv.ParseFloat(before, 64)
			if err != nil {
				return Length{}, err
			}
			if n < 0 {
				return Length{}, fmt.Errorf(`字号不能为负数：%s`, raw)
			}
			return RemLength(n), nil
		}
		if n, ok := namedFontSizeScale[raw]; ok {
			return PercentageLength(n), nil
		}
		return parseNumberOrPercentage(raw)
	}
	parseNumber := func(raw string) (int, error) {
		n, err := strconv.Atoi(raw)
		return n, err
	}
	parsePadding := func(raw string) (Padding, error) {
		const maxPadding = int(^uint16(0))
		parts := strings.Fields(raw)
		if len(parts) < 1 || len(parts) > 4 {
			return 0, fmt.Errorf(`padding 需要 1 到 4 个值：%s`, raw)
		}
		values := make([]int, len(parts))
		for i, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil {
				return 0, err
			}
			if n < 0 || n > maxPadding {
				return 0, fmt.Errorf(`padding 必须在 0 到 %d 之间：%s`, maxPadding, raw)
			}
			values[i] = n
		}
		switch len(values) {
		case 1:
			return PaddingValue(values[0], values[0], values[0], values[0]), nil
		case 2:
			return PaddingValue(values[0], values[1], values[0], values[1]), nil
		case 3:
			return PaddingValue(values[0], values[1], values[2], values[1]), nil
		case 4:
			return PaddingValue(values[0], values[1], values[2], values[3]), nil
		}
		panic(`unreachable`)
	}
	parseColor := func(raw string) (Color, error) {
		return ParseColor(raw)
	}
	parseBoolean := func(raw string, emptyIsTrue bool) (bool, error) {
		switch raw {
		case `1`, `true`:
			return true, nil
		case `0`, `false`:
			return false, nil
		case ``:
			return emptyIsTrue, nil
		default:
			return false, fmt.Errorf(`未知布尔值：%v`, raw)
		}
	}
	switch name {
	default:
		outErr = ErrUnknownStyleProperty
		return
	case `align`:
		if raw == `` || raw == `center` || raw == `middle` || raw == `both` {
			current = &s.Align
			update = raw
			affectLayout = true
			return
		}
		outErr = fmt.Errorf(`不认识的对齐方式：%s`, raw)
		return
	case `background-color`:
		affectPaint = true
		current = &s.BackgroundColor
		update, outErr = parseColor(raw)
		return
	case `background-image`:
		current = &s.BackgroundImage
		update = raw
		affectPaint = true
		return
	case `border-color`:
		affectPaint = true
		current = &s.BorderColor
		update, outErr = parseColor(raw)
		return
	case `border-width`:
		affectLayout = true
		current = &s.BorderWidth
		update, outErr = parseNumber(raw)
		return
	case `outline-color`:
		affectPaint = true
		current = &s.OutlineColor
		update, outErr = parseColor(raw)
		return
	case `outline-width`:
		// outline不会影响布局。
		// affectLayout = true
		current = &s.OutlineWidth
		update, outErr = parseNumber(raw)
		return
	case `color`:
		affectInherit = true
		affectPaint = true
		current = &s.Color
		update, outErr = parseColor(raw)
		return
	case `height`:
		affectLayout = true
		current = &s.Height
		update, outErr = parseNumberOrPercentage(raw)
		return
	case `padding`:
		affectLayout = true
		current = &s.Padding
		update, outErr = parsePadding(raw)
		return
	case `width`:
		affectLayout = true
		current = &s.Width
		update, outErr = parseNumberOrPercentage(raw)
		return
	case `font-family`:
		// 不同字体大小不一样，所以也会影响布局
		affectInherit = true
		affectLayout = true
		current = &s.FontFamily
		update = raw
		return
	case `font-size`:
		affectInherit = true
		affectLayout = true
		current = &s.FontSize
		update, outErr = parseFontSize(raw)
		return
	case `bold`, `font-bold`:
		affectInherit = true
		affectLayout = true
		current = &s.FontBold
		update, outErr = parseBoolean(raw, true)
		return
	case `italic`, `font-italic`:
		affectInherit = true
		affectLayout = true
		current = &s.FontItalic
		update, outErr = parseBoolean(raw, true)
		return
	case `spacer`:
		affectLayout = true
		current = &s.Spacer
		update, outErr = parseBoolean(raw, true)
		return
	case `display`:
		affectLayout = true
		current = &s.Display
		switch raw {
		case ``, `1`, `true`:
			update = DisplayVisible
		case `0`, `false`, `none`:
			update = DisplayNone
		case `block`:
			update = DisplayBlock
		case `inline`:
			update = DisplayInline
		default:
			outErr = fmt.Errorf(`不认识的显示方式：%s`, raw)
		}
		return
	case `fill`:
		affectLayout = true
		switch raw {
		case ``, `stretch`:
			update = FillStretch
		case `none`:
			update = FillNone
		case `contain`:
			update = FillContain
		case `cover`:
			update = FillCover
		case `scale-down`:
			update = FillScaleDown
		default:
			outErr = fmt.Errorf(`不认识的填充方式: %s`, raw)
			return
		}
		affectLayout = true
		current = &s.Fill
		return
	}
}

type LengthKind uint8

const (
	LengthNone LengthKind = iota
	LengthNumber
	LengthPercentage
	LengthRem
)

// Length 表示样式中的绝对数值、百分比或 rem。是否显式声明仍由
// Styles 的属性位图记录；LengthNone 让脱离 Styles 使用的零值保持安全。
type Length struct {
	number int64
	kind   LengthKind
}

func NumberLength[T ~int | ~int64](v T) Length {
	return Length{number: int64(v), kind: LengthNumber}
}

func PercentageLength[T ~int | ~int64](v T) Length {
	return Length{number: int64(v), kind: LengthPercentage}
}

const remScale = 1000

// RemLength 使用千分之一 rem 保存小数，避免样式计算引入浮点误差。
func RemLength(v float64) Length {
	return Length{number: int64(math.Round(v * remScale)), kind: LengthRem}
}

func (l Length) Empty() bool        { return l.kind == LengthNone }
func (l Length) IsNumber() bool     { return l.kind == LengthNumber }
func (l Length) IsPercentage() bool { return l.kind == LengthPercentage }
func (l Length) IsRem() bool        { return l.kind == LengthRem }
func (l Length) Number() int64      { return l.number }

type Padding uint64

func PaddingValue(top, right, bottom, left int) Padding {
	packed := uint64(top)<<48 |
		uint64(right)<<32 |
		uint64(bottom)<<16 |
		uint64(left)
	return Padding(packed)
}

const paddingMask = uint64(0xffff)

func (p Padding) PaddingTop() int {
	return int(uint64(p) >> 48 & paddingMask)
}

func (p Padding) PaddingRight() int {
	return int(uint64(p) >> 32 & paddingMask)
}

func (p Padding) PaddingBottom() int {
	return int(uint64(p) >> 16 & paddingMask)
}

func (p Padding) PaddingLeft() int {
	return int(uint64(p) & paddingMask)
}

// 0xAA_RR_GG_BB
// 低32位与设备的像素格式匹配（低端序）
//
// 颜色包含特殊值，使用前应判断 IsNone，IsClear。
type Color uint32

// 特殊值的AA始终为零，所以是安全的。
const (
	// 特殊值：判断是否为空色。
	//
	// 如果父元素设备了背景，子元素不想要。
	// 这时候如果什么也不写，会导致继承。
	// 所以只能写个none。
	ColorNone Color = iota + 1

	// 特殊的打洞色。
	// 使用此色后，此块屏幕区域会直接清空成透明色。
	//
	// 此值的特殊背景：游戏机的GPU可以在UI层下面叠加一层
	// 视频层，由于在UI层下面，这就要求UI层透明。最简单的办法是
	// 直接清空需要的区域，而不是隐藏下面的所以文档/控件层，太麻烦了。
	ColorClear
)

func ColorFromRGBA(r, g, b, a uint8) Color {
	if a == 0 {
		return 0
	}
	out := uint32(0)
	out |= uint32(b) << 0
	out |= uint32(g) << 8
	out |= uint32(r) << 16
	out |= uint32(a) << 24
	return Color(out)
}

func (c Color) IsNone() bool {
	return c == ColorNone
}
func (c Color) IsClear() bool {
	return c == ColorClear
}
func (c Color) R() uint8 {
	return uint8(c >> 16)
}
func (c Color) G() uint8 {
	return uint8(c >> 8)
}
func (c Color) B() uint8 {
	return uint8(c >> 0)
}
func (c Color) A() uint8 {
	return uint8(c >> 24)
}
func (c Color) NRGBA() color.NRGBA {
	return color.NRGBA{
		R: c.R(),
		G: c.G(),
		B: c.B(),
		A: c.A(),
	}
}
func (c Color) Value() uint32 {
	return uint32(c)
}

// 用结构体而不是直接type为[]string的原因是修改的时候不想重新赋值。
type Class struct {
	class []string
}

func ParseClass(raw string) Class {
	return Class{class: strings.Fields(raw)}
}
func (c *Class) Set(class string) {
	c.class = strings.Fields(class)
}
func (c Class) Contains(class string) bool {
	return slices.Contains(c.class, class)
}
func (c Class) ContainsAll(class ...string) bool {
	for _, class := range class {
		if !c.Contains(class) {
			return false
		}
	}
	return true
}
func (c *Class) Add(class string) {
	if !c.Contains(class) {
		c.class = append(c.class, class)
	}
}
func (c *Class) Remove(class string) {
	c.class = slices.DeleteFunc(c.class, func(each string) bool { return each == class })
}
func (c *Class) Toggle(class string, force ...any) {
	var add bool
	if len(force) > 0 {
		add = force[0].(bool)
	} else {
		add = !c.Contains(class)
	}
	if add {
		c.Add(class)
	} else {
		c.Remove(class)
	}
}

// 表示匹配到的规则。
type RuleMatch struct {
	// 合并后的相关性。
	Specificity  uint32
	Declarations []Declaration
}

func ParseColor(c string) (_ Color, outErr error) {
	if len(c) == 0 {
		return 0, nil
	}

	switch c {
	case `none`:
		return ColorNone, nil
	case `clear`:
		return ColorClear, nil
	}

	defer func() {
		if e := recover(); e != nil {
			outErr = fmt.Errorf(`%v`, e)
		}
	}()

	cr := color.NRGBA{}

	if c[0] == '#' {
		index := -1
		decode := func() uint8 {
			index++
			switch b := c[1+index]; {
			case '0' <= b && b <= '9':
				return b - '0'
			case 'a' <= b && b <= 'f':
				return b - 'a' + 10
			case 'A' <= b && b <= 'F':
				return b - 'A' + 10
			default:
				panic(`无效颜色值`)
			}
		}
		h := c[1:]
		switch len(h) {
		case 3:
			r, g, b := decode(), decode(), decode()
			r |= r << 4
			g |= g << 4
			b |= b << 4
			cr = color.NRGBA{r, g, b, 0xFF}
		case 4:
			r, g, b, a := decode(), decode(), decode(), decode()
			r |= r << 4
			g |= g << 4
			b |= b << 4
			a |= a << 4
			cr = color.NRGBA{r, g, b, a}
		case 6:
			r := decode()<<4 | decode()
			g := decode()<<4 | decode()
			b := decode()<<4 | decode()
			cr = color.NRGBA{r, g, b, 0xFF}
		case 8:
			r := decode()<<4 | decode()
			g := decode()<<4 | decode()
			b := decode()<<4 | decode()
			a := decode()<<4 | decode()
			cr = color.NRGBA{r, g, b, a}
		default:
			panic(`无效颜色值`)
		}
	} else if c, ok := presetColors[string(c)]; ok {
		cr = color.NRGBA{
			R: uint8(c >> 16),
			G: uint8(c >> 8),
			B: uint8(c >> 0),
			A: uint8(c >> 24),
		}
	} else {
		panic(`未知颜色`)
	}

	return ColorFromRGBA(cr.R, cr.G, cr.B, cr.A), nil
}

// ColorFromString 在解析失败时崩溃。
func ColorFromString(raw string) Color {
	return Must1(ParseColor(raw))
}

var presetColors = map[string]uint32{
	`coral`:          0xFFF08080,
	`salmon`:         0xFFE9967A,
	`red`:            0xFFFF325B,
	`hotpink`:        0xFFFF69B4,
	`deeppink`:       0xFFFF1493,
	`palevioletred`:  0xFFDB7093,
	`tomato`:         0xFFFF6347,
	`darkorange`:     0xFFFF8C00,
	`orange`:         0xFFFFA500,
	`yellow`:         0xFFFFD800,
	`darkkhaki`:      0xFFBDB76B,
	`magenta`:        0xFFDA70D6,
	`purple`:         0xFF9932CC,
	`slateblue`:      0xFF6A5ACD,
	`mediumseagreen`: 0xFF3CB371,
	`green`:          0xFF17A817,
	`yellowgreen`:    0xFF9ACD32,
	`olive`:          0xFF6B8E23,
	`darkseagreen`:   0xFF8FBC8B,
	`lightseagreen`:  0xFF20B2AA,
	`teal`:           0xFF008080,
	`cyan`:           0xFF00CED1,
	`aqua`:           0xFF00CED1,
	`cadetblue`:      0xFF5F9EA0,
	`steelblue`:      0xFF4682B4,
	`deepskyblue`:    0xFF00BFFF,
	`blue`:           0xFF1E90FF,
	`burlywood`:      0xFFDEB887,
	`tan`:            0xFFD2B48C,
	`rosybrown`:      0xFFBC8F8F,
	`sandybrown`:     0xFFF4A460,
	`goldenrod`:      0xFFDAA520,
	`darkgoldenrod`:  0xFFB8860B,
	`peru`:           0xFFCD853F,
	`chocolate`:      0xFFD2691E,
	`white`:          0xFFFFFFFF,
	`silver`:         0xFFC0C0C0,
	`darkgray`:       0xFFA9A9A9,
	`gray`:           0xFF808080,
	`slategray`:      0xFF708090,
	`black`:          0xFF000000,
}

func ParseFontFamily(s string) (out []string) {
	for name := range strings.SplitSeq(s, `,`) {
		name = strings.TrimSpace(name)
		if len(name) > 2 {
			if r := name[0]; r == '"' || r == '\'' {
				name = name[1:]
			}
			if r := name[len(name)-1]; r == '"' || r == '\'' {
				name = name[:len(name)-1]
			}
		}
		out = append(out, name)
	}
	return
}

type Rule struct {
	// 只表示单条选择器（类似逗号分隔的会被拆成多条）
	Selector     Selector
	Declarations []Declaration
}

type Selector = []NodeSelector

type _Combinator uint8

const (
	// block > inline
	childCombinator _Combinator = iota + 1
)

type NodeSelector struct {
	Tag   string
	Class []string
	ID    string

	// match all
	Asterisk bool
	// match child/sibling...
	Combinator _Combinator

	// 可直接比较大小，但不等于真实的css相关性，需要转换。
	// 8 + 8 + 8   +  8
	// 0   id  class  tag
	Specificity uint32
}

type Declaration struct {
	Name  string
	Value string
}

type Sheet struct {
	Rules []Rule
}

// StyleParser 解析项目支持的 CSS 子集。
type StyleParser struct{}

// ParseStyle 是为现有调用方保留的兼容入口。
func ParseStyle(data string) (_ *Sheet, outErr error) {
	return (StyleParser{}).ParseStyle(data)
}

// ParseStyle 解析样式表，并将嵌套规则展开为扁平规则。
func (p StyleParser) ParseStyle(data string) (_ *Sheet, outErr error) {
	buf := &BufioReader{
		Reader: bufio.NewReaderSize(strings.NewReader(data), max(4096, len(data)+1)),
	}

	defer func() {
		if e := recover(); e != nil {
			outErr = fmt.Errorf(`%v`, e)
		}
	}()

	ss := Sheet{}
	for {
		buf.skipSpaces()
		if buf.peekByte() == 0 {
			break
		}
		ss.Rules = append(ss.Rules, p.parseRule(buf, nil)...)
	}

	return &ss, nil
}

func (p StyleParser) parseRule(buf *BufioReader, parents []Selector) []Rule {
	header := strings.TrimSpace(buf.readUntil('{'))
	if header == `` {
		panic(`没有选择器。`)
	}
	if buf.peekByte() != '{' {
		panic(`缺少 {`)
	}
	buf.Discard(1)

	selectors := p.expandSelectors(parents, header)
	rules := []Rule{}
	declarations := []Declaration{}
	hadContent := false
	flushDeclarations := func() {
		if len(declarations) == 0 {
			return
		}
		for _, selector := range selectors {
			rules = append(rules, Rule{Selector: selector, Declarations: declarations})
		}
		declarations = nil
	}

	for {
		buf.skipSpaces()
		switch buf.peekByte() {
		case 0:
			panic(`缺少 }`)
		case '}':
			flushDeclarations()
			buf.Discard(1)
			if !hadContent {
				for _, selector := range selectors {
					rules = append(rules, Rule{Selector: selector})
				}
			}
			return rules
		}

		hadContent = true
		switch buf.nextDelimiter() {
		case ':':
			declarations = append(declarations, p.parseDeclaration(buf))
		case '{':
			flushDeclarations()
			rules = append(rules, p.parseRule(buf, selectors)...)
		default:
			panic(`缺少 { 或 :`)
		}
	}
}

type BufioReader struct {
	*bufio.Reader
}

func (b *BufioReader) skipSpaces() {
	for {
		c, err := b.Peek(1)
		if err != nil {
			if err == io.EOF {
				return
			}
			panic(err)
		}
		switch c[0] {
		case ' ', '\t', '\n':
			b.Discard(1)
			continue
		default:
			return
		}
	}
}

func (b *BufioReader) peekByte() byte {
	c, err := b.Reader.Peek(1)
	if err != nil {
		return 0
	}
	return c[0]
}

func (b *BufioReader) readUntil(stop byte) string {
	tmp := []byte{}
	for {
		c := b.peekByte()
		if c == stop || c == 0 {
			return string(tmp)
		}
		b.Discard(1)
		tmp = append(tmp, c)
	}
}

// nextDelimiter 用于区分声明和嵌套规则。当前 CSS 子集的声明名后一定是 :，
// 选择器后一定是 {。ParseStyle 为 Reader 配置了可容纳整份样式表的缓冲区。
func (b *BufioReader) nextDelimiter() byte {
	for n := 1; ; n++ {
		data, err := b.Peek(n)
		if err != nil {
			if err == io.EOF {
				return 0
			}
			panic(err)
		}
		switch data[n-1] {
		case ':', '{', '}':
			return data[n-1]
		}
	}
}

func (p StyleParser) expandSelectors(parents []Selector, header string) []Selector {
	parts := strings.Split(header, `,`)
	if len(parents) == 0 {
		selectors := make([]Selector, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.Contains(part, `&`) {
				panic(`& 只能用在嵌套选择器中`)
			}
			selectors = append(selectors, p.ParseSelector(part))
		}
		return selectors
	}

	selectors := make([]Selector, 0, len(parents)*len(parts))
	for _, parent := range parents {
		parentText := p.selectorString(parent)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == `` {
				panic(`没有选择器。`)
			}
			var expanded string
			switch {
			case strings.Contains(part, `&`):
				expanded = strings.ReplaceAll(part, `&`, parentText)
			case strings.HasPrefix(part, `>`):
				expanded = parentText + ` ` + part
			default:
				expanded = parentText + ` ` + part
			}
			selectors = append(selectors, p.ParseSelector(expanded))
		}
	}
	return selectors
}

func (StyleParser) selectorString(selector Selector) string {
	var out strings.Builder
	for i, node := range selector {
		if i > 0 {
			if selector[i-1].Combinator == childCombinator {
				out.WriteString(` > `)
			} else {
				out.WriteByte(' ')
			}
		}
		out.WriteString(node.Tag)
		if node.Asterisk {
			out.WriteByte('*')
		}
		if node.ID != `` {
			out.WriteByte('#')
			out.WriteString(node.ID)
		}
		for _, class := range node.Class {
			out.WriteByte('.')
			out.WriteString(class)
		}
	}
	return out.String()
}

// ParseSelector 解析单个选择器。无效选择器会 panic，与 DOM 查询的既有行为一致。
func (p StyleParser) ParseSelector(selector string) Selector {
	buf := BufioReader{
		Reader: bufio.NewReader(strings.NewReader(selector)),
	}
	return p.parseSelector(&buf)
}

// 解析选择器。
//
// 支持的语法：
//   - block
//   - #id
//   - .class
//   - *
//   - >
func (p StyleParser) parseSelector(buf *BufioReader) []NodeSelector {
	selectors := []NodeSelector{}
	current := NodeSelector{}

	buf.skipSpaces()

	for {
		b := buf.peekByte()
		if b == '#' {
			buf.Discard(1)
			current.ID = p.parseIdent(buf)
			current.Specificity += 1 << 16
		} else if b == '.' {
			buf.Discard(1)
			current.Class = append(current.Class, p.parseIdent(buf))
			current.Specificity += 1 << 8
		} else if p.isIdentChar(b) {
			current.Tag = p.parseIdent(buf)
			current.Specificity += 1 << 0
		} else if b == '*' {
			buf.Discard(1)
			current.Asterisk = true
		} else if b == ',' || b == '{' || b == 0 {
			break
		} else if b == '>' {

		} else {
			panic(`不认识的字符:` + string(b))
		}
		if b := buf.peekByte(); b == ' ' || b == '\t' || b == '*' || b == '>' || b == 0 || b == '{' {
			if b == '>' {
				buf.Discard(1)
				if len(selectors) <= 0 {
					panic(`无效child combinator`)
				}
				last := &selectors[len(selectors)-1]
				last.Combinator = childCombinator
				buf.skipSpaces()
			} else {
				selectors = append(selectors, current)
				current = NodeSelector{}
				buf.skipSpaces()
			}
		}
	}

	if len(selectors) <= 0 {
		panic(`没有选择器。`)
	}

	// 最后一个不能有 combinator
	last := selectors[len(selectors)-1]
	if last.Combinator != 0 {
		panic(`最后一个选择器不能有Combinator`)
	}

	return selectors
}

func (p StyleParser) parseDeclaration(buf *BufioReader) Declaration {
	current := Declaration{}
	buf.skipSpaces()
	current.Name = p.parseIdent(buf)
	if current.Name == `` {
		panic(`没有声明名`)
	}
	buf.skipSpaces()
	if b := buf.peekByte(); b != ':' {
		panic(`缺少 :`)
	}
	buf.Discard(1)
	tmp := []byte{}
	for {
		b := buf.peekByte()
		if b == ';' || b == 0 {
			break
		}
		buf.Discard(1)
		tmp = append(tmp, b)
	}
	if len(tmp) <= 0 {
		panic(`没有值`)
	}

	value := strings.TrimSpace(string(tmp))
	if c := value[0]; c == '"' || c == '\'' {
		value = value[1:]
	}
	if c := value[len(value)-1]; c == '"' || c == '\'' {
		value = value[:len(value)-1]
	}
	current.Value = value
	if b := buf.peekByte(); b != ';' {
		panic(`缺少 ;`)
	}
	buf.Discard(1)
	return current
}

func (StyleParser) isIdentChar(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || b == '-' || b == '_'
}

func (p StyleParser) parseIdent(buf *BufioReader) string {
	tmp := []byte{}
	for {
		b := buf.peekByte()
		if p.isIdentChar(b) {
			buf.Discard(1)
			tmp = append(tmp, b)
			continue
		}
		break
	}
	if len(tmp) == 0 {
		panic(`缺少标识符`)
	}
	return string(tmp)
}

/// ---------------------------------------------------------------------------

type _Styler struct {
	// 系统全局样式表。
	defaultStyles *Sheet

	// 目前的doc不像是html一样是body的parent节点，
	// doc的body(即doc.root)和doc是没有parent关系的，
	// 所以需要单独拿出来应用。但是允许为空，方便调试。
	documentStyles *Styles
}

// 计算样式。
//
// 样式的几个来源：
//
//  1. 从系统级样式表（User-Agent Styles）；
//  2. 从 <document>，因为目前 doc 不是 root box 的父节点；
//  3. 从 document html 文件内的 <style> 节点，即参数 `sheet`。
func (s _Styler) Style(box Box, descendents bool, sheet *Sheet) (outErr error) {
	walkBox(box, func(box Box) bool {
		// 因为默认样式的优先级 < 页面提供的样式（即便前者 spec 更高），
		// 所以这里不能放在一起并被后面排序。
		var rules [][]RuleMatch
		if s.defaultStyles != nil {
			rules = append(rules, s.findRulesFor(box, s.defaultStyles))
		}
		if sheet != nil {
			rules = append(rules, s.findRulesFor(box, sheet))
		}
		if err := s.computeStyles(box, rules); err != nil {
			outErr = fmt.Errorf(`样式应用失败：%w`, err)
			return false
		}
		return descendents
	})
	return
}

// 从样式规则里面找出匹配节点的规则集。
// 找到的规则没有排序。
func (s _Styler) findRulesFor(node Box, sheet *Sheet) []RuleMatch {
	matches := []RuleMatch{}
	for _, rule := range sheet.Rules {
		if s.match(node, rule.Selector) {
			spec := uint32(0)
			for _, sel := range rule.Selector {
				spec += sel.Specificity
			}
			matches = append(matches, RuleMatch{
				Specificity:  spec,
				Declarations: rule.Declarations,
			})
		}
	}
	return matches
}

// 按优先级递减返回声明。样式来源越靠后优先级越高；同一来源内，
// specificity 越高优先级越高；其余情况下，源码中靠后的声明优先。
func (s _Styler) declarationsByPriority(rulesSet [][]RuleMatch) iter.Seq[Declaration] {
	for _, rules := range rulesSet {
		slices.SortStableFunc(rules, func(a, b RuleMatch) int {
			return int(a.Specificity) - int(b.Specificity)
		})
	}
	return func(yield func(Declaration) bool) {
		for _, rules := range slices.Backward(rulesSet) {
			for _, rule := range slices.Backward(rules) {
				for _, d := range slices.Backward(rule.Declarations) {
					if !yield(d) {
						return
					}
				}
			}
		}
	}
}

// 为节点计算样式。依次完成 cascade、defaulting 和相对值计算，
// 最后直接将结果保存到节点。
func (s _Styler) computeStyles(node Box, rules [][]RuleMatch) error {
	// Cascade：内联样式优先，然后从高到低查找样式表声明。每个属性
	// 一旦取得值，低优先级声明就不能再覆盖它。
	styles := node.Base().inlineStyles
	stylesValue := reflect.ValueOf(&styles).Elem()
	for d := range s.declarationsByPriority(rules) {
		_, _, _, current, update, err := styles.parseProperty(d.Name, d.Value)
		if err != nil {
			return fmt.Errorf(`样式应用错误：%w`, err)
		}
		property := stylePropertyByName(d.Name)
		if !styles.has(property) {
			assignStyleProperty(current, update)
			styles.mark(property)
		}
	}

	// Defaulting：当前节点没有指定可继承属性时，才从最近的祖先，
	// 最后从 <document> 获取计算值。
	optDocValue := reflect.ValueOf(s.documentStyles)
	for field, value := range stylesValue.Fields() {
		if !shouldInherit(field.Name) {
			continue
		}
		property := stylePropertyByName(field.Name)
		if !styles.has(property) {
			setFromParent := false
			for parent := range node.Base().Ancestors() {
				parentStyles := parent.GetComputedStyles()
				parentValue := reflect.ValueOf(parentStyles)
				parentField := parentValue.Elem().FieldByIndex(field.Index)
				if parentStyles.has(property) {
					value.Set(parentField)
					styles.mark(property)
					setFromParent = true
					// 从最近的祖先那里获取一次即可。
					break
				}
			}
			// <document> 才是最终的根节点。
			if !setFromParent && !optDocValue.IsNil() {
				docField := optDocValue.Elem().FieldByIndex(field.Index)
				if s.documentStyles.has(property) {
					value.Set(docField)
					styles.mark(property)
				}
			}
		}
	}

	// Compute：百分比字号相对于父节点的计算字号。根节点没有父节点时，
	// <document> 充当它的继承来源。
	if styles.FontSize.IsPercentage() {
		base := Length{}
		if parent := node.Parent(); parent != nil {
			base = parent.GetComputedStyles().FontSize
		} else if s.documentStyles != nil {
			base = s.documentStyles.FontSize
		}
		if base.IsNumber() {
			styles.FontSize = NumberLength(base.number * styles.FontSize.number / 100)
		}
	}

	// rem 始终相对于 <document> 的计算字号，不受中间祖先字号影响。
	if styles.FontSize.IsRem() && s.documentStyles != nil {
		base := s.documentStyles.FontSize
		if base.IsNumber() {
			styles.FontSize = NumberLength(base.number * styles.FontSize.number / remScale)
		}
	}

	// 直接保存起来。
	node.Base().computedStyles = styles

	return nil
}

// 判断节点是否和选择器完整匹配。
func (s _Styler) match(node Box, selector Selector) bool {
	// 先是自身匹配
	if !s._matchSelf(node, selector[len(selector)-1]) {
		return false
	}

	if len(selector) <= 1 {
		return true
	}

	// 如果自身匹配，继续往上寻找可能的祖先匹配。
	// 每一个后代选择器都需要对每个祖先进行尝试。
	ancestorSelectors := selector[:len(selector)-1]

	return s._matchAncestors(node, ancestorSelectors)
}

// 判断单个简单选择器是否匹配当前节点。
func (s _Styler) _matchSelf(node Box, selector NodeSelector) bool {
	if selector.Asterisk {
		return true
	}
	return (selector.Tag == `` || selector.Tag == node.Base().Tag) &&
		(len(selector.Class) == 0 || node.Base().class.ContainsAll(selector.Class...)) &&
		(selector.ID == `` || selector.ID == node.Base().ID)
}

func (s _Styler) _matchAncestors(node Box, ancestorSelectors Selector) bool {
	return s._matchAncestorsRecursive(node, ancestorSelectors, len(ancestorSelectors)-1)
}

// 找单个选择器能匹配的至少一个祖先。
// 基本约等于 document.querySelector 的功能。
// 递归好烧脑。
func (s _Styler) _matchAncestorsRecursive(node Box, ancestorSelectors Selector, backIndex int) bool {
	// 前面的所有选择器均匹配上了，并且已经没有选择器了，
	// 所以到这里就表示所有选择器匹配成功了。
	if backIndex < 0 {
		return true
	}

	// 如果前一个选择器有combinator，可以快速在此判断。
	if ancestorSelectors[backIndex].Combinator == childCombinator {
		parent := node.Parent()
		if parent != nil && !s._matchSelf(parent, ancestorSelectors[backIndex]) {
			return false
		}
	}

	for ancestor := range node.Base().Ancestors() {
		if s._matchSelf(ancestor, ancestorSelectors[backIndex]) {
			if s._matchAncestorsRecursive(ancestor, ancestorSelectors, backIndex-1) {
				return true
			}
		}
	}
	return false
}
