precision mediump float;
uniform sampler2D u_texture;
uniform vec4 u_color;
varying vec2 v_uv;
void main() {
	float coverage = texture2D(u_texture, v_uv).a;
	if (coverage == 0.0) discard;
	gl_FragColor = vec4(u_color.rgb, coverage);
}
