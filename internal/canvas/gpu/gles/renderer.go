package gles

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"
	"unsafe"

	"github.com/movsb/fbiw/internal/canvas"
)

const (
	glColorBufferBit     = 0x00004000
	glScissorTest        = 0x0c11
	glBlend              = 0x0be2
	glArrayBuffer        = 0x8892
	glStaticDraw         = 0x88e4
	glDynamicDraw        = 0x88e8
	glFloat              = 0x1406
	glTriangleStrip      = 0x0005
	glTriangles          = 0x0004
	glVertexShader       = 0x8b31
	glFragmentShader     = 0x8b30
	glCompileStatus      = 0x8b81
	glLinkStatus         = 0x8b82
	glInfoLogLength      = 0x8b84
	glConstantAlpha      = 0x8003
	glOneMinusConstAlpha = 0x8004
	glOne                = 1
	glZero               = 0
	glNoError            = 0
	glTexture2D          = 0x0de1
	glTextureMinFilter   = 0x2801
	glTextureMagFilter   = 0x2800
	glTextureWrapS       = 0x2802
	glTextureWrapT       = 0x2803
	glNearest            = 0x2600
	glClampToEdge        = 0x812f
	glRGBA               = 0x1908
	glUnsignedByte       = 0x1401
	glSrcAlpha           = 0x0302
	glOneMinusSrcAlpha   = 0x0303
	glAlpha              = 0x1906
	glUnpackAlignment    = 0x0cf5
)

type api struct {
	clearColor          func(float32, float32, float32, float32)
	clear               func(uint32)
	enable              func(uint32)
	disable             func(uint32)
	scissor             func(int32, int32, int32, int32)
	viewport            func(int32, int32, int32, int32)
	createShader        func(uint32) uint32
	shaderSource        func(uint32, int32, *uintptr, *int32)
	compileShader       func(uint32)
	getShaderiv         func(uint32, uint32, *int32)
	getShaderInfoLog    func(uint32, int32, *int32, *byte)
	deleteShader        func(uint32)
	createProgram       func() uint32
	attachShader        func(uint32, uint32)
	linkProgram         func(uint32)
	getProgramiv        func(uint32, uint32, *int32)
	getProgramInfoLog   func(uint32, int32, *int32, *byte)
	deleteProgram       func(uint32)
	useProgram          func(uint32)
	getAttribLocation   func(uint32, *byte) int32
	getUniformLocation  func(uint32, *byte) int32
	uniform2f           func(int32, float32, float32)
	uniform4f           func(int32, float32, float32, float32, float32)
	genBuffers          func(int32, *uint32)
	deleteBuffers       func(int32, *uint32)
	bindBuffer          func(uint32, uint32)
	bufferData          func(uint32, uintptr, uintptr, uint32)
	enableVertexAttrib  func(uint32)
	vertexAttribPointer func(uint32, int32, uint32, uint8, int32, uintptr)
	drawArrays          func(uint32, int32, int32)
	blendColor          func(float32, float32, float32, float32)
	blendFuncSeparate   func(uint32, uint32, uint32, uint32)
	glGetError          func() uint32
	uniform1i           func(int32, int32)
	genTextures         func(int32, *uint32)
	deleteTextures      func(int32, *uint32)
	bindTexture         func(uint32, uint32)
	texParameteri       func(uint32, uint32, int32)
	texImage2D          func(uint32, int32, int32, int32, int32, int32, uint32, uint32, uintptr)
	texSubImage2D       func(uint32, int32, int32, int32, int32, int32, uint32, uint32, uintptr)
	colorMask           func(uint8, uint8, uint8, uint8)
	pixelStorei         func(uint32, int32)
	readPixels          func(int32, int32, int32, int32, uint32, uint32, uintptr)
}

type Renderer struct {
	api                *api
	width, height      int
	swapBuffers        func() error
	closePlatform      func()
	platformInfo       string
	closed             bool
	colorProgram       uint32
	quadBuffer         uint32
	positionLocation   int32
	rectLocation       int32
	viewportLocation   int32
	colorLocation      int32
	textureProgram     uint32
	texturePosition    int32
	textureRect        int32
	textureViewport    int32
	textureUVRect      int32
	textureSampler     int32
	textureAlphaMode   int32
	maskProgram        uint32
	maskPosition       int32
	maskUVPosition     int32
	maskViewport       int32
	maskSampler        int32
	maskColor          int32
	maskAlphaMode      int32
	maskBuffer         uint32
	maskGlyphs         map[maskCacheKey]maskGlyph
	maskAtlases        []maskAtlas
	maskBatch          maskBatch
	imageTextures      map[imageCacheKey]imageTexture
	transformProgram   uint32
	transformPosition  int32
	transformRect      int32
	transformViewport  int32
	transformCenter    int32
	transformImageSize int32
	transformInverse   int32
	transformSampler   int32
	transformAlphaMode int32
}

type maskCacheKey struct {
	digest        [sha256.Size]byte
	width, height int
}

type maskGlyph struct {
	atlas               int
	x, y, width, height int
}

type maskAtlas struct {
	texture         uint32
	x, y, rowHeight int
}

type maskBatch struct {
	atlas    int
	color    canvas.Color
	vertices []float32
}

const (
	maskAtlasSize  = 1024
	maxMaskAtlases = 8
)

type imageCacheKey struct {
	pixels                uintptr
	length, width, height int
	opaque                bool
}

type imageTexture struct {
	id     uint32
	pixels []byte
}

const maxCachedImages = 256

func (r *Renderer) Size() (int, int) { return r.width, r.height }
func (*Renderer) BeginFrame()        {}
func (r *Renderer) EndFrame() {
	r.flushMasks()
	if err := r.swapBuffers(); err != nil {
		panic(err)
	}
}
func (r *Renderer) Clear() {
	r.flushMasks()
	r.api.disable(glScissorTest)
	r.api.disable(glBlend)
	r.api.clearColor(0, 0, 0, 0)
	r.api.clear(glColorBufferBit)
}
func (r *Renderer) FillRect(rect, clip image.Rectangle, fill canvas.Color) {
	r.flushMasks()
	rect = rect.Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if rect.Empty() {
		return
	}
	if fill.IsClear() {
		r.api.disable(glBlend)
		r.api.enable(glScissorTest)
		r.api.scissor(int32(rect.Min.X), int32(r.height-rect.Max.Y), int32(rect.Dx()), int32(rect.Dy()))
		r.api.clearColor(0, 0, 0, 0)
		r.api.clear(glColorBufferBit)
		return
	}
	r.api.disable(glScissorTest)
	if fill.A() == 255 || fill.A() == 0 {
		r.api.disable(glBlend)
	} else {
		a := float32(fill.A()) / 256
		r.api.enable(glBlend)
		r.api.blendColor(0, 0, 0, a)
		r.api.blendFuncSeparate(glConstantAlpha, glOneMinusConstAlpha, glOne, glZero)
	}
	r.api.useProgram(r.colorProgram)
	r.api.uniform4f(r.rectLocation, float32(rect.Min.X), float32(rect.Min.Y), float32(rect.Dx()), float32(rect.Dy()))
	outputAlpha := float32(1)
	if fill.A() == 0 {
		outputAlpha = 0
	}
	r.api.uniform4f(r.colorLocation, float32(fill.R())/255, float32(fill.G())/255, float32(fill.B())/255, outputAlpha)
	r.api.bindBuffer(glArrayBuffer, r.quadBuffer)
	r.api.enableVertexAttrib(uint32(r.positionLocation))
	r.api.vertexAttribPointer(uint32(r.positionLocation), 2, glFloat, 0, 0, 0)
	r.api.drawArrays(glTriangleStrip, 0, 4)
}
func (r *Renderer) DrawImage(img canvas.Image, src image.Rectangle, dst image.Point, clip image.Rectangle) {
	r.flushMasks()
	w, h := src.Dx(), src.Dy()
	sx, sy := src.Min.X, src.Min.Y
	if sx < 0 {
		dst.X -= sx
		w += sx
		sx = 0
	}
	if sy < 0 {
		dst.Y -= sy
		h += sy
		sy = 0
	}
	w, h = min(w, img.Width-sx), min(h, img.Height-sy)
	clip = clip.Intersect(image.Rect(0, 0, r.width, r.height))
	if dst.X < clip.Min.X {
		d := clip.Min.X - dst.X
		dst.X += d
		sx += d
		w -= d
	}
	if dst.Y < clip.Min.Y {
		d := clip.Min.Y - dst.Y
		dst.Y += d
		sy += d
		h -= d
	}
	w = min(w, clip.Max.X-dst.X)
	h = min(h, clip.Max.Y-dst.Y)
	if w <= 0 || h <= 0 || img.Width <= 0 || img.Height <= 0 || len(img.Pixels) < img.Width*img.Height*4 {
		return
	}

	texture := r.imageTexture(img)
	r.api.bindTexture(glTexture2D, texture)
	r.api.texParameteri(glTexture2D, glTextureMinFilter, glNearest)
	r.api.texParameteri(glTexture2D, glTextureMagFilter, glNearest)

	r.api.disable(glScissorTest)
	r.api.useProgram(r.textureProgram)
	r.api.uniform4f(r.textureRect, float32(dst.X), float32(dst.Y), float32(w), float32(h))
	r.api.uniform4f(r.textureUVRect, float32(sx)/float32(img.Width), float32(sy)/float32(img.Height), float32(w)/float32(img.Width), float32(h)/float32(img.Height))
	r.api.uniform1i(r.textureSampler, 0)
	r.api.bindBuffer(glArrayBuffer, r.quadBuffer)
	r.api.enableVertexAttrib(uint32(r.texturePosition))
	r.api.vertexAttribPointer(uint32(r.texturePosition), 2, glFloat, 0, 0, 0)
	if img.Opaque {
		r.api.disable(glBlend)
		r.api.uniform1i(r.textureAlphaMode, 0)
		r.api.drawArrays(glTriangleStrip, 0, 4)
	} else {
		// Match the CPU backend: fully transparent pixels preserve the target,
		// while every contributing source pixel makes target alpha opaque.
		r.api.colorMask(1, 1, 1, 0)
		r.api.enable(glBlend)
		r.api.blendFuncSeparate(glSrcAlpha, glOneMinusSrcAlpha, glOne, glZero)
		r.api.uniform1i(r.textureAlphaMode, 0)
		r.api.drawArrays(glTriangleStrip, 0, 4)
		r.api.colorMask(0, 0, 0, 1)
		r.api.disable(glBlend)
		r.api.uniform1i(r.textureAlphaMode, 1)
		r.api.drawArrays(glTriangleStrip, 0, 4)
		r.api.colorMask(1, 1, 1, 1)
		r.api.uniform1i(r.textureAlphaMode, 0)
	}
}

func (r *Renderer) imageTexture(img canvas.Image) uint32 {
	key := imageCacheKey{
		pixels: uintptr(unsafe.Pointer(&img.Pixels[0])),
		length: len(img.Pixels),
		width:  img.Width,
		height: img.Height,
		opaque: img.Opaque,
	}
	if texture, ok := r.imageTextures[key]; ok {
		return texture.id
	}
	if len(r.imageTextures) >= maxCachedImages {
		r.releaseImageTextures()
	}
	var texture uint32
	r.api.genTextures(1, &texture)
	if texture == 0 {
		panic("glGenTextures returned 0")
	}
	r.api.bindTexture(glTexture2D, texture)
	r.api.texParameteri(glTexture2D, glTextureMinFilter, glNearest)
	r.api.texParameteri(glTexture2D, glTextureMagFilter, glNearest)
	r.api.texParameteri(glTexture2D, glTextureWrapS, glClampToEdge)
	r.api.texParameteri(glTexture2D, glTextureWrapT, glClampToEdge)
	r.api.texImage2D(glTexture2D, 0, glRGBA, int32(img.Width), int32(img.Height), 0, glRGBA, glUnsignedByte, key.pixels)
	runtime.KeepAlive(img.Pixels)
	r.imageTextures[key] = imageTexture{id: texture, pixels: img.Pixels}
	return texture
}

func (r *Renderer) releaseImageTextures() {
	for key, texture := range r.imageTextures {
		id := texture.id
		r.api.deleteTextures(1, &id)
		delete(r.imageTextures, key)
	}
}
func (r *Renderer) DrawImageTransformed(img canvas.Image, degrees, scale, cx, cy float64, clip image.Rectangle) {
	r.flushMasks()
	if img.Width <= 0 || img.Height <= 0 || len(img.Pixels) < img.Width*img.Height*4 || scale <= 0 {
		return
	}
	sin, cos := math.Sincos(degrees * math.Pi / 180)
	rx := (math.Abs(cos)*float64(img.Width)+math.Abs(sin)*float64(img.Height))*scale/2 + scale
	ry := (math.Abs(sin)*float64(img.Width)+math.Abs(cos)*float64(img.Height))*scale/2 + scale
	visible := image.Rect(int(math.Floor(cx-rx)), int(math.Floor(cy-ry)), int(math.Ceil(cx+rx)), int(math.Ceil(cy+ry)))
	visible = visible.Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if visible.Empty() {
		return
	}
	sin, cos = sin/scale, cos/scale
	texture := r.imageTexture(img)
	r.api.bindTexture(glTexture2D, texture)
	r.api.texParameteri(glTexture2D, glTextureMinFilter, glNearest)
	r.api.texParameteri(glTexture2D, glTextureMagFilter, glNearest)
	r.api.disable(glScissorTest)
	r.api.useProgram(r.transformProgram)
	r.api.uniform4f(r.transformRect, float32(visible.Min.X), float32(visible.Min.Y), float32(visible.Dx()), float32(visible.Dy()))
	r.api.uniform2f(r.transformCenter, float32(cx), float32(cy))
	r.api.uniform2f(r.transformImageSize, float32(img.Width), float32(img.Height))
	r.api.uniform4f(r.transformInverse, float32(cos), float32(sin), float32(-sin), float32(cos))
	r.api.uniform1i(r.transformSampler, 0)
	r.api.bindBuffer(glArrayBuffer, r.quadBuffer)
	r.api.enableVertexAttrib(uint32(r.transformPosition))
	r.api.vertexAttribPointer(uint32(r.transformPosition), 2, glFloat, 0, 0, 0)

	r.api.colorMask(1, 1, 1, 0)
	r.api.enable(glBlend)
	r.api.blendFuncSeparate(glSrcAlpha, glOneMinusSrcAlpha, glOne, glZero)
	r.api.uniform1i(r.transformAlphaMode, 0)
	r.api.drawArrays(glTriangleStrip, 0, 4)
	r.api.colorMask(0, 0, 0, 1)
	r.api.disable(glBlend)
	r.api.uniform1i(r.transformAlphaMode, 1)
	r.api.drawArrays(glTriangleStrip, 0, 4)
	r.api.colorMask(1, 1, 1, 1)
	r.api.uniform1i(r.transformAlphaMode, 0)
}
func (r *Renderer) DrawMask(mask []byte, mw, mh int, dst image.Point, clip image.Rectangle, fill canvas.Color) {
	if mw <= 0 || mh <= 0 || len(mask) < mw*mh {
		return
	}
	visible := image.Rect(dst.X, dst.Y, dst.X+mw, dst.Y+mh).Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if visible.Empty() {
		return
	}
	sx, sy := visible.Min.X-dst.X, visible.Min.Y-dst.Y
	glyph := r.maskGlyph(mask[:mw*mh], mw, mh)
	if len(r.maskBatch.vertices) > 0 && (r.maskBatch.atlas != glyph.atlas || r.maskBatch.color != fill) {
		r.flushMasks()
	}
	r.maskBatch.atlas, r.maskBatch.color = glyph.atlas, fill
	x0, y0 := float32(visible.Min.X), float32(visible.Min.Y)
	x1, y1 := float32(visible.Max.X), float32(visible.Max.Y)
	u0 := float32(glyph.x+sx) / maskAtlasSize
	v0 := float32(glyph.y+sy) / maskAtlasSize
	u1 := float32(glyph.x+sx+visible.Dx()) / maskAtlasSize
	v1 := float32(glyph.y+sy+visible.Dy()) / maskAtlasSize
	r.maskBatch.vertices = append(r.maskBatch.vertices,
		x0, y0, u0, v0, x1, y0, u1, v0, x0, y1, u0, v1,
		x0, y1, u0, v1, x1, y0, u1, v0, x1, y1, u1, v1,
	)
}

func (r *Renderer) maskGlyph(mask []byte, width, height int) maskGlyph {
	key := maskCacheKey{digest: sha256.Sum256(mask), width: width, height: height}
	if glyph, ok := r.maskGlyphs[key]; ok {
		return glyph
	}
	if width > maskAtlasSize || height > maskAtlasSize {
		panic("GLES glyph mask exceeds atlas size")
	}
	atlasIndex := len(r.maskAtlases) - 1
	if atlasIndex < 0 || !r.canPlaceMask(atlasIndex, width, height) {
		if len(r.maskAtlases) >= maxMaskAtlases {
			r.flushMasks()
			r.releaseMaskAtlases()
		}
		atlasIndex = r.newMaskAtlas()
	}
	var glyph maskGlyph
	if !r.placeMask(atlasIndex, width, height, &glyph) {
		panic("GLES glyph mask atlas placement failed")
	}
	atlas := &r.maskAtlases[atlasIndex]
	glyph.atlas = atlasIndex
	r.api.bindTexture(glTexture2D, atlas.texture)
	r.api.texSubImage2D(glTexture2D, 0, int32(glyph.x), int32(glyph.y), int32(width), int32(height), glAlpha, glUnsignedByte, uintptr(unsafe.Pointer(&mask[0])))
	runtime.KeepAlive(mask)
	r.maskGlyphs[key] = glyph
	return glyph
}

func (r *Renderer) newMaskAtlas() int {
	var texture uint32
	r.api.genTextures(1, &texture)
	if texture == 0 {
		panic("glGenTextures returned 0")
	}
	r.api.bindTexture(glTexture2D, texture)
	r.api.texParameteri(glTexture2D, glTextureMinFilter, glNearest)
	r.api.texParameteri(glTexture2D, glTextureMagFilter, glNearest)
	r.api.texParameteri(glTexture2D, glTextureWrapS, glClampToEdge)
	r.api.texParameteri(glTexture2D, glTextureWrapT, glClampToEdge)
	r.api.texImage2D(glTexture2D, 0, glAlpha, maskAtlasSize, maskAtlasSize, 0, glAlpha, glUnsignedByte, 0)
	r.maskAtlases = append(r.maskAtlases, maskAtlas{texture: texture})
	return len(r.maskAtlases) - 1
}

func (r *Renderer) placeMask(index, width, height int, result *maskGlyph) bool {
	a := &r.maskAtlases[index]
	if a.x+width > maskAtlasSize {
		a.x, a.y, a.rowHeight = 0, a.y+a.rowHeight, 0
	}
	if a.y+height > maskAtlasSize {
		return false
	}
	if result != nil {
		*result = maskGlyph{x: a.x, y: a.y, width: width, height: height}
	}
	a.x += width
	a.rowHeight = max(a.rowHeight, height)
	return true
}

func (r *Renderer) canPlaceMask(index, width, height int) bool {
	a := r.maskAtlases[index]
	if a.x+width > maskAtlasSize {
		a.x, a.y, a.rowHeight = 0, a.y+a.rowHeight, 0
	}
	return a.y+height <= maskAtlasSize
}

func (r *Renderer) flushMasks() {
	vertices := r.maskBatch.vertices
	if len(vertices) == 0 {
		return
	}
	r.api.disable(glScissorTest)
	r.api.useProgram(r.maskProgram)
	r.api.bindTexture(glTexture2D, r.maskAtlases[r.maskBatch.atlas].texture)
	r.api.uniform4f(r.maskColor, float32(r.maskBatch.color.R())/255, float32(r.maskBatch.color.G())/255, float32(r.maskBatch.color.B())/255, 1)
	r.api.bindBuffer(glArrayBuffer, r.maskBuffer)
	r.api.bufferData(glArrayBuffer, uintptr(len(vertices))*unsafe.Sizeof(vertices[0]), uintptr(unsafe.Pointer(&vertices[0])), glDynamicDraw)
	r.api.enableVertexAttrib(uint32(r.maskPosition))
	r.api.vertexAttribPointer(uint32(r.maskPosition), 2, glFloat, 0, 16, 0)
	r.api.enableVertexAttrib(uint32(r.maskUVPosition))
	r.api.vertexAttribPointer(uint32(r.maskUVPosition), 2, glFloat, 0, 16, 8)
	count := int32(len(vertices) / 4)
	r.api.colorMask(1, 1, 1, 0)
	r.api.enable(glBlend)
	r.api.blendFuncSeparate(glSrcAlpha, glOneMinusSrcAlpha, glOne, glZero)
	r.api.uniform1i(r.maskAlphaMode, 0)
	r.api.drawArrays(glTriangles, 0, count)
	r.api.colorMask(0, 0, 0, 1)
	r.api.disable(glBlend)
	r.api.uniform1i(r.maskAlphaMode, 1)
	r.api.drawArrays(glTriangles, 0, count)
	r.api.colorMask(1, 1, 1, 1)
	r.api.uniform1i(r.maskAlphaMode, 0)
	r.maskBatch.vertices = vertices[:0]
}

func (r *Renderer) releaseMaskAtlases() {
	for _, atlas := range r.maskAtlases {
		texture := atlas.texture
		r.api.deleteTextures(1, &texture)
	}
	clear(r.maskGlyphs)
	r.maskAtlases = r.maskAtlases[:0]
}

func (r *Renderer) Snapshot() image.Image {
	// 文字绘制会批量延迟到下一次非文字操作或帧结束；读取前必须先提交，
	// 否则截图可能缺少帧尾的文字。
	r.flushMasks()

	// OpenGL framebuffer 的原点位于左下角，而 image.NRGBA 的原点位于
	// 左上角。先读取连续 RGBA 数据，再逐行倒序复制到目标图片。
	rowBytes := r.width * 4
	pixels := make([]byte, rowBytes*r.height)
	r.api.readPixels(0, 0, int32(r.width), int32(r.height), glRGBA, glUnsignedByte, uintptr(unsafe.Pointer(&pixels[0])))
	runtime.KeepAlive(pixels)
	if code := r.api.glGetError(); code != glNoError {
		panic(fmt.Sprintf("read GLES framebuffer: 0x%x", code))
	}

	return snapshotImage(pixels, r.width, r.height)
}

func snapshotImage(pixels []byte, width, height int) *image.NRGBA {
	rowBytes := width * 4
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		source := (height - 1 - y) * rowBytes
		copy(out.Pix[y*out.Stride:y*out.Stride+rowBytes], pixels[source:source+rowBytes])
	}
	return out
}

func shaderLog(a *api, shader uint32) string {
	var length int32
	a.getShaderiv(shader, glInfoLogLength, &length)
	if length <= 1 {
		return ""
	}
	log := make([]byte, length)
	var written int32
	a.getShaderInfoLog(shader, length, &written, &log[0])
	return string(log[:max(0, min(int(written), len(log)))])
}

func programLog(a *api, program uint32) string {
	var length int32
	a.getProgramiv(program, glInfoLogLength, &length)
	if length <= 1 {
		return ""
	}
	log := make([]byte, length)
	var written int32
	a.getProgramInfoLog(program, length, &written, &log[0])
	return string(log[:max(0, min(int(written), len(log)))])
}

func compileShader(a *api, shaderType uint32, source string) (uint32, error) {
	shader := a.createShader(shaderType)
	if shader == 0 {
		return 0, errors.New("glCreateShader returned 0")
	}
	bytes := append([]byte(source), 0)
	pointer := uintptr(unsafe.Pointer(&bytes[0]))
	length := int32(len(bytes) - 1)
	a.shaderSource(shader, 1, &pointer, &length)
	a.compileShader(shader)
	runtime.KeepAlive(bytes)
	var compiled int32
	a.getShaderiv(shader, glCompileStatus, &compiled)
	if compiled == 0 {
		log := shaderLog(a, shader)
		a.deleteShader(shader)
		return 0, fmt.Errorf("compile GLES shader: %s", log)
	}
	return shader, nil
}

func glName(name string) []byte { return append([]byte(name), 0) }

func (r *Renderer) initColorPipeline() error {
	vertex, err := compileShader(r.api, glVertexShader, colorVertexShader)
	if err != nil {
		return err
	}
	defer r.api.deleteShader(vertex)
	fragment, err := compileShader(r.api, glFragmentShader, colorFragmentShader)
	if err != nil {
		return err
	}
	defer r.api.deleteShader(fragment)

	program := r.api.createProgram()
	if program == 0 {
		return errors.New("glCreateProgram returned 0")
	}
	r.api.attachShader(program, vertex)
	r.api.attachShader(program, fragment)
	r.api.linkProgram(program)
	var linked int32
	r.api.getProgramiv(program, glLinkStatus, &linked)
	if linked == 0 {
		log := programLog(r.api, program)
		r.api.deleteProgram(program)
		return fmt.Errorf("link GLES color program: %s", log)
	}
	r.colorProgram = program

	positionName, rectName := glName("a_position"), glName("u_rect")
	viewportName, colorName := glName("u_viewport"), glName("u_color")
	r.positionLocation = r.api.getAttribLocation(program, &positionName[0])
	r.rectLocation = r.api.getUniformLocation(program, &rectName[0])
	r.viewportLocation = r.api.getUniformLocation(program, &viewportName[0])
	r.colorLocation = r.api.getUniformLocation(program, &colorName[0])
	if r.positionLocation < 0 || r.rectLocation < 0 || r.viewportLocation < 0 || r.colorLocation < 0 {
		r.api.deleteProgram(program)
		r.colorProgram = 0
		return errors.New("GLES color shader locations unavailable")
	}

	vertices := [...]float32{0, 0, 1, 0, 0, 1, 1, 1}
	r.api.genBuffers(1, &r.quadBuffer)
	if r.quadBuffer == 0 {
		r.api.deleteProgram(program)
		r.colorProgram = 0
		return errors.New("glGenBuffers returned 0")
	}
	r.api.bindBuffer(glArrayBuffer, r.quadBuffer)
	r.api.bufferData(glArrayBuffer, uintptr(len(vertices))*unsafe.Sizeof(vertices[0]), uintptr(unsafe.Pointer(&vertices[0])), glStaticDraw)
	r.api.useProgram(program)
	r.api.uniform2f(r.viewportLocation, float32(r.width), float32(r.height))
	r.api.viewport(0, 0, int32(r.width), int32(r.height))
	runtime.KeepAlive(vertices)
	if code := r.api.glGetError(); code != glNoError {
		r.api.deleteBuffers(1, &r.quadBuffer)
		r.api.deleteProgram(r.colorProgram)
		r.quadBuffer = 0
		r.colorProgram = 0
		return fmt.Errorf("initialize GLES color pipeline: 0x%x", code)
	}
	return nil
}

func (r *Renderer) initTexturePipeline() error {
	vertex, err := compileShader(r.api, glVertexShader, textureVertexShader)
	if err != nil {
		return err
	}
	defer r.api.deleteShader(vertex)
	fragment, err := compileShader(r.api, glFragmentShader, textureFragmentShader)
	if err != nil {
		return err
	}
	defer r.api.deleteShader(fragment)

	program := r.api.createProgram()
	if program == 0 {
		return errors.New("glCreateProgram returned 0")
	}
	r.api.attachShader(program, vertex)
	r.api.attachShader(program, fragment)
	r.api.linkProgram(program)
	var linked int32
	r.api.getProgramiv(program, glLinkStatus, &linked)
	if linked == 0 {
		log := programLog(r.api, program)
		r.api.deleteProgram(program)
		return fmt.Errorf("link GLES texture program: %s", log)
	}
	r.textureProgram = program

	positionName, rectName := glName("a_position"), glName("u_rect")
	viewportName, uvRectName := glName("u_viewport"), glName("u_uv_rect")
	samplerName, alphaModeName := glName("u_texture"), glName("u_alpha_only")
	r.texturePosition = r.api.getAttribLocation(program, &positionName[0])
	r.textureRect = r.api.getUniformLocation(program, &rectName[0])
	r.textureViewport = r.api.getUniformLocation(program, &viewportName[0])
	r.textureUVRect = r.api.getUniformLocation(program, &uvRectName[0])
	r.textureSampler = r.api.getUniformLocation(program, &samplerName[0])
	r.textureAlphaMode = r.api.getUniformLocation(program, &alphaModeName[0])
	if r.texturePosition < 0 || r.textureRect < 0 || r.textureViewport < 0 || r.textureUVRect < 0 || r.textureSampler < 0 || r.textureAlphaMode < 0 {
		r.api.deleteProgram(program)
		r.textureProgram = 0
		return errors.New("GLES texture shader locations unavailable")
	}
	r.api.useProgram(program)
	r.api.uniform2f(r.textureViewport, float32(r.width), float32(r.height))
	r.api.uniform1i(r.textureSampler, 0)
	r.api.uniform1i(r.textureAlphaMode, 0)
	if code := r.api.glGetError(); code != glNoError {
		r.api.deleteProgram(program)
		r.textureProgram = 0
		return fmt.Errorf("initialize GLES texture pipeline: 0x%x", code)
	}
	return nil
}

func (r *Renderer) initMaskPipeline() error {
	vertex, err := compileShader(r.api, glVertexShader, maskVertexShader)
	if err != nil {
		return err
	}
	defer r.api.deleteShader(vertex)
	fragment, err := compileShader(r.api, glFragmentShader, maskFragmentShader)
	if err != nil {
		return err
	}
	defer r.api.deleteShader(fragment)

	program := r.api.createProgram()
	if program == 0 {
		return errors.New("glCreateProgram returned 0")
	}
	r.api.attachShader(program, vertex)
	r.api.attachShader(program, fragment)
	r.api.linkProgram(program)
	var linked int32
	r.api.getProgramiv(program, glLinkStatus, &linked)
	if linked == 0 {
		log := programLog(r.api, program)
		r.api.deleteProgram(program)
		return fmt.Errorf("link GLES mask program: %s", log)
	}
	r.maskProgram = program

	positionName, uvName := glName("a_position"), glName("a_uv")
	viewportName := glName("u_viewport")
	samplerName, colorName := glName("u_texture"), glName("u_color")
	alphaModeName := glName("u_alpha_only")
	r.maskPosition = r.api.getAttribLocation(program, &positionName[0])
	r.maskUVPosition = r.api.getAttribLocation(program, &uvName[0])
	r.maskViewport = r.api.getUniformLocation(program, &viewportName[0])
	r.maskSampler = r.api.getUniformLocation(program, &samplerName[0])
	r.maskColor = r.api.getUniformLocation(program, &colorName[0])
	r.maskAlphaMode = r.api.getUniformLocation(program, &alphaModeName[0])
	if r.maskPosition < 0 || r.maskUVPosition < 0 || r.maskViewport < 0 || r.maskSampler < 0 || r.maskColor < 0 || r.maskAlphaMode < 0 {
		r.api.deleteProgram(program)
		r.maskProgram = 0
		return errors.New("GLES mask shader locations unavailable")
	}
	r.api.useProgram(program)
	r.api.uniform2f(r.maskViewport, float32(r.width), float32(r.height))
	r.api.uniform1i(r.maskSampler, 0)
	r.api.uniform1i(r.maskAlphaMode, 0)
	r.api.pixelStorei(glUnpackAlignment, 1)
	r.api.genBuffers(1, &r.maskBuffer)
	if r.maskBuffer == 0 {
		r.api.deleteProgram(program)
		r.maskProgram = 0
		return errors.New("glGenBuffers for mask batch returned 0")
	}
	if code := r.api.glGetError(); code != glNoError {
		r.api.deleteBuffers(1, &r.maskBuffer)
		r.maskBuffer = 0
		r.api.deleteProgram(program)
		r.maskProgram = 0
		return fmt.Errorf("initialize GLES mask pipeline: 0x%x", code)
	}
	return nil
}

func (r *Renderer) initTransformPipeline() error {
	vertex, err := compileShader(r.api, glVertexShader, transformVertexShader)
	if err != nil {
		return err
	}
	defer r.api.deleteShader(vertex)
	fragment, err := compileShader(r.api, glFragmentShader, transformFragmentShader)
	if err != nil {
		return err
	}
	defer r.api.deleteShader(fragment)
	program := r.api.createProgram()
	if program == 0 {
		return errors.New("glCreateProgram returned 0")
	}
	r.api.attachShader(program, vertex)
	r.api.attachShader(program, fragment)
	r.api.linkProgram(program)
	var linked int32
	r.api.getProgramiv(program, glLinkStatus, &linked)
	if linked == 0 {
		log := programLog(r.api, program)
		r.api.deleteProgram(program)
		return fmt.Errorf("link GLES transform program: %s", log)
	}
	r.transformProgram = program
	positionName, rectName := glName("a_position"), glName("u_rect")
	viewportName, centerName := glName("u_viewport"), glName("u_center")
	imageSizeName, inverseName := glName("u_image_size"), glName("u_inverse")
	samplerName, alphaModeName := glName("u_texture"), glName("u_alpha_only")
	r.transformPosition = r.api.getAttribLocation(program, &positionName[0])
	r.transformRect = r.api.getUniformLocation(program, &rectName[0])
	r.transformViewport = r.api.getUniformLocation(program, &viewportName[0])
	r.transformCenter = r.api.getUniformLocation(program, &centerName[0])
	r.transformImageSize = r.api.getUniformLocation(program, &imageSizeName[0])
	r.transformInverse = r.api.getUniformLocation(program, &inverseName[0])
	r.transformSampler = r.api.getUniformLocation(program, &samplerName[0])
	r.transformAlphaMode = r.api.getUniformLocation(program, &alphaModeName[0])
	if r.transformPosition < 0 || r.transformRect < 0 || r.transformViewport < 0 || r.transformCenter < 0 || r.transformImageSize < 0 || r.transformInverse < 0 || r.transformSampler < 0 || r.transformAlphaMode < 0 {
		r.api.deleteProgram(program)
		r.transformProgram = 0
		return errors.New("GLES transform shader locations unavailable")
	}
	r.api.useProgram(program)
	r.api.uniform2f(r.transformViewport, float32(r.width), float32(r.height))
	r.api.uniform1i(r.transformSampler, 0)
	r.api.uniform1i(r.transformAlphaMode, 0)
	if code := r.api.glGetError(); code != glNoError {
		r.api.deleteProgram(program)
		r.transformProgram = 0
		return fmt.Errorf("initialize GLES transform pipeline: 0x%x", code)
	}
	return nil
}

func (r *Renderer) releaseGLResources() {
	r.flushMasks()
	r.releaseMaskAtlases()
	r.releaseImageTextures()
	if r.maskBuffer != 0 {
		r.api.deleteBuffers(1, &r.maskBuffer)
		r.maskBuffer = 0
	}
	if r.quadBuffer != 0 {
		r.api.deleteBuffers(1, &r.quadBuffer)
		r.quadBuffer = 0
	}
	if r.textureProgram != 0 {
		r.api.deleteProgram(r.textureProgram)
		r.textureProgram = 0
	}
	if r.maskProgram != 0 {
		r.api.deleteProgram(r.maskProgram)
		r.maskProgram = 0
	}
	if r.transformProgram != 0 {
		r.api.deleteProgram(r.transformProgram)
		r.transformProgram = 0
	}
	if r.colorProgram != 0 {
		r.api.deleteProgram(r.colorProgram)
		r.colorProgram = 0
	}
}

// Close releases renderer resources and delegates platform teardown.
func (r *Renderer) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	r.releaseGLResources()
	r.closePlatform()
	return nil
}
