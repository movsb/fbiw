precision highp float;
uniform sampler2D u_texture;
uniform vec2 u_center;
uniform vec2 u_image_size;
uniform vec4 u_source_rect;
uniform vec4 u_clip_rect;
uniform vec2 u_clip_params;
uniform vec4 u_inverse;
varying vec2 v_pixel;

vec4 sourcePixel(vec2 pixel) {
	if (pixel.x < u_source_rect.x || pixel.y < u_source_rect.y ||
		pixel.x >= u_source_rect.x + u_source_rect.z || pixel.y >= u_source_rect.y + u_source_rect.w) {
		return vec4(0.0);
	}
	return texture2D(u_texture, (pixel + 0.5) / u_image_size).bgra;
}

void main() {
	vec2 delta = v_pixel - u_center;
	vec2 source = vec2(
		u_inverse.x * delta.x + u_inverse.y * delta.y,
		u_inverse.z * delta.x + u_inverse.w * delta.y
	) + u_source_rect.xy + u_source_rect.zw * 0.5 - 0.5;
	vec2 base = floor(source);
	vec2 fraction = source - base;
	vec4 c00 = sourcePixel(base);
	vec4 c10 = sourcePixel(base + vec2(1.0, 0.0));
	vec4 c01 = sourcePixel(base + vec2(0.0, 1.0));
	vec4 c11 = sourcePixel(base + vec2(1.0, 1.0));
	float w00 = (1.0 - fraction.x) * (1.0 - fraction.y);
	float w10 = fraction.x * (1.0 - fraction.y);
	float w01 = (1.0 - fraction.x) * fraction.y;
	float w11 = fraction.x * fraction.y;
	float alpha = c00.a*w00 + c10.a*w10 + c01.a*w01 + c11.a*w11;
	float coverage = 1.0;
	if (u_clip_params.x > 0.0) {
		vec2 half_size = u_clip_rect.zw * 0.5;
		vec2 q = abs(v_pixel - (u_clip_rect.xy + half_size)) - half_size + u_clip_params.x;
		float distance = length(max(q, vec2(0.0))) - u_clip_params.x;
		coverage = clamp(0.5 - distance, 0.0, 1.0);
	}
	if (alpha <= 0.0 || coverage <= 0.0) discard;
	vec3 premultiplied = c00.rgb*c00.a*w00 + c10.rgb*c10.a*w10 + c01.rgb*c01.a*w01 + c11.rgb*c11.a*w11;
	gl_FragColor = vec4(premultiplied / alpha, alpha * coverage * u_clip_params.y);
}
