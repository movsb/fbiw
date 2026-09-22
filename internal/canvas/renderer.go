package canvas

import (
	"fmt"
	"image"
	"image/color"
)

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

func (c Color) IsNone() bool       { return c == ColorNone }
func (c Color) IsClear() bool      { return c == ColorClear }
func (c Color) R() uint8           { return uint8(c >> 16) }
func (c Color) G() uint8           { return uint8(c >> 8) }
func (c Color) B() uint8           { return uint8(c >> 0) }
func (c Color) A() uint8           { return uint8(c >> 24) }
func (c Color) NRGBA() color.NRGBA { return color.NRGBA{R: c.R(), G: c.G(), B: c.B(), A: c.A()} }
func (c Color) Value() uint32      { return uint32(c) }
func (c Color) String() string     { return fmt.Sprintf(`#%02x%02x%02x%02x`, c.R(), c.G(), c.B(), c.A()) }

type Image struct {
	Pixels        []byte
	Width, Height int
	Opaque        bool
}

type Renderer interface {
	Size() (int, int)

	BeginFrame()
	EndFrame()

	Clear()
	FillRect(rect, clip image.Rectangle, color Color)
	DrawImage(img Image, src image.Rectangle, degrees, scaleX, scaleY, cx, cy float64, clip image.Rectangle)
	DrawMask(mask []byte, maskWidth, maskHeight int, dst image.Point, clip image.Rectangle, color Color)
	Snapshot() image.Image

	Close() error
}

type TestRenderer interface {
	Renderer
	Pixel(point image.Point) color.NRGBA
	SetPixel(point image.Point, color color.NRGBA)
}
