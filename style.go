package fbiw

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io"
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

var DefaultStyles = Must1(ParseStyle([]byte(`
document {
	color: black;
	font-family: system;
	font-size: 25;
}
b {
	bold: true;
}
i {
	italic: true;
}
`)))

// 直接传入的是结构体字段，原始名字，没有小写、没有中划线。
func ShouldInherit(name string) bool {
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
	setNumber := func(v *Value, raw string) error {
		n, err := strconv.Atoi(raw)
		*v = NumberValue(n)
		return err
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
			s.Align = StringValue(raw)
			affectLayout = true
			return
		}
		outErr = fmt.Errorf(`不认识的对齐方式：%s`, raw)
		return
	case `background-color`:
		affectPaint = true
		outErr = setColor(&s.BackgroundColor, raw)
		return
	case `background-image`:
		s.BackgroundImage = StringValue(raw)
		affectPaint = true
		return
	case `border-color`:
		affectPaint = true
		outErr = setColor(&s.BorderColor, raw)
		return
	case `border-width`:
		affectLayout = true
		outErr = setNumber(&s.BorderWidth, raw)
		return
	case `outline-color`:
		affectPaint = true
		outErr = setColor(&s.OutlineColor, raw)
		return
	case `outline-width`:
		affectLayout = true
		outErr = setNumber(&s.OutlineWidth, raw)
		return
	case `color`:
		affectInherit = true
		affectPaint = true
		outErr = setColor(&s.Color, raw)
		return
	case `height`:
		affectLayout = true
		outErr = setNumberOrPercentage(&s.Height, raw)
		return
	case `padding`:
		affectLayout = true
		outErr = setNumber(&s.Padding, raw)
		return
	case `width`:
		affectLayout = true
		outErr = setNumberOrPercentage(&s.Width, raw)
		return
	case `font-family`:
		// 不同字体大小不一样，所以也会影响布局
		affectInherit = true
		affectLayout = true
		s.FontFamily = StringValue(raw)
		return
	case `font-size`:
		affectInherit = true
		affectLayout = true
		outErr = setNumber(&s.FontSize, raw)
		return
	case `bold`, `font-bold`:
		affectInherit = true
		affectLayout = true
		outErr = setBoolean(&s.FontBold, raw, true)
		return
	case `italic`, `font-italic`:
		affectInherit = true
		affectLayout = true
		outErr = setBoolean(&s.FontItalic, raw, true)
		return
	case `spacer`:
		affectLayout = true
		outErr = setBoolean(&s.Spacer, raw, true)
		return
	case `display`:
		affectLayout = true
		outErr = setBoolean(&s.Display, raw, true)
		return
	case `fill`:
		affectLayout = true
		switch raw {
		case ``, `stretch`:
			s.Fill = NumberValue(0)
		case `none`:
			// 太大的图片绘制会超出canvas范围，还没修bug
			// 大多数时候使用 scale-down 其实足够。
			panic(`目前不支持none填充模式`)
			s.Fill = NumberValue(int(FillNone))
		case `contain`:
			s.Fill = NumberValue(int(FillContain))
		case `cover`:
			panic(`目前不支持cover填充模式`)
			s.Fill = NumberValue(int(FillCover))
		case `scale-down`:
			s.Fill = NumberValue(int(FillScaleDown))
		default:
			outErr = fmt.Errorf(`不认识的填充方式: %s`, raw)
			return
		}
		affectLayout = true
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
)

// 表示各种样式值。
type Value struct {
	Type _ValueType

	String string
	Color  Color
	Number int
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

func NumberValue(v int) Value {
	return Value{
		Type:   VTNumber,
		Number: v,
	}
}
func PercentageValue(v int) Value {
	return Value{
		Type:   VTPercentage,
		Number: v,
	}
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

func ParseStyle(data []byte) (_ *Sheet, outErr error) {
	buf := &BufioReader{
		Reader: bufio.NewReader(bytes.NewReader(data)),
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
