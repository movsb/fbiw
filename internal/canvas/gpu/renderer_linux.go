//go:build linux

package gpu

import (
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
}

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
func (*Renderer) DrawImage(canvas.Image, image.Rectangle, image.Point, image.Rectangle) {
	panic("GLES renderer图片纹理尚未实现")
}
func (*Renderer) DrawImageTransformed(canvas.Image, float64, float64, float64, float64, image.Rectangle) {
	panic("GLES renderer图片变换尚未实现")
}
func (*Renderer) DrawMask([]byte, int, int, image.Point, image.Rectangle, canvas.Color) {
	panic("GLES renderer字形纹理尚未实现")
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
	}
	if err = renderer.initColorPipeline(); err != nil {
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
	if r.quadBuffer != 0 {
		r.api.deleteBuffers(1, &r.quadBuffer)
	}
	if r.colorProgram != 0 {
		r.api.deleteProgram(r.colorProgram)
	}
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
	if code := r.api.glGetError(); code != glNoError {
		return fmt.Errorf("draw GLES color probe: 0x%x", code)
	}
	r.EndFrame()
	fmt.Printf("GLES renderer probe OK: EGL %d.%d, surface %dx%d\n", r.eglMajor, r.eglMinor, r.width, r.height)
	return nil
}
