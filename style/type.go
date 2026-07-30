package style

import (
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
)

// 用于保存节点的样式值。
type Styles struct {
	// 子元素的水平/垂直对齐方式。
	// 默认("")居左，“center”居中。
	// 默认("")居顶，“middle”居中。
	Align Value

	BackgroundColor Value
	BackgroundImage Value
	BorderColor     Value
	BorderWidth     Value
	Color           Value
	Height          Value
	Padding         Value
	Width           Value
}

func ShouldInherit(name string) bool {
	switch name {
	case `color`:
		return true
	default:
		return false
	}
}

var ErrUnknownStyleProperty = errors.New(`未知样式属性`)

func (s *Styles) Set(name string, raw string) error {
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

	switch name {
	default:
		return ErrUnknownStyleProperty
	case `align`:
		if raw == `` || raw == `center` || raw == `middle` {
			s.Align = StringValue(raw)
			return nil
		}
		return fmt.Errorf(`不认识的对齐方式：%s`, raw)
	case `background-color`:
		return setColor(&s.BackgroundColor, raw)
	case `background-image`:
		s.BackgroundImage = StringValue(raw)
		return nil
	case `border-color`:
		return setColor(&s.BorderColor, raw)
	case `border-width`:
		return setNumber(&s.BorderWidth, raw)
	case `color`:
		return setColor(&s.Color, raw)
	case `height`:
		return setNumberOrPercentage(&s.Height, raw)
	case `padding`:
		return setNumber(&s.Padding, raw)
	case `width`:
		return setNumberOrPercentage(&s.Width, raw)
	}
}

type _ValueType uint8

const (
	VTNone _ValueType = iota
	VTString
	VTColor
	VTNumber
	VTPercentage
)

type Value struct {
	Type _ValueType

	String string
	Color  Color // 0xRR_GG_BB_AA
	Number int
}

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

type Color uint32

func (c Color) R() uint8 {
	return uint8(c >> 24)
}
func (c Color) G() uint8 {
	return uint8(c >> 16)
}
func (c Color) B() uint8 {
	return uint8(c >> 8)
}
func (c Color) A() uint8 {
	return uint8(c)
}
func (c Color) NRGBA() color.NRGBA {
	return color.NRGBA{
		R: c.R(),
		G: c.G(),
		B: c.B(),
		A: c.A(),
	}
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
func (c Class) ContainsAny(class ...string) bool {
	return slices.ContainsFunc(class, c.Contains)
}
func (c *Class) Add(class string) {
	if !c.Contains(class) {
		c.class = append(c.class, class)
	}
}
func (c *Class) Remove(class string) {
	c.class = slices.DeleteFunc(c.class, func(each string) bool { return each == class })
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
				return b - 'a'
			case 'A' <= b && b <= 'F':
				return b - 'A'
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
			R: uint8(c >> 24),
			G: uint8(c >> 16),
			B: uint8(c >> 8),
			A: uint8(c),
		}
	} else {
		panic(`未知颜色`)
	}

	out := uint32(0)
	out |= uint32(cr.A) << 0
	out |= uint32(cr.B) << 8
	out |= uint32(cr.G) << 16
	out |= uint32(cr.R) << 24

	return ColorValue(Color(out)), nil
}

var presetColors = map[string]uint32{
	`coral`:          0xF08080FF,
	`salmon`:         0xE9967AFF,
	`red`:            0xFF325BFF,
	`hotpink`:        0xFF69B4FF,
	`deeppink`:       0xFF1493FF,
	`palevioletred`:  0xDB7093FF,
	`tomato`:         0xFF6347FF,
	`darkorange`:     0xFF8C00FF,
	`orange`:         0xFFA500FF,
	`yellow`:         0xFFD800FF,
	`darkkhaki`:      0xBDB76BFF,
	`magenta`:        0xDA70D6FF,
	`purple`:         0x9932CCFF,
	`slateblue`:      0x6A5ACDFF,
	`mediumseagreen`: 0x3CB371FF,
	`green`:          0x17A817FF,
	`yellowgreen`:    0x9ACD32FF,
	`olive`:          0x6B8E23FF,
	`darkseagreen`:   0x8FBC8BFF,
	`lightseagreen`:  0x20B2AAFF,
	`teal`:           0x008080FF,
	`cyan`:           0x00CED1FF,
	`aqua`:           0x00CED1FF,
	`cadetblue`:      0x5F9EA0FF,
	`steelblue`:      0x4682B4FF,
	`deepskyblue`:    0x00BFFFFF,
	`blue`:           0x1E90FFFF,
	`burlywood`:      0xDEB887FF,
	`tan`:            0xD2B48CFF,
	`rosybrown`:      0xBC8F8FFF,
	`sandybrown`:     0xF4A460FF,
	`goldenrod`:      0xDAA520FF,
	`darkgoldenrod`:  0xB8860BFF,
	`peru`:           0xCD853FFF,
	`chocolate`:      0xD2691EFF,
	`white`:          0xFFFFFFFF,
	`silver`:         0xC0C0C0FF,
	`darkgray`:       0xA9A9A9FF,
	`gray`:           0x808080FF,
	`slategray`:      0x708090FF,
	`black`:          0x000000FF,
}
