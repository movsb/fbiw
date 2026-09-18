attribute vec2 a_position;
uniform vec4 u_rect;
uniform vec2 u_viewport;
varying vec2 v_pixel;
void main() {
	v_pixel = u_rect.xy + a_position * u_rect.zw;
	vec2 clip = v_pixel / u_viewport * 2.0 - 1.0;
	gl_Position = vec4(clip.x, -clip.y, 0.0, 1.0);
}
