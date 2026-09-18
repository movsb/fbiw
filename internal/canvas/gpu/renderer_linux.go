//go:build linux

package gpu

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	canvas "github.com/movsb/fbiw/internal/canvas"
)

const (
	eglFalse             = 0
	eglNone              = 0x3038
	eglRenderableType    = 0x3040
	eglOpenGLES2Bit      = 0x0004
	eglRedSize           = 0x3024
	eglGreenSize         = 0x3023
	eglBlueSize          = 0x3022
	eglAlphaSize         = 0x3021
	eglSamples           = 0x3031
	eglContextClientVers = 0x3098
	eglWidth             = 0x3057
	eglHeight            = 0x3056
	glColorBufferBit     = 0x00004000
	glScissorTest        = 0x0c11
	glBlend              = 0x0be2
	glArrayBuffer        = 0x8892
	glStaticDraw         = 0x88e4
	glFloat              = 0x1406
	glTriangleStrip      = 0x0005
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
	getDisplay          func(uintptr) uintptr
	initialize          func(uintptr, *int32, *int32) uint32
	chooseConfig        func(uintptr, *int32, *uintptr, int32, *int32) uint32
	getConfigAttrib     func(uintptr, uintptr, int32, *int32) uint32
	createWindowSurface func(uintptr, uintptr, uintptr, *int32) uintptr
	createContext       func(uintptr, uintptr, uintptr, *int32) uintptr
	makeCurrent         func(uintptr, uintptr, uintptr, uintptr) uint32
	querySurface        func(uintptr, uintptr, int32, *int32) uint32
	swapBuffers         func(uintptr, uintptr) uint32
	getError            func() int32
	destroySurface      func(uintptr, uintptr) uint32
	destroyContext      func(uintptr, uintptr) uint32
	terminate           func(uintptr) uint32
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
	colorMask           func(uint8, uint8, uint8, uint8)
	pixelStorei         func(uint32, int32)
}

func loadAPI() (*api, func(), error) {
	egl, err := purego.Dlopen("libEGL.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, nil, fmt.Errorf("load libEGL.so.1: %w", err)
	}
	gles, err := purego.Dlopen("libGLESv2.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		purego.Dlclose(egl)
		return nil, nil, fmt.Errorf("load libGLESv2.so.2: %w", err)
	}
	closeLibraries := func() { purego.Dlclose(gles); purego.Dlclose(egl) }
	a := &api{}
	purego.RegisterLibFunc(&a.getDisplay, egl, "eglGetDisplay")
	purego.RegisterLibFunc(&a.initialize, egl, "eglInitialize")
	purego.RegisterLibFunc(&a.chooseConfig, egl, "eglChooseConfig")
	purego.RegisterLibFunc(&a.getConfigAttrib, egl, "eglGetConfigAttrib")
	purego.RegisterLibFunc(&a.createWindowSurface, egl, "eglCreateWindowSurface")
	purego.RegisterLibFunc(&a.createContext, egl, "eglCreateContext")
	purego.RegisterLibFunc(&a.makeCurrent, egl, "eglMakeCurrent")
	purego.RegisterLibFunc(&a.querySurface, egl, "eglQuerySurface")
	purego.RegisterLibFunc(&a.swapBuffers, egl, "eglSwapBuffers")
	purego.RegisterLibFunc(&a.getError, egl, "eglGetError")
	purego.RegisterLibFunc(&a.destroySurface, egl, "eglDestroySurface")
	purego.RegisterLibFunc(&a.destroyContext, egl, "eglDestroyContext")
	purego.RegisterLibFunc(&a.terminate, egl, "eglTerminate")
	purego.RegisterLibFunc(&a.clearColor, gles, "glClearColor")
	purego.RegisterLibFunc(&a.clear, gles, "glClear")
	purego.RegisterLibFunc(&a.enable, gles, "glEnable")
	purego.RegisterLibFunc(&a.disable, gles, "glDisable")
	purego.RegisterLibFunc(&a.scissor, gles, "glScissor")
	purego.RegisterLibFunc(&a.viewport, gles, "glViewport")
	purego.RegisterLibFunc(&a.createShader, gles, "glCreateShader")
	purego.RegisterLibFunc(&a.shaderSource, gles, "glShaderSource")
	purego.RegisterLibFunc(&a.compileShader, gles, "glCompileShader")
	purego.RegisterLibFunc(&a.getShaderiv, gles, "glGetShaderiv")
	purego.RegisterLibFunc(&a.getShaderInfoLog, gles, "glGetShaderInfoLog")
	purego.RegisterLibFunc(&a.deleteShader, gles, "glDeleteShader")
	purego.RegisterLibFunc(&a.createProgram, gles, "glCreateProgram")
	purego.RegisterLibFunc(&a.attachShader, gles, "glAttachShader")
	purego.RegisterLibFunc(&a.linkProgram, gles, "glLinkProgram")
	purego.RegisterLibFunc(&a.getProgramiv, gles, "glGetProgramiv")
	purego.RegisterLibFunc(&a.getProgramInfoLog, gles, "glGetProgramInfoLog")
	purego.RegisterLibFunc(&a.deleteProgram, gles, "glDeleteProgram")
	purego.RegisterLibFunc(&a.useProgram, gles, "glUseProgram")
	purego.RegisterLibFunc(&a.getAttribLocation, gles, "glGetAttribLocation")
	purego.RegisterLibFunc(&a.getUniformLocation, gles, "glGetUniformLocation")
	purego.RegisterLibFunc(&a.uniform2f, gles, "glUniform2f")
	purego.RegisterLibFunc(&a.uniform4f, gles, "glUniform4f")
	purego.RegisterLibFunc(&a.genBuffers, gles, "glGenBuffers")
	purego.RegisterLibFunc(&a.deleteBuffers, gles, "glDeleteBuffers")
	purego.RegisterLibFunc(&a.bindBuffer, gles, "glBindBuffer")
	purego.RegisterLibFunc(&a.bufferData, gles, "glBufferData")
	purego.RegisterLibFunc(&a.enableVertexAttrib, gles, "glEnableVertexAttribArray")
	purego.RegisterLibFunc(&a.vertexAttribPointer, gles, "glVertexAttribPointer")
	purego.RegisterLibFunc(&a.drawArrays, gles, "glDrawArrays")
	purego.RegisterLibFunc(&a.blendColor, gles, "glBlendColor")
	purego.RegisterLibFunc(&a.blendFuncSeparate, gles, "glBlendFuncSeparate")
	purego.RegisterLibFunc(&a.glGetError, gles, "glGetError")
	purego.RegisterLibFunc(&a.uniform1i, gles, "glUniform1i")
	purego.RegisterLibFunc(&a.genTextures, gles, "glGenTextures")
	purego.RegisterLibFunc(&a.deleteTextures, gles, "glDeleteTextures")
	purego.RegisterLibFunc(&a.bindTexture, gles, "glBindTexture")
	purego.RegisterLibFunc(&a.texParameteri, gles, "glTexParameteri")
	purego.RegisterLibFunc(&a.texImage2D, gles, "glTexImage2D")
	purego.RegisterLibFunc(&a.colorMask, gles, "glColorMask")
	purego.RegisterLibFunc(&a.pixelStorei, gles, "glPixelStorei")
	return a, closeLibraries, nil
}

type Renderer struct {
	api                *api
	display, surface   uintptr
	context            uintptr
	width, height      int
	eglMajor, eglMinor int32
	closeLibraries     func()
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
	maskRect           int32
	maskViewport       int32
	maskUVRect         int32
	maskSampler        int32
	maskColor          int32
	maskAlphaMode      int32
	maskTextures       map[maskCacheKey]uint32
	imageTextures      map[imageCacheKey]imageTexture
}

type maskCacheKey struct {
	digest        [sha256.Size]byte
	width, height int
}

const maxCachedMasks = 4096

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
	if r.api.swapBuffers(r.display, r.surface) == eglFalse {
		panic(fmt.Sprintf("eglSwapBuffers: 0x%x", r.api.getError()))
	}
}
func (r *Renderer) Clear() {
	r.api.disable(glScissorTest)
	r.api.disable(glBlend)
	r.api.clearColor(0, 0, 0, 0)
	r.api.clear(glColorBufferBit)
}
func (r *Renderer) FillRect(rect, clip image.Rectangle, fill canvas.Color) {
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
func (*Renderer) DrawImageTransformed(canvas.Image, float64, float64, float64, float64, image.Rectangle) {
	panic("GLES renderer图片变换尚未实现")
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

	texture := r.maskTexture(mask[:mw*mh], mw, mh)
	r.api.bindTexture(glTexture2D, texture)

	r.api.disable(glScissorTest)
	r.api.useProgram(r.maskProgram)
	r.api.uniform4f(r.maskRect, float32(visible.Min.X), float32(visible.Min.Y), float32(visible.Dx()), float32(visible.Dy()))
	r.api.uniform4f(r.maskUVRect, float32(sx)/float32(mw), float32(sy)/float32(mh), float32(visible.Dx())/float32(mw), float32(visible.Dy())/float32(mh))
	r.api.uniform4f(r.maskColor, float32(fill.R())/255, float32(fill.G())/255, float32(fill.B())/255, 1)
	r.api.uniform1i(r.maskSampler, 0)
	r.api.bindBuffer(glArrayBuffer, r.quadBuffer)
	r.api.enableVertexAttrib(uint32(r.maskPosition))
	r.api.vertexAttribPointer(uint32(r.maskPosition), 2, glFloat, 0, 0, 0)

	// Coverage blends RGB; a second alpha-only pass matches the CPU rule that
	// every non-zero mask pixel makes the destination alpha opaque.
	r.api.colorMask(1, 1, 1, 0)
	r.api.enable(glBlend)
	r.api.blendFuncSeparate(glSrcAlpha, glOneMinusSrcAlpha, glOne, glZero)
	r.api.uniform1i(r.maskAlphaMode, 0)
	r.api.drawArrays(glTriangleStrip, 0, 4)
	r.api.colorMask(0, 0, 0, 1)
	r.api.disable(glBlend)
	r.api.uniform1i(r.maskAlphaMode, 1)
	r.api.drawArrays(glTriangleStrip, 0, 4)
	r.api.colorMask(1, 1, 1, 1)
	r.api.uniform1i(r.maskAlphaMode, 0)
}

func (r *Renderer) maskTexture(mask []byte, width, height int) uint32 {
	key := maskCacheKey{digest: sha256.Sum256(mask), width: width, height: height}
	if texture := r.maskTextures[key]; texture != 0 {
		return texture
	}
	if len(r.maskTextures) >= maxCachedMasks {
		r.releaseMaskTextures()
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
	r.api.texImage2D(glTexture2D, 0, glAlpha, int32(width), int32(height), 0, glAlpha, glUnsignedByte, uintptr(unsafe.Pointer(&mask[0])))
	runtime.KeepAlive(mask)
	r.maskTextures[key] = texture
	return texture
}

func (r *Renderer) releaseMaskTextures() {
	for key, texture := range r.maskTextures {
		r.api.deleteTextures(1, &texture)
		delete(r.maskTextures, key)
	}
}
func (*Renderer) Pixel(image.Point) color.NRGBA { panic("GLES renderer单像素读取尚未实现") }
func (*Renderer) SetPixel(image.Point, color.NRGBA) {
	panic("GLES renderer单像素写入尚未实现")
}
func (*Renderer) Snapshot() image.Image { panic("GLES renderer截图尚未实现") }

const colorVertexShader = `
attribute vec2 a_position;
uniform vec4 u_rect;
uniform vec2 u_viewport;
void main() {
	vec2 pixel = u_rect.xy + a_position * u_rect.zw;
	vec2 clip = pixel / u_viewport * 2.0 - 1.0;
	gl_Position = vec4(clip.x, -clip.y, 0.0, 1.0);
}`

const colorFragmentShader = `
precision mediump float;
uniform vec4 u_color;
void main() {
	gl_FragColor = u_color;
}`

const textureVertexShader = `
attribute vec2 a_position;
uniform vec4 u_rect;
uniform vec2 u_viewport;
uniform vec4 u_uv_rect;
varying vec2 v_uv;
void main() {
	vec2 pixel = u_rect.xy + a_position * u_rect.zw;
	vec2 clip = pixel / u_viewport * 2.0 - 1.0;
	gl_Position = vec4(clip.x, -clip.y, 0.0, 1.0);
	v_uv = u_uv_rect.xy + a_position * u_uv_rect.zw;
}`

const textureFragmentShader = `
precision mediump float;
uniform sampler2D u_texture;
uniform int u_alpha_only;
varying vec2 v_uv;
void main() {
	vec4 color = texture2D(u_texture, v_uv).bgra;
	if (u_alpha_only != 0) {
		if (color.a == 0.0) discard;
		gl_FragColor = vec4(0.0, 0.0, 0.0, 1.0);
	} else {
		gl_FragColor = color;
	}
}`

const maskFragmentShader = `
precision mediump float;
uniform sampler2D u_texture;
uniform vec4 u_color;
uniform int u_alpha_only;
varying vec2 v_uv;
void main() {
	float coverage = texture2D(u_texture, v_uv).a;
	if (coverage == 0.0) discard;
	if (u_alpha_only != 0) {
		gl_FragColor = vec4(0.0, 0.0, 0.0, 1.0);
	} else {
		gl_FragColor = vec4(u_color.rgb, coverage);
	}
}`

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
	vertex, err := compileShader(r.api, glVertexShader, textureVertexShader)
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

	positionName, rectName := glName("a_position"), glName("u_rect")
	viewportName, uvRectName := glName("u_viewport"), glName("u_uv_rect")
	samplerName, colorName := glName("u_texture"), glName("u_color")
	alphaModeName := glName("u_alpha_only")
	r.maskPosition = r.api.getAttribLocation(program, &positionName[0])
	r.maskRect = r.api.getUniformLocation(program, &rectName[0])
	r.maskViewport = r.api.getUniformLocation(program, &viewportName[0])
	r.maskUVRect = r.api.getUniformLocation(program, &uvRectName[0])
	r.maskSampler = r.api.getUniformLocation(program, &samplerName[0])
	r.maskColor = r.api.getUniformLocation(program, &colorName[0])
	r.maskAlphaMode = r.api.getUniformLocation(program, &alphaModeName[0])
	if r.maskPosition < 0 || r.maskRect < 0 || r.maskViewport < 0 || r.maskUVRect < 0 || r.maskSampler < 0 || r.maskColor < 0 || r.maskAlphaMode < 0 {
		r.api.deleteProgram(program)
		r.maskProgram = 0
		return errors.New("GLES mask shader locations unavailable")
	}
	r.api.useProgram(program)
	r.api.uniform2f(r.maskViewport, float32(r.width), float32(r.height))
	r.api.uniform1i(r.maskSampler, 0)
	r.api.uniform1i(r.maskAlphaMode, 0)
	r.api.pixelStorei(glUnpackAlignment, 1)
	if code := r.api.glGetError(); code != glNoError {
		r.api.deleteProgram(program)
		r.maskProgram = 0
		return fmt.Errorf("initialize GLES mask pipeline: 0x%x", code)
	}
	return nil
}

func (r *Renderer) releaseGLResources() {
	r.releaseMaskTextures()
	r.releaseImageTextures()
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
	if r.colorProgram != 0 {
		r.api.deleteProgram(r.colorProgram)
		r.colorProgram = 0
	}
}

// Open creates the EGL surface and GLES context for a renderer. The calling
// goroutine remains locked to its current OS thread until Close is called.
func Open() (_ *Renderer, err error) {
	runtime.LockOSThread()
	var (
		a              *api
		closeLibraries func()
		display        uintptr
		surface        uintptr
		context        uintptr
		current        bool
	)
	defer func() {
		if err == nil {
			return
		}
		if a != nil {
			if current {
				a.makeCurrent(display, 0, 0, 0)
			}
			if context != 0 {
				a.destroyContext(display, context)
			}
			if surface != 0 {
				a.destroySurface(display, surface)
			}
			if display != 0 {
				a.terminate(display)
			}
		}
		if closeLibraries != nil {
			closeLibraries()
		}
		runtime.UnlockOSThread()
	}()

	a, closeLibraries, err = loadAPI()
	if err != nil {
		return nil, err
	}
	display = a.getDisplay(0)
	if display == 0 {
		return nil, errors.New("eglGetDisplay returned EGL_NO_DISPLAY")
	}
	var major, minor int32
	if a.initialize(display, &major, &minor) == eglFalse {
		return nil, fmt.Errorf("eglInitialize: 0x%x", a.getError())
	}
	attrs := []int32{eglRedSize, 8, eglGreenSize, 8, eglBlueSize, 8, eglAlphaSize, 8, eglSamples, 4, eglRenderableType, eglOpenGLES2Bit, eglNone}
	var count int32
	if a.chooseConfig(display, &attrs[0], nil, 0, &count) == eglFalse || count == 0 {
		return nil, fmt.Errorf("eglChooseConfig: count=%d error=0x%x", count, a.getError())
	}
	configs := make([]uintptr, count)
	if a.chooseConfig(display, &attrs[0], &configs[0], count, &count) == eglFalse {
		return nil, fmt.Errorf("eglChooseConfig list: 0x%x", a.getError())
	}
	wanted := []struct{ attribute, value int32 }{{eglRedSize, 8}, {eglGreenSize, 8}, {eglBlueSize, 8}, {eglAlphaSize, 8}, {eglSamples, 4}}
	var config uintptr
	for _, candidate := range configs[:count] {
		matches := true
		for _, want := range wanted {
			var value int32
			if a.getConfigAttrib(display, candidate, want.attribute, &value) == eglFalse || value != want.value {
				matches = false
				break
			}
		}
		if matches {
			config = candidate
			break
		}
	}
	if config == 0 {
		return nil, errors.New("no exact RGBA8 MSAA4 EGL config")
	}
	surface = a.createWindowSurface(display, config, 0, nil)
	if surface == 0 {
		return nil, fmt.Errorf("eglCreateWindowSurface: 0x%x", a.getError())
	}
	contextAttrs := []int32{eglContextClientVers, 2, eglNone}
	context = a.createContext(display, config, 0, &contextAttrs[0])
	if context == 0 {
		return nil, fmt.Errorf("eglCreateContext: 0x%x", a.getError())
	}
	if a.makeCurrent(display, surface, surface, context) == eglFalse {
		return nil, fmt.Errorf("eglMakeCurrent: 0x%x", a.getError())
	}
	current = true
	var width, height int32
	if a.querySurface(display, surface, eglWidth, &width) == eglFalse || a.querySurface(display, surface, eglHeight, &height) == eglFalse {
		return nil, fmt.Errorf("eglQuerySurface: 0x%x", a.getError())
	}
	renderer := &Renderer{
		api:            a,
		display:        display,
		surface:        surface,
		context:        context,
		width:          int(width),
		height:         int(height),
		eglMajor:       major,
		eglMinor:       minor,
		closeLibraries: closeLibraries,
		maskTextures:   make(map[maskCacheKey]uint32),
		imageTextures:  make(map[imageCacheKey]imageTexture),
	}
	if err = renderer.initColorPipeline(); err != nil {
		return nil, err
	}
	if err = renderer.initTexturePipeline(); err != nil {
		renderer.releaseGLResources()
		return nil, err
	}
	if err = renderer.initMaskPipeline(); err != nil {
		renderer.releaseGLResources()
		return nil, err
	}
	return renderer, nil
}

// Close releases all EGL resources and unlocks the OS thread locked by Open.
// It must be called by the same goroutine that called Open.
func (r *Renderer) Close() {
	if r == nil || r.closed {
		return
	}
	r.closed = true
	r.releaseGLResources()
	r.api.makeCurrent(r.display, 0, 0, 0)
	r.api.destroyContext(r.display, r.context)
	r.api.destroySurface(r.display, r.surface)
	r.api.terminate(r.display)
	r.closeLibraries()
	runtime.UnlockOSThread()
}

func RunProbe() error {
	r, err := Open()
	if err != nil {
		return err
	}
	defer r.Close()
	r.BeginFrame()
	r.Clear()
	bounds := image.Rect(0, 0, r.width, r.height)
	r.FillRect(bounds, bounds, canvas.Color(0xff1f5cc7))
	r.FillRect(image.Rect(80, 80, 360, 260), bounds, canvas.Color(0xffff8a20))
	r.FillRect(image.Rect(160, 160, 440, 340), image.Rect(200, 120, 400, 300), canvas.Color(0xff38c972))
	r.FillRect(image.Rect(300, 220, 620, 460), bounds, canvas.Color(0x808b3dff))
	probeImage := canvas.Image{Width: 160, Height: 120, Pixels: make([]byte, 160*120*4)}
	for y := 0; y < probeImage.Height; y++ {
		for x := 0; x < probeImage.Width; x++ {
			pixel := probeImage.Pixels[(y*probeImage.Width+x)*4:]
			pixel[0], pixel[1], pixel[2], pixel[3] = 0x30, 0xd0, 0xff, 0xd0
			if (x/20+y/20)%2 == 0 {
				pixel[0], pixel[1], pixel[2] = 0xe0, 0x40, 0x30
			}
		}
	}
	r.DrawImage(probeImage, image.Rect(20, 10, 150, 110), image.Pt(650, 160), image.Rect(680, 180, 790, 250))
	cachedImages := len(r.imageTextures)
	r.DrawImage(probeImage, image.Rect(0, 0, 80, 60), image.Pt(820, 160), bounds)
	if len(r.imageTextures) != cachedImages {
		return errors.New("GLES image texture cache missed identical storage")
	}
	maskWidth, maskHeight := 127, 96
	probeMask := make([]byte, maskWidth*maskHeight)
	for y := 0; y < maskHeight; y++ {
		for x := 0; x < maskWidth; x++ {
			dx, dy := x-maskWidth/2, y-maskHeight/2
			distance := dx*dx + dy*dy
			if distance < 42*42 {
				probeMask[y*maskWidth+x] = uint8(min(255, (42*42-distance)/4))
			}
		}
	}
	r.DrawMask(probeMask, maskWidth, maskHeight, image.Pt(610, 360), image.Rect(630, 375, 720, 440), canvas.Color(0xffffe050))
	cachedMasks := len(r.maskTextures)
	r.DrawMask(probeMask, maskWidth, maskHeight, image.Pt(750, 360), bounds, canvas.Color(0xff50e0ff))
	if len(r.maskTextures) != cachedMasks {
		return errors.New("GLES mask texture cache missed identical content")
	}
	if code := r.api.glGetError(); code != glNoError {
		return fmt.Errorf("draw GLES color probe: 0x%x", code)
	}
	r.EndFrame()
	fmt.Printf("GLES renderer probe OK: EGL %d.%d, surface %dx%d\n", r.eglMajor, r.eglMinor, r.width, r.height)
	return nil
}
