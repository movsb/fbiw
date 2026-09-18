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
}
