package fbiw

import (
	"context"
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
	"github.com/movsb/fbiw/internal/ports"
	"github.com/phuslu/lru"
	xdraw "golang.org/x/image/draw"
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
	renderer Renderer

	// 渲染的偏移坐标。
	x, y int

	// buffer 的宽度和高度。
	width, height int

	// framebuffer 坐标系中的可绘制区域。
	clip image.Rectangle
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
		renderer: renderer,
		width:    width,
		height:   height,
		x:        0,
		y:        0,
		clip:     image.Rect(0, 0, width, height),
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
		renderer: c.renderer,
		x:        c.x + x,
		y:        c.y + y,
		width:    c.width,
		height:   c.height,
		clip:     c.clip,
	}
}

func (c *Canvas) DrawImage(img DecodedImage) {
	c.DrawImageRegion(img, 0, 0, img.Width, img.Height)
}

// DrawImageRotated 绕图片中心顺时针旋转 degrees 度并绘制。
// 使用当前原点和裁剪范围；零角度复用 DrawImage。
func (c *Canvas) DrawImageRotated(img DecodedImage, degrees float64) {
	if math.IsNaN(degrees) || math.IsInf(degrees, 0) {
		panic("DrawImageRotated: 无效的角度。")
	}
	degrees = math.Mod(degrees, 360)
	if degrees == 0 {
		c.DrawImage(img)
		return
	}
	if img.Width <= 0 || img.Height <= 0 {
		return
	}
	c.drawImageRotatedCenter(img, degrees, float64(c.x)+float64(img.Width)/2, float64(c.y)+float64(img.Height)/2)
}

func (c *Canvas) drawImageRotatedCenter(img DecodedImage, degrees, cx, cy float64) {
	c.drawImageTransformedCenter(img, degrees, 1, cx, cy)
}

func (c *Canvas) drawImageTransformedCenter(img DecodedImage, degrees, scale, cx, cy float64) {
	c.renderer.DrawImageTransformed(toCanvasImage(img), degrees, scale, cx, cy, c.clipBounds())
}

// DrawImageRegion 把图片的指定区域绘制到 Canvas 当前原点。
func (c *Canvas) DrawImageRegion(img DecodedImage, srcX, srcY, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	c.renderer.DrawImage(
		toCanvasImage(img),
		image.Rect(srcX, srcY, srcX+width, srcY+height),
		image.Pt(c.x, c.y),
		c.clipBounds(),
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
func (c *Canvas) getPixel(x, y int) color.NRGBA {
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

// 清屏。
// 暂时是简单用黑色清。
func (c *Canvas) Clear() {
	c.renderer.Clear()
}

// TODO 去掉。换成画矩形。
func (c *Canvas) DrawBorder(cr Color, w, h int, borderWidth int) {
	c.FillRect(0, 0, w, borderWidth, cr)
	c.FillRect(0, h-borderWidth, w, borderWidth, cr)
	c.FillRect(0, borderWidth, borderWidth, h-borderWidth*2, cr)
	c.FillRect(w-borderWidth, borderWidth, borderWidth, h-borderWidth*2, cr)
}

// 画字符串，以指定的字体、指定的颜色、于当前位置。
//
// 超出 framebuffer 的像素会被裁剪。
func (c *Canvas) DrawString(text string, faces []*FontFace, color Color) {
	if color == canvas.ColorNone {
		return
	}
	c.drawStringDevice(text, faces, color)
}

// 按设备要求直接写显存。
//
// 和 fillAlphaBlend 系列一样保留各个版本，方便在实际设备上持续比较。
// 正常绘制始终调用当前最快的版本。
func (c *Canvas) drawStringDevice(text string, faces []*FontFace, color Color) {
	c.drawStringDevice2(text, faces, color)
}

// 版本 2：先裁剪整个字形、缓存行和颜色通道，并使用精确的快速除法混色。
//
// 字形缓存中保存的是每个像素的覆盖率（Alpha mask）。这里直接把覆盖率
// 与目标颜色、显存中原有的 BGRA 像素混合，避免经过 image/draw 的通用
// Color 接口和颜色模型转换。这个函数处于每帧绘制的热路径，内层循环应当
// 尽量只保留读取 mask、混色和写回三个步骤。
func (c *Canvas) drawStringDevice2(text string, faces []*FontFace, color Color) {
	canvas.DrawText(
		c.renderer, text, faces, image.Pt(c.x, c.y), c.clipBounds(), color,
	)
}

type _ImageCacheKey struct {
	fsys          fs.FS
	path          string
	width, height int
	options       ImageDecodeOptions
}

// 用标准库的 draw.Draw 造成了极多不必要的计算，
// 而目标屏幕的内存格式是确定的（B、G、R、A），都不是 [image.RGBA] 或
// [image.NRGBA] 的格式（它们是 R、G、B、A），每次渲染的时候都转换实在没有意义。
// 所以这里直接在内存中保存目标格式，加快渲染效率。
type DecodedImage struct {
	Pixels        []byte // 内存格式：B G R A，长度：width*height*4
	Width, Height int    // 如果指定了移除透明像素，则保存的是移除后的大小。
	Opaque        bool   // 整张图片的 Alpha 是否全部为 255；用于选择直接复制路径。
}

func toCanvasImage(img DecodedImage) canvas.Image {
	return canvas.Image{
		Pixels: img.Pixels,
		Width:  img.Width, Height: img.Height,
		Opaque: img.Opaque,
	}
}

// ImageDecodeOptions 控制图片解码时执行的变换。
type ImageDecodeOptions struct {
	// TrimTransparentBorder 移除图片四周全部像素均为全透明的行和列。
	// 全透明图片会保留为一个透明像素。
	// 先移除再计算大小。
	TrimTransparentBorder bool
}

type ImageManager struct {
	contentCache *lru.TTLCache[_ImageCacheKey, DecodedImage]
	gifCache     *lru.TTLCache[_ImageCacheKey, *_AnimatedGIF]

	// 图片加载可能被异步调用。
	closed atomic.Bool
}

func NewImageManager() *ImageManager {
	return &ImageManager{
		// https://github.com/phuslu/lru/issues/32
		contentCache: lru.NewTTLCache(1024, lru.WithShards[_ImageCacheKey, DecodedImage](1)),
		gifCache:     lru.NewTTLCache(16, lru.WithShards[_ImageCacheKey, *_AnimatedGIF](1)),
	}
}

// 不需要清空两个cache，因为刚刚启动的 goroutine 可能仍然在访问。
// 原子锁没有阻止此行为。
func (m *ImageManager) Close() {
	m.closed.Store(true)
}

// 如果 width和height均为0，返回原图大小。
// 否则表示指定缩放到此大小。
func (m *ImageManager) decodeImage(fsys fs.FS, path string, wantWidth, wantHeight int, options ImageDecodeOptions) (DecodedImage, error) {
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
	if options.TrimTransparentBorder {
		img = trimTransparentBorder(img)
	}

	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if wantWidth != 0 && wantHeight != 0 && (wantWidth != width || wantHeight != height) {
		now := time.Now()
		resized := image.NewNRGBA(image.Rect(0, 0, wantWidth, wantHeight))
		xdraw.CatmullRom.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Src, nil)
		img = resized
		log.Printf(`缩放图片: %s (%dx%d)->(%dx%d) %v`,
			path, width, height, wantWidth, wantHeight, time.Since(now).Round(time.Millisecond*100))
		width = wantWidth
		height = wantHeight
	}

	return decodedPixels(img), nil
}

func trimTransparentBorder(img image.Image) image.Image {
	bounds := img.Bounds()
	content := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha == 0 {
				continue
			}
			content.Min.X = min(content.Min.X, x)
			content.Min.Y = min(content.Min.Y, y)
			content.Max.X = max(content.Max.X, x+1)
			content.Max.Y = max(content.Max.Y, y+1)
		}
	}

	if content.Empty() {
		return image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	if content == bounds {
		return img
	}

	trimmed := image.NewNRGBA(image.Rect(0, 0, content.Dx(), content.Dy()))
	draw.Draw(trimmed, trimmed.Bounds(), img, content.Min, draw.Src)
	return trimmed
}

// 多线程安全。
func (m *ImageManager) GetImageCached(fsys fs.FS, path string, options ImageDecodeOptions) (DecodedImage, error) {
	return m._getImageCached(fsys, path, 0, 0, false, options)
}

// 多线程安全。
func (m *ImageManager) GetImageScaledCached(fsys fs.FS, path string, width, height int, checking bool, options ImageDecodeOptions) (DecodedImage, error) {
	return m._getImageCached(fsys, path, width, height, checking, options)
}

func (m *ImageManager) _getImageCached(fsys fs.FS, path string, width, height int, checking bool, options ImageDecodeOptions) (DecodedImage, error) {
	if m.closed.Load() {
		return DecodedImage{}, fs.ErrClosed
	}
	key := _ImageCacheKey{
		fsys:    fsys,
		path:    path,
		width:   width,
		height:  height,
		options: options,
	}
	if checking {
		img, found := m.contentCache.Get(key)
		if found {
			return img, nil
		}
		return img, os.ErrNotExist
	}
	img, err, _ := m.contentCache.GetOrLoad(context.Background(), key,
		func(ctx context.Context, _ _ImageCacheKey) (DecodedImage, time.Duration, error) {
			decoded, err := m.decodeImage(fsys, path, width, height, options)
			return decoded, time.Minute * 10, err
		},
	)

	// 如果没指定尺寸，则应该用实际的尺寸也缓存一份。
	if width == 0 && height == 0 && err == nil {
		k := key
		k.width = img.Width
		k.height = img.Height
		m.contentCache.SetIfAbsent(k, img, time.Minute*10)
	}

	return img, err
}

type _AnimatedGIF struct {
	frames []DecodedImage
	delays []time.Duration
}

func decodeGIF(fsys fs.FS, path string, width, height int) (*_AnimatedGIF, error) {
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
	if width == 0 || height == 0 {
		width, height = g.Config.Width, g.Config.Height
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
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		resized := image.Image(canvas)
		if width != g.Config.Width || height != g.Config.Height {
			dst := image.NewNRGBA(image.Rect(0, 0, width, height))
			xdraw.CatmullRom.Scale(dst, dst.Bounds(), canvas, canvas.Bounds(), draw.Src, nil)
			resized = dst
		}
		result.frames = append(result.frames, decodedPixels(resized))
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

func (m *ImageManager) getGIF(fsys fs.FS, path string, width, height int, checking bool) (*_AnimatedGIF, error) {
	if m.closed.Load() {
		return nil, fs.ErrClosed
	}
	key := _ImageCacheKey{fsys: fsys, path: path, width: width, height: height}
	if checking {
		if value, found := m.gifCache.Get(key); found {
			return value, nil
		}
		return nil, os.ErrNotExist
	}
	value, err, _ := m.gifCache.GetOrLoad(context.Background(), key, func(context.Context, _ImageCacheKey) (*_AnimatedGIF, time.Duration, error) {
		v, e := decodeGIF(fsys, path, width, height)
		return v, 10 * time.Minute, e
	})
	if width == 0 && height == 0 && err == nil {
		actual := key
		actual.width, actual.height = value.frames[0].Width, value.frames[0].Height
		m.gifCache.SetIfAbsent(actual, value, 10*time.Minute)
	}
	return value, err
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
