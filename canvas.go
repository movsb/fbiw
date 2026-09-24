package fbiw

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"io"
	"io/fs"
	"log"
	"math"
	"os"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/movsb/fbiw/internal/canvas"
	"github.com/movsb/fbiw/internal/helpers"
	"github.com/movsb/fbiw/internal/ports"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

type Renderer = canvas.Renderer

// OpenDisplay opens the renderer provided by the current platform port.
func OpenDisplay() Renderer {
	return ports.OpenDisplay()
}

// 绘图层。
//
// 提供基础绘制工具。
type Canvas struct {
	renderer         Renderer
	roundedRectMasks map[_RoundedRectMaskKey]*image.Alpha

	// 渲染的偏移坐标。
	x, y int

	// buffer 的宽度和高度。
	width, height int

	// framebuffer 坐标系中的可绘制区域。
	clip        image.Rectangle
	roundedClip image.Rectangle
	clipRadius  float64
}

type _RoundedRectMaskKey struct {
	width, height, radius, inset int
}

func NewCanvas(renderer Renderer) *Canvas {
	if renderer == nil {
		panic(`Canvas renderer不能为空`)
	}
	width, height := renderer.Size()
	if width <= 0 || height <= 0 {
		panic(`无效Canvas大小`)
	}
	return &Canvas{
		renderer:         renderer,
		roundedRectMasks: make(map[_RoundedRectMaskKey]*image.Alpha),
		width:            width,
		height:           height,
		x:                0,
		y:                0,
		clip:             image.Rect(0, 0, width, height),
	}
}

func (c *Canvas) Close() error {
	return c.renderer.Close()
}

// resize updates both the renderer backing store and the canvas viewport.
// Not every platform renderer is resizable (for example, a Linux framebuffer).
func (c *Canvas) resize(width, height int) error {
	if width <= 0 || height <= 0 || width == c.width && height == c.height {
		return nil
	}
	r, ok := c.renderer.(interface {
		Resize(width, height int) error
	})
	if !ok {
		return fmt.Errorf("renderer does not support resizing")
	}
	if err := r.Resize(width, height); err != nil {
		return err
	}
	c.width, c.height = width, height
	c.clip = image.Rect(0, 0, width, height)
	return nil
}

func (c *Canvas) beginFrame() { c.renderer.BeginFrame() }
func (c *Canvas) endFrame()   { c.renderer.EndFrame() }

func (c *Canvas) SaveToFile(path string) {
	fp, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer fp.Close()
	if err := png.Encode(fp, c.renderer.Snapshot()); err != nil {
		panic(err)
	}
}

// Clip 返回一个裁剪到当前局部矩形的新 Canvas，不修改原 Canvas。
func (c *Canvas) Clip(x, y, width, height int) *Canvas {
	clipped := *c
	r := image.Rect(c.x+x, c.y+y, c.x+x+max(0, width), c.y+y+max(0, height))
	clipped.clip = c.clipBounds().Intersect(r)
	return &clipped
}

// ClipRounded 返回一个带圆角图片裁剪的 Canvas，不修改原 Canvas。
func (c *Canvas) ClipRounded(x, y, width, height, radius int) *Canvas {
	clipped := c.Clip(x, y, width, height)
	clipped.roundedClip = image.Rect(c.x+x, c.y+y, c.x+x+max(0, width), c.y+y+max(0, height))
	clipped.clipRadius = float64(min(max(radius, 0), min(max(0, width), max(0, height))/2))
	return clipped
}

func (c *Canvas) clipBounds() image.Rectangle {
	if c.clip.Empty() {
		return image.Rect(0, 0, c.width, c.height)
	}
	return c.clip
}

// 提供局部平移 ——— 仅仅是把内部的起点 (x,y) 平移 (x,y) 个单位，并不承担任何裁剪功能。
// 非常类似于常见的 translate 方法。
func (c *Canvas) Offset(x, y int) *Canvas {
	if x == 0 && y == 0 {
		return c
	}
	return &Canvas{
		renderer:         c.renderer,
		roundedRectMasks: c.roundedRectMasks,
		x:                c.x + x,
		y:                c.y + y,
		width:            c.width,
		height:           c.height,
		clip:             c.clip,
		roundedClip:      c.roundedClip,
		clipRadius:       c.clipRadius,
	}
}

// DrawImage 以 Canvas 当前原点为坐标系，绕 (cx, cy) 旋转和缩放整张图片。
func (c *Canvas) DrawImage(img DecodedImage, degrees, scaleX, scaleY, cx, cy float64) {
	c.renderer.DrawImage(
		canvas.Image{
			Pixels: img.Pixels,
			Width:  img.Width,
			Height: img.Height,
			Opaque: img.Opaque,
		},
		image.Rect(0, 0, img.Width, img.Height),
		degrees, scaleX, scaleY, float64(c.x)+cx, float64(c.y)+cy,
		c.clipBounds(), c.roundedClip, c.clipRadius,
	)
}

// DrawMask 使用 mask 的 Alpha 覆盖率在当前局部坐标绘制纯色图形。
// Canvas 负责坐标转换和裁剪；renderer 只接收连续、从原点开始的 mask 数据。
func (c *Canvas) DrawMask(mask *image.Alpha, x, y int, color Color) {
	if mask == nil || mask.Rect.Empty() || color.IsNone() {
		return
	}

	width, height := mask.Rect.Dx(), mask.Rect.Dy()
	offset := mask.PixOffset(mask.Rect.Min.X, mask.Rect.Min.Y)
	pixels := mask.Pix[offset:]
	if mask.Stride != width {
		packed := make([]byte, width*height)
		for row := range height {
			copy(packed[row*width:(row+1)*width], pixels[row*mask.Stride:row*mask.Stride+width])
		}
		pixels = packed
	} else {
		pixels = pixels[:width*height]
	}

	c.renderer.DrawMask(
		pixels, width, height,
		image.Pt(c.x+x, c.y+y),
		c.clipBounds(),
		canvas.Color(color),
	)
}

// 供测试用。
func (c *Canvas) testGetPixel(x, y int) color.NRGBA {
	xx, yy := c.x+x, c.y+y

	tr, ok := c.renderer.(canvas.TestRenderer)
	if !ok {
		panic(`渲染器未实现获取像素`)
	}

	return tr.Pixel(image.Pt(xx, yy))
}

// 供测试用。
/*
func (c *Canvas) setPixel(x, y int, cr color.NRGBA) {
	if yy := c.y + y; yy < 0 || yy >= c.height {
		return
	}
	if xx := c.x + x; xx < 0 || xx >= c.width {
		return
	}

	tr, ok := c.renderer.(canvas.TestRenderer)
	if !ok {
		panic(`渲染器未实现设置像素`)
	}

	tr.SetPixel(image.Pt(c.x+x, c.y+y), cr)
}
*/

func (c *Canvas) FillRect(x, y, width, height int, color Color) {
	if width <= 0 || height <= 0 {
		return
	}

	x0 := c.x + x
	y0 := c.y + y
	x1 := x0 + width
	y1 := y0 + height
	clip := c.clipBounds()

	if x0 < clip.Min.X {
		x0 = clip.Min.X
	}
	if x0 >= clip.Max.X {
		return
	}
	if y0 < clip.Min.Y {
		y0 = clip.Min.Y
	}
	if y0 >= clip.Max.Y {
		return
	}
	if x1 > clip.Max.X {
		x1 = clip.Max.X
	}
	if y1 > clip.Max.Y {
		y1 = clip.Max.Y
	}

	c.renderer.FillRect(image.Rect(x0, y0, x1, y1), clip, Color(color))
}

// DrawRect 绘制带可选填充、边框和圆角的矩形。
func (c *Canvas) DrawRect(x, y, width, height, radius, borderWidth int, fill, border Color) {
	if width <= 0 || height <= 0 {
		return
	}
	radius = min(max(radius, 0), min(width, height)/2)
	borderWidth = min(max(borderWidth, 0), min(width, height)/2)
	if radius == 0 {
		if !fill.IsNone() {
			c.FillRect(x, y, width, height, fill)
		}
		if borderWidth > 0 && !border.IsNone() {
			c.FillRect(x, y, width, borderWidth, border)
			c.FillRect(x, y+height-borderWidth, width, borderWidth, border)
			c.FillRect(x, y+borderWidth, borderWidth, height-borderWidth*2, border)
			c.FillRect(x+width-borderWidth, y+borderWidth, borderWidth, height-borderWidth*2, border)
		}
		return
	}
	outer := c.roundedRectMask(width, height, radius, 0)
	if !fill.IsNone() {
		c.DrawMask(outer, x, y, fill)
	}
	if borderWidth > 0 && !border.IsNone() {
		key := _RoundedRectMaskKey{width, height, radius, -borderWidth}
		ring := c.roundedRectMasks[key]
		if ring == nil {
			inner := c.roundedRectMask(width, height, max(0, radius-borderWidth), borderWidth)
			ring = image.NewAlpha(outer.Rect)
			for i := range ring.Pix {
				ring.Pix[i] = outer.Pix[i] - min(outer.Pix[i], inner.Pix[i])
			}
			c.roundedRectMasks[key] = ring
		}
		c.DrawMask(ring, x, y, border)
	}
}

func (c *Canvas) roundedRectMask(width, height, radius, inset int) *image.Alpha {
	if c.roundedRectMasks == nil {
		c.roundedRectMasks = make(map[_RoundedRectMaskKey]*image.Alpha)
	}
	key := _RoundedRectMaskKey{width, height, radius, inset}
	if mask := c.roundedRectMasks[key]; mask != nil {
		return mask
	}
	if len(c.roundedRectMasks) >= 256 {
		clear(c.roundedRectMasks)
	}
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	if radius == 0 {
		for y := inset; y < height-inset; y++ {
			for x := inset; x < width-inset; x++ {
				mask.Pix[y*mask.Stride+x] = 255
			}
		}
		c.roundedRectMasks[key] = mask
		return mask
	}
	left, top := float64(inset), float64(inset)
	right, bottom := float64(width-inset), float64(height-inset)
	r := min(float64(radius), min(right-left, bottom-top)/2)
	for y := range height {
		for x := range width {
			pixelLeft, pixelTop := float64(x), float64(y)
			pixelRight, pixelBottom := pixelLeft+1, pixelTop+1
			if pixelLeft >= left && pixelRight <= right && pixelTop >= top && pixelBottom <= bottom &&
				((pixelLeft >= left+r && pixelRight <= right-r) ||
					(pixelTop >= top+r && pixelBottom <= bottom-r)) {
				mask.Pix[y*mask.Stride+x] = 255
				continue
			}
			covered := 0
			for sy := range 4 {
				for sx := range 4 {
					px, py := float64(x)+(float64(sx)+.5)/4, float64(y)+(float64(sy)+.5)/4
					qx := max(math.Abs(px-(left+right)/2)-(right-left)/2+r, 0)
					qy := max(math.Abs(py-(top+bottom)/2)-(bottom-top)/2+r, 0)
					if px >= left && px < right && py >= top && py < bottom && qx*qx+qy*qy <= r*r {
						covered++
					}
				}
			}
			mask.Pix[y*mask.Stride+x] = uint8(covered * 255 / 16)
		}
	}
	c.roundedRectMasks[key] = mask
	return mask
}

// 清屏。
// 暂时是简单用黑色清。
func (c *Canvas) Clear() {
	c.renderer.Clear()
}

// 画字符串，以指定的字体、指定的颜色、于当前位置。
//
// 超出 framebuffer 的像素会被裁剪。
func (c *Canvas) DrawString(text string, faces []*FontFace, color Color) {
	if color == canvas.ColorNone {
		return
	}
	canvas.DrawText(
		c.renderer, text, faces, image.Pt(c.x, c.y), c.clipBounds(), color,
	)
}

type _ImageCacheKey struct {
	fsys    fs.FS
	path    string
	options ImageDecodeOptions
}

// 用标准库的 draw.Draw 造成了极多不必要的计算，
// 而目标屏幕的内存格式是确定的（B、G、R、A），都不是 [image.RGBA] 或
// [image.NRGBA] 的格式（它们是 R、G、B、A），每次渲染的时候都转换实在没有意义。
// 所以这里直接在内存中保存目标格式，加快渲染效率。
type DecodedImage struct {
	Pixels        []byte // 内存格式：B G R A，长度：width*height*4
	Width, Height int    // 如果指定了边缘裁剪，则保存裁剪后的大小。
	Opaque        bool   // 整张图片的 Alpha 是否全部为 255；用于选择直接复制路径。
}

// ImageDecodeOptions 控制图片解码时执行的变换。
type ImageDecodeOptions struct {
	// TrimTransparentBorder 移除图片四周全部像素均为全透明的行和列。
	// 全透明图片会保留为一个透明像素。
	// 先移除再计算大小。
	TrimTransparentBorder bool

	// TrimBlackBorder 移除图片四周全部像素均为不透明纯黑的行和列。
	// 全黑图片会保留一个黑色像素。
	TrimBlackBorder bool
}

type ImageManager struct {
	contentCache *helpers.TTLCache[_ImageCacheKey, DecodedImage]
	gifCache     *helpers.TTLCache[_ImageCacheKey, *_AnimatedGIF]

	// 图片加载可能被异步调用。
	closed atomic.Bool
}

func NewImageManager() *ImageManager {
	return &ImageManager{
		contentCache: helpers.NewTTLCache[_ImageCacheKey, DecodedImage](128, 10*time.Minute, time.Minute),
		gifCache:     helpers.NewTTLCache[_ImageCacheKey, *_AnimatedGIF](16, 10*time.Minute, time.Minute),
	}
}

// 不需要清空两个cache，因为刚刚启动的 goroutine 可能仍然在访问。
// 原子锁没有阻止此行为。
func (m *ImageManager) Close() {
	m.closed.Store(true)
}

func (m *ImageManager) decodeImage(fsys fs.FS, path string, options ImageDecodeOptions) (DecodedImage, error) {
	if m.closed.Load() {
		return DecodedImage{}, fs.ErrClosed
	}

	log.Println(`重新解码：`, path)

	fp, err := fsys.Open(path)
	if err != nil {
		log.Println(err, path)
		return DecodedImage{}, err
	}
	defer fp.Close()

	img, _, err := image.Decode(fp)
	if err != nil {
		log.Println(`图片解码错误`, err, path)
		return DecodedImage{}, err
	}
	if options.TrimTransparentBorder || options.TrimBlackBorder {
		img = trimImageBorder(img, func(r, g, b, a uint32) bool {
			return options.TrimTransparentBorder && a == 0 ||
				options.TrimBlackBorder && r == 0 && g == 0 && b == 0 && a == 0xffff
		}, Iif(options.TrimTransparentBorder, color.NRGBA{}, color.NRGBA{A: 255}))
	}

	return decodedPixels(img), nil
}

func trimImageBorder(img image.Image, removable func(r, g, b, a uint32) bool, empty color.NRGBA) image.Image {
	bounds := img.Bounds()
	content := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if removable(r, g, b, a) {
				continue
			}
			content.Min.X = min(content.Min.X, x)
			content.Min.Y = min(content.Min.Y, y)
			content.Max.X = max(content.Max.X, x+1)
			content.Max.Y = max(content.Max.Y, y+1)
		}
	}

	if content.Empty() {
		trimmed := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		trimmed.SetNRGBA(0, 0, empty)
		return trimmed
	}
	if content == bounds {
		return img
	}

	trimmed := image.NewNRGBA(image.Rect(0, 0, content.Dx(), content.Dy()))
	draw.Draw(trimmed, trimmed.Bounds(), img, content.Min, draw.Src)
	return trimmed
}

func (m *ImageManager) Load(fsys fs.FS, path string, checking bool, options ImageDecodeOptions) (DecodedImage, error) {
	if m.closed.Load() {
		return DecodedImage{}, fs.ErrClosed
	}
	key := _ImageCacheKey{
		fsys:    fsys,
		path:    path,
		options: options,
	}
	if checking {
		img, found := m.contentCache.Get(key)
		if found {
			return img, nil
		}
		return img, os.ErrNotExist
	}
	return m.contentCache.GetOrLoad(key,
		func() (DecodedImage, error) {
			return m.decodeImage(fsys, path, options)
		},
	)
}

type _AnimatedGIF struct {
	frames []DecodedImage
	delays []time.Duration
}

func decodeGIF(fsys fs.FS, path string) (*_AnimatedGIF, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return nil, err
	}
	if len(g.Image) == 0 {
		return nil, fmt.Errorf("GIF has no frames: %s", path)
	}
	result := &_AnimatedGIF{
		frames: make([]DecodedImage, 0, len(g.Image)),
		delays: make([]time.Duration, 0, len(g.Image)),
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	for i, frame := range g.Image {
		var previous *image.NRGBA
		if i < len(g.Disposal) && g.Disposal[i] == gif.DisposalPrevious {
			previous = image.NewNRGBA(canvas.Bounds())
			copy(previous.Pix, canvas.Pix)
		}
		drawGIFFrame(canvas, frame)
		result.frames = append(result.frames, decodedPixels(canvas))
		delay := time.Duration(g.Delay[i]) * 10 * time.Millisecond
		if delay <= 0 {
			delay = 10 * time.Millisecond
		}
		result.delays = append(result.delays, delay)
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				if previous != nil {
					canvas = previous
				}
			}
		}
	}
	return result, nil
}

// GIF 帧和画布使用相同坐标。直接读取常见帧格式的像素，避免
// draw.Draw 在 NRGBA 目标上逐像素调用 At 和颜色转换。
func drawGIFFrame(dst *image.NRGBA, src image.Image) {
	r := src.Bounds().Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	var palette []color.NRGBA
	switch frame := src.(type) {
	case *image.Paletted:
		palette = make([]color.NRGBA, len(frame.Palette))
		for i, c := range frame.Palette {
			palette[i] = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			d, s := dst.PixOffset(r.Min.X, y), frame.PixOffset(r.Min.X, y)
			for x := r.Min.X; x < r.Max.X; x++ {
				c := palette[frame.Pix[s]]
				blendNRGBA(dst.Pix[d:d+4], c.R, c.G, c.B, c.A)
				d, s = d+4, s+1
			}
		}
	case *image.NRGBA:
		for y := r.Min.Y; y < r.Max.Y; y++ {
			d, s := dst.PixOffset(r.Min.X, y), frame.PixOffset(r.Min.X, y)
			for x := r.Min.X; x < r.Max.X; x++ {
				blendNRGBA(dst.Pix[d:d+4], frame.Pix[s], frame.Pix[s+1], frame.Pix[s+2], frame.Pix[s+3])
				d, s = d+4, s+4
			}
		}
	default:
		draw.Draw(dst, r, src, r.Min, draw.Over)
	}
}

func blendNRGBA(dst []byte, red, green, blue, alpha uint8) {
	if alpha == 0 {
		return
	}
	if alpha == 255 || dst[3] == 0 {
		dst[0], dst[1], dst[2], dst[3] = red, green, blue, alpha
		return
	}
	sa, da := uint32(alpha), uint32(dst[3])
	covered := (da*(255-sa) + 127) / 255
	outA := sa + covered
	dst[0] = uint8((uint32(red)*sa + uint32(dst[0])*covered + outA/2) / outA)
	dst[1] = uint8((uint32(green)*sa + uint32(dst[1])*covered + outA/2) / outA)
	dst[2] = uint8((uint32(blue)*sa + uint32(dst[2])*covered + outA/2) / outA)
	dst[3] = uint8(outA)
}

func decodedPixels(img image.Image) DecodedImage {
	b := img.Bounds()

	decoded := DecodedImage{
		Width:  b.Dx(),
		Height: b.Dy(),
		Pixels: make([]byte, b.Dx()*b.Dy()*4),
		Opaque: true,
	}

	var pixels []byte
	var stride int

	switch m := img.(type) {
	case *image.RGBA:
		pixels = m.Pix
		stride = m.Stride
	case *image.NRGBA:
		pixels = m.Pix
		stride = m.Stride
	}

	// fast-path
	if len(pixels) > 0 {
		for y := range decoded.Height {
			s := pixels[y*stride:]
			for x := range decoded.Width {
				offset := (y*decoded.Width + x) * 4
				d := decoded.Pixels[offset : offset+4]
				d[0] = s[2+x*4]
				d[1] = s[1+x*4]
				d[2] = s[0+x*4]
				d[3] = s[3+x*4]
				if d[3] != 255 {
					decoded.Opaque = false
				}
			}
		}
		return decoded
	}

	// better-slow than never 🥵
	minY, maxY := img.Bounds().Min.Y, img.Bounds().Max.Y
	minX, maxX := img.Bounds().Min.X, img.Bounds().Max.X
	for y0, y1 := minY, maxY; y0 < y1; y0++ {
		for x0, x1 := minX, maxX; x0 < x1; x0++ {
			c := color.NRGBAModel.Convert(img.At(x0, y0)).(color.NRGBA)
			offset := ((y0-minY)*decoded.Width + x0 - minX) * 4
			d := decoded.Pixels[offset : offset+4]
			cr := ColorFromRGBA(c.R, c.G, c.B, c.A)
			*(*uint32)(unsafe.Pointer(&d[0])) = cr.Value()
			if cr.A() != 255 {
				decoded.Opaque = false
			}
		}
	}

	return decoded
}

func (m *ImageManager) LoadGIF(fsys fs.FS, path string, checking bool) (*_AnimatedGIF, error) {
	if m.closed.Load() {
		return nil, fs.ErrClosed
	}
	key := _ImageCacheKey{fsys: fsys, path: path}
	if checking {
		if value, found := m.gifCache.Get(key); found {
			return value, nil
		}
		return nil, os.ErrNotExist
	}
	return m.gifCache.GetOrLoad(key, func() (*_AnimatedGIF, error) {
		return decodeGIF(fsys, path)
	})
}

type FontManager struct {
	fonts map[_FontKey]*_FontValue
	faces map[_FontFaceKey]*FontFace
}

func NewFontManager() *FontManager {
	return &FontManager{
		fonts: map[_FontKey]*_FontValue{},
		faces: map[_FontFaceKey]*FontFace{},
	}
}

func (fm *FontManager) Close() {
	for _, f := range fm.fonts {
		if ff := f.File; ff != nil {
			ff.Close()
		}
	}
	clear(fm.fonts)
}

type _FontKey struct {
	Family string
	Bold   bool
	Italic bool
}
type _FontValue struct {
	Font *opentype.Font

	// 从二进制加载的字体没有此字段。
	File io.ReadCloser
}

type FontKey = _FontKey

type _FontFaceKey struct {
	Family string
	Size   int
	Bold   bool
	Italic bool
}

// 添加字体族。
//
// 为了降低内存使用，不会完整读取字体文件，使用过程中按需读取。
// 所以字体文件在加载后会被一直引用。
//
// family 可以重复，只要其它样式不一样就行。
//
// fsys.Open的文件必须支持 io.ReaderAt。os.DirFS和embed.FS 均支持。
func (fm *FontManager) AddFontFile(fsys fs.FS, path string, family string, bold, italic bool) error {
	key := _FontKey{
		Family: family,
		Bold:   bold,
		Italic: italic,
	}
	if _, ok := fm.fonts[key]; ok {
		return nil
	}

	fp, err := fsys.Open(path)
	if err != nil {
		return err
	}

	// 先完整读内存，如果占用高，可以考虑转 ParseReader，
	// 但是那样可以会每个字符读文件？不知道速度怎样。
	parsedFont, err := opentype.ParseReaderAt(fp.(io.ReaderAt))
	if err != nil {
		return err
	}

	fm.fonts[key] = &_FontValue{
		File: fp,
		Font: parsedFont,
	}

	return nil
}

// 从二进制数据添加字体。
// data 会被一直持有。
func (fm *FontManager) AddFontData(data []byte, family string, bold, italic bool) error {
	key := _FontKey{
		Family: family,
		Bold:   bold,
		Italic: italic,
	}
	if _, ok := fm.fonts[key]; ok {
		return nil
	}

	parsedFont, err := opentype.Parse(data)
	if err != nil {
		return err
	}

	fm.fonts[key] = &_FontValue{
		Font: parsedFont,
	}

	return nil
}

// 返回系统字体。
//
// 如果有样式的系统字体找不到，会返回非粗体、非斜体版本。
// 如果还是找不到，就直接崩溃。
func (fm *FontManager) GetSystemFace(size int, bold bool, italic bool) *FontFace {
	system, err := fm.GetFace(`system`, size, bold, italic)
	if err == nil {
		return system
	}
	// 怎么连对应形状的系统字体也找不到？
	system, err = fm.GetFace(`system`, size, false, false)
	if err == nil {
		return system
	}
	panic(`没有任何可用的系统字体，没救了。`)
}

// 返回指定名字的字体。
func (fm *FontManager) GetFace(family string, size int, bold bool, italic bool) (*FontFace, error) {
	faceKey := _FontFaceKey{
		Family: family,
		Size:   size,
		Bold:   bold,
		Italic: italic,
	}
	if face, ok := fm.faces[faceKey]; ok {
		return face, nil
	}

	fontKey := _FontKey{
		Family: family,
		Bold:   bold,
		Italic: italic,
	}
	fontValue, ok := fm.fonts[fontKey]
	if !ok {
		return nil, fmt.Errorf(`字体家族未找到：%v`, fontKey)
	}

	theFace, err := opentype.NewFace(fontValue.Font, &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72, // 为72时1点=1像素
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf(`无法创建字体样式：%w`, err)
	}

	fontFace := canvas.NewFontFace(family, theFace)

	fm.faces[faceKey] = fontFace

	return fontFace, nil
}

type (
	FontFace   = canvas.FontFace
	GlyphValue = canvas.GlyphValue
)

var (
	SegmentText = canvas.SegmentText
)
