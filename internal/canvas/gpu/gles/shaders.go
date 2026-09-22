package gles

import _ "embed"

var (
	//go:embed shaders/color.vert.glsl
	colorVertexShader string
	//go:embed shaders/color.frag.glsl
	colorFragmentShader string
	//go:embed shaders/mask.vert.glsl
	maskVertexShader string
	//go:embed shaders/mask.frag.glsl
	maskFragmentShader string
	//go:embed shaders/transform.vert.glsl
	transformVertexShader string
	//go:embed shaders/transform.frag.glsl
	transformFragmentShader string
	//go:embed shaders/present.vert.glsl
	presentVertexShader string
	//go:embed shaders/present.frag.glsl
	presentFragmentShader string
)
