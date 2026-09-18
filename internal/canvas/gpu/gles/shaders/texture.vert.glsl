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
}
