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
)

// 用于保存节点的样式值。
type Styles struct {
	// 子元素的水平/垂直对齐方式。
	//   - 默认("")水平居左，“center”居中。
	//   - 默认("")垂直居顶，“middle”居中。
	//   - “both”两者均居中。
	Align Value

	BackgroundColor Value
	BackgroundImage Value
	BorderColor     Value
	BorderWidth     Value
	OutlineWidth    Value
	OutlineColor    Value
	Color           Value
	Height          Value
	Padding         Value
	Width           Value

	FontFamily Value // font-family
	FontSize   Value // font-size
	FontBold   Value // bold
	FontItalic Value // italic

	// 是否当作Spacer可变大小布局。
	Spacer Value

	// 显示属性。布尔类型。
	// 如果为true，参与排版；如果为false，完全隐藏。
	// 此属性虽非继承属性，但是子盒子即便为true但父盒子为false时，
	// 此子盒子仍然不会被显示。所以不能通过判断子盒子的display是否
	// 为true来判断子盒子是否正处于显示状态。
	Display Value

	// 填充方式。
	Fill Value
}

// 可替换对象内容的填充方式。
// [object-fit CSS property - CSS | MDN](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/object-fit)
//
// 目前的canvas绘图不能超出范围，所以暂时不支持 cover 和 none。
// 大多数时候用 scale-down 足够？
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

var DefaultStyles = Must1(ParseStyle(`
document {
	color: black;
	font-family: system;
	font-size: 32;
}
b {
	bold: true;
}
i {
	italic: true;
}
.h1 { font-size: 1.50rem; }
.h2 { font-size: 1.35rem; }
.h3 { font-size: 1.20rem; }
`))

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
func (s *Styles) Set(name string, raw string) (affectInherit, affectLayout, affectPaint bool, outErr error) {
	var current *Value
	var update Value

	affectInherit, affectLayout, affectPaint, current, update, outErr = s.parseProperty(name, raw)
	if outErr == nil {
		*current = update
	}

	return
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
	current *Value, update Value,
	outErr error,
) {
	setNumberOrPercentage := func(v *Value, raw string) error {
		if before, ok := strings.CutSuffix(raw, `%`); ok {
			n, err := strconv.Atoi(before)
			*v = PercentageValue(n)
			return err
		} else {
			n, err := strconv.Atoi(before)
			*v = NumberValue(n)
			return err
		}
	}
	setFontSize := func(v *Value, raw string) error {
		if before, ok := strings.CutSuffix(raw, `rem`); ok {
			n, err := strconv.ParseFloat(before, 64)
			if err != nil {
				return err
			}
			if n < 0 {
				return fmt.Errorf(`字号不能为负数：%s`, raw)
			}
			*v = RemValue(n)
			return nil
		}
		if n, ok := namedFontSizeScale[raw]; ok {
			*v = PercentageValue(n)
			return nil
		}
		return setNumberOrPercentage(v, raw)
	}
	setNumber := func(v *Value, raw string) error {
		n, err := strconv.Atoi(raw)
		*v = NumberValue(n)
		return err
	}
	setPadding := func(v *Value, raw string) error {
		const maxPadding = int(^uint16(0))
		parts := strings.Fields(raw)
		if len(parts) < 1 || len(parts) > 4 {
			return fmt.Errorf(`padding 需要 1 到 4 个值：%s`, raw)
		}
		values := make([]uint16, len(parts))
		for i, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil {
				return err
			}
			if n < 0 || n > maxPadding {
				return fmt.Errorf(`padding 必须在 0 到 %d 之间：%s`, maxPadding, raw)
			}
			values[i] = uint16(n)
		}
		switch len(values) {
		case 1:
			*v = PaddingValue(values[0], values[0], values[0], values[0])
		case 2:
			*v = PaddingValue(values[0], values[1], values[0], values[1])
		case 3:
			*v = PaddingValue(values[0], values[1], values[2], values[1])
		case 4:
			*v = PaddingValue(values[0], values[1], values[2], values[3])
		}
		return nil
	}
	setColor := func(v *Value, raw string) error {
		vv, err := ParseColor(raw)
		*v = vv
		return err
	}
	setBoolean := func(v *Value, raw string, emptyIsTrue bool) error {
		switch raw {
		case `1`, `true`:
			*v = BoolValue(true)
			return nil
		case `0`, `false`:
			*v = BoolValue(false)
			return nil
		case ``:
			*v = BoolValue(emptyIsTrue)
			return nil
		default:
			return fmt.Errorf(`未知布尔值：%v`, raw)
		}
	}
	switch name {
	default:
		outErr = ErrUnknownStyleProperty
		return
	case `align`:
		if raw == `` || raw == `center` || raw == `middle` || raw == `both` {
			current = &s.Align
			update = StringValue(raw)
			affectLayout = true
			return
		}
		outErr = fmt.Errorf(`不认识的对齐方式：%s`, raw)
		return
	case `background-color`:
		affectPaint = true
		current = &s.BackgroundColor
		outErr = setColor(&update, raw)
		return
	case `background-image`:
		current = &s.BackgroundImage
		update = StringValue(raw)
		affectPaint = true
		return
	case `border-color`:
		affectPaint = true
		current = &s.BorderColor
		outErr = setColor(&update, raw)
		return
	case `border-width`:
		affectLayout = true
		current = &s.BorderWidth
		outErr = setNumber(&update, raw)
		return
	case `outline-color`:
		affectPaint = true
		current = &s.OutlineColor
		outErr = setColor(&update, raw)
		return
	case `outline-width`:
		affectLayout = true
		current = &s.OutlineWidth
		outErr = setNumber(&update, raw)
		return
	case `color`:
		affectInherit = true
		affectPaint = true
		current = &s.Color
		outErr = setColor(&update, raw)
		return
	case `height`:
		affectLayout = true
		current = &s.Height
		outErr = setNumberOrPercentage(&update, raw)
		return
	case `padding`:
		affectLayout = true
		current = &s.Padding
		outErr = setPadding(&update, raw)
		return
	case `width`:
		affectLayout = true
		current = &s.Width
		outErr = setNumberOrPercentage(&update, raw)
		return
	case `font-family`:
		// 不同字体大小不一样，所以也会影响布局
		affectInherit = true
		affectLayout = true
		current = &s.FontFamily
		update = StringValue(raw)
		return
	case `font-size`:
		affectInherit = true
		affectLayout = true
		current = &s.FontSize
		outErr = setFontSize(&update, raw)
		return
	case `bold`, `font-bold`:
		affectInherit = true
		affectLayout = true
		current = &s.FontBold
		outErr = setBoolean(&update, raw, true)
		return
	case `italic`, `font-italic`:
		affectInherit = true
		affectLayout = true
		current = &s.FontItalic
		outErr = setBoolean(&update, raw, true)
		return
	case `spacer`:
		affectLayout = true
		current = &s.Spacer
		outErr = setBoolean(&update, raw, true)
		return
	case `display`:
		affectLayout = true
		current = &s.Display
		outErr = setBoolean(&update, raw, true)
		return
	case `fill`:
		affectLayout = true
		switch raw {
		case ``, `stretch`:
			update = NumberValue(0)
		case `none`:
			// 太大的图片绘制会超出canvas范围，还没修bug
			// 大多数时候使用 scale-down 其实足够。
			panic(`目前不支持none填充模式`)
			// update = NumberValue(int(FillNone))
		case `contain`:
			update = NumberValue(int(FillContain))
		case `cover`:
			panic(`目前不支持cover填充模式`)
			// update = NumberValue(int(FillCover))
		case `scale-down`:
			update = NumberValue(int(FillScaleDown))
		default:
			outErr = fmt.Errorf(`不认识的填充方式: %s`, raw)
			return
		}
		affectLayout = true
		current = &s.Fill
		return
	}
}

type _ValueType uint8

const (
	VTNone _ValueType = iota
	VTString
	VTColor
	VTNumber
	VTPercentage
	VTBool
	VTRem
)

// 表示各种样式值。
type Value struct {
	Type _ValueType

	String string
	Color  Color
	Number int64
	Bool   bool
}

// 特别地：对于颜色来说，Empty() 只表示它没有设置，
// 但它仍然要从父元素继承。为了不继承，需要判断 Color.None()。
func (v Value) Empty() bool {
	return v.Type == VTNone
}

func (v Value) IsString() bool {
	return v.Type == VTString
}
func (v Value) IsNumber() bool {
	return v.Type == VTNumber
}
func (v Value) IsPercentage() bool {
	return v.Type == VTPercentage
}
func (v Value) IsRem() bool {
	return v.Type == VTRem
}
func (v Value) IsBool() bool {
	return v.Type == VTBool
}
func (v Value) IsColor() bool {
	return v.Type == VTColor
}
func (v Value) Fill() Fill {
	return Fill(v.Number)
}

func StringValue(s string) Value {
	return Value{
		Type:   VTString,
		String: s,
	}
}
func ColorValue(cr Color) Value {
	return Value{
		Type:  VTColor,
		Color: cr,
	}
}

// 如果解析失败，会崩溃。
func ColorValueFromString(cr string) Value {
	return Must1(ParseColor(cr))
}

func NumberValue[T ~int | ~int64](v T) Value {
	return Value{
		Type:   VTNumber,
		Number: int64(v),
	}
}
func PercentageValue[T ~int | ~int64](v T) Value {
	return Value{
		Type:   VTPercentage,
		Number: int64(v),
	}
}

const remScale = 1000

// RemValue 使用千分之一 rem 保存小数，避免样式计算引入浮点误差。
func RemValue(v float64) Value {
	return Value{
		Type:   VTRem,
		Number: int64(math.Round(v * remScale)),
	}
}

func PaddingValue(top, right, bottom, left uint16) Value {
	packed := uint64(top)<<48 |
		uint64(right)<<32 |
		uint64(bottom)<<16 |
		uint64(left)
	return Value{
		Type:   VTNumber,
		Number: int64(packed),
	}
}

const paddingMask = uint64(0xffff)

func (v Value) PaddingTop() int {
	return int(uint64(v.Number) >> 48 & paddingMask)
}

func (v Value) PaddingRight() int {
	return int(uint64(v.Number) >> 32 & paddingMask)
}

func (v Value) PaddingBottom() int {
	return int(uint64(v.Number) >> 16 & paddingMask)
}

func (v Value) PaddingLeft() int {
	return int(uint64(v.Number) & paddingMask)
}
func BoolValue(v bool) Value {
	return Value{
		Type: VTBool,
		Bool: v,
	}
}

// 0xAA_RR_GG_BB
// 与设备的像素格式匹配（低端序）
type Color uint32

const (
	ColorNone = Color(0x00010101)

	// 特殊的打洞色。
	// 使用此色后，此块屏幕区域会直接清空成透明色。
	//
	// 此值的特殊背景：游戏机的GPU可以在UI层下面叠加一层
	// 视频层，由于在UI层下面，这就要求UI层透明。最简单的办法是
	// 直接清空需要的区域，而不是隐藏下面的所以文档/控件层，太麻烦了。
	ColorClear = Color(0x00020202)
)

func ColorFromRGBA(r, g, b, a uint8) Color {
	out := uint32(0)
	out |= uint32(b) << 0
	out |= uint32(g) << 8
	out |= uint32(r) << 16
	out |= uint32(a) << 24
	return Color(out)
}

// 特殊值：判断是否为空色。
//
// 如果父元素设备了背景，子元素不想要。
// 这时候如果什么也不写，会导致继承。
// 所以只能写个none。
func (c Color) None() bool {
	return c == ColorNone
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
func (c Color) Invert() Color {
	return c ^ 0x00FFFFFF
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

func ParseColor(c string) (_ Value, outErr error) {
	if len(c) == 0 {
		return Value{}, nil
	}

	switch c {
	case `none`:
		return ColorValue(ColorNone), nil
	case `clear`:
		return ColorValue(ColorClear), nil
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

	return ColorValue(ColorFromRGBA(cr.R, cr.G, cr.B, cr.A)), nil
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
	for _, name := range strings.Split(s, `,`) {
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

type _Rule struct {
	Selectors    []Selector
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

func ParseStyle(data string) (_ *Sheet, outErr error) {
	buf := &BufioReader{
		Reader: bufio.NewReader(strings.NewReader(data)),
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
		rule := parseRule(buf)
		for _, s := range rule.Selectors {
			ss.Rules = append(ss.Rules, Rule{
				Selector:     s,
				Declarations: rule.Declarations,
			})
		}
	}

	return &ss, nil
}

func parseRule(buf *BufioReader) _Rule {
	r := _Rule{}

	for {
		selectors := parseSelector(buf)
		r.Selectors = append(r.Selectors, selectors)
		buf.skipSpaces()
		b := buf.peekByte()
		if b == ',' {
			continue
		}
		break
	}

	buf.skipSpaces()
	if b := buf.peekByte(); b != '{' {
		panic(`缺少 {`)
	}
	buf.Discard(1)

	buf.skipSpaces()
	if b := buf.peekByte(); b != '}' {
		r.Declarations = parseDeclarations(buf)
	}

	buf.skipSpaces()
	if b := buf.peekByte(); b != '}' {
		panic(`缺少 }`)
	}
	buf.Discard(1)

	return r
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

func parseSelectorString(selector string) Selector {
	buf := BufioReader{
		Reader: bufio.NewReader(strings.NewReader(selector)),
	}
	return parseSelector(&buf)
}

// 解析选择器。
//
// 支持的语法：
//   - block
//   - #id
//   - .class
//   - *
//   - >
func parseSelector(buf *BufioReader) []NodeSelector {
	selectors := []NodeSelector{}
	current := NodeSelector{}

	buf.skipSpaces()

	for {
		b := buf.peekByte()
		if b == '#' {
			buf.Discard(1)
			current.ID = parseIdent(buf)
			current.Specificity += 1 << 16
		} else if b == '.' {
			buf.Discard(1)
			current.Class = append(current.Class, parseIdent(buf))
			current.Specificity += 1 << 8
		} else if isIdentChar(b) {
			current.Tag = parseIdent(buf)
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

func parseDeclarations(buf *BufioReader) []Declaration {
	d := []Declaration{}

	current := Declaration{}

	for {
		buf.skipSpaces()
		current.Name = parseIdent(buf)
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
		d = append(d, current)
		if b := buf.peekByte(); b != ';' {
			panic(`缺少 ;`)
		}
		buf.Discard(1)
		buf.skipSpaces()
		if b := buf.peekByte(); b == 0 || b == '}' {
			break
		}
	}

	return d
}

func isIdentChar(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || b == '-' || b == '_'
}

func parseIdent(buf *BufioReader) string {
	tmp := []byte{}
	for {
		b := buf.peekByte()
		if isIdentChar(b) {
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
		if current.Empty() {
			*current = update
		}
	}

	// Defaulting：当前节点没有指定可继承属性时，才从最近的祖先，
	// 最后从 <document> 获取计算值。
	optDocValue := reflect.ValueOf(s.documentStyles)
	for field, value := range stylesValue.Fields() {
		if !shouldInherit(field.Name) {
			continue
		}
		if field.Type == reflect.TypeFor[Value]() && value.Interface().(Value).Empty() {
			setFromParent := false
			for parent := range node.Base().Ancestors() {
				parentValue := reflect.ValueOf(parent.GetComputedStyles())
				parentField := parentValue.Elem().FieldByIndex(field.Index)
				value3 := parentField.Interface().(Value)
				if !value3.Empty() {
					value.Set(parentField)
					setFromParent = true
					// 从最近的祖先那里获取一次即可。
					break
				}
			}
			// <document> 才是最终的根节点。
			if !setFromParent && !optDocValue.IsNil() {
				docField := optDocValue.Elem().FieldByIndex(field.Index)
				docValue := docField.Interface().(Value)
				if !docValue.Empty() {
					value.Set(docField)
				}
			}
		}
	}

	// Compute：百分比字号相对于父节点的计算字号。根节点没有父节点时，
	// <document> 充当它的继承来源。
	if styles.FontSize.IsPercentage() {
		base := Value{}
		if parent := node.Parent(); parent != nil {
			base = parent.GetComputedStyles().FontSize
		} else if s.documentStyles != nil {
			base = s.documentStyles.FontSize
		}
		if base.IsNumber() {
			styles.FontSize = NumberValue(base.Number * styles.FontSize.Number / 100)
		}
	}

	// rem 始终相对于 <document> 的计算字号，不受中间祖先字号影响。
	if styles.FontSize.IsRem() && s.documentStyles != nil {
		base := s.documentStyles.FontSize
		if base.IsNumber() {
			styles.FontSize = NumberValue(base.Number * styles.FontSize.Number / remScale)
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
