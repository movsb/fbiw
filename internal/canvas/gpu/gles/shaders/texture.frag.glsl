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
}
