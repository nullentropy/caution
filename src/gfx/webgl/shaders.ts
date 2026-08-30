// One unified UI shader. Every primitive is an instanced unit quad
//
// The fragment shader branches on mode:
//   0 = SDF rounded rect (fill + optional border), analytic AA
//   1 = SDF rounded-rect shadow (smoothstep falloff over blur radius)
//   2 = textured (glyphs / images), premultiplied atlas, tinted by color
// Clipping is a per-instance axis-aligned rect evaluated per fragment, so clip
// changes never break batching. All coordinates are logical pixels.

export const VERT = `#version 300 es
precision highp float;

layout(location=0) in vec2 a_unit;
layout(location=1) in vec4 i_rect;    // x, y, w, h
layout(location=2) in vec4 i_radii;   // tl, tr, br, bl
layout(location=3) in vec4 i_color;   // straight alpha
layout(location=4) in vec4 i_border;  // border color, straight alpha
layout(location=5) in vec4 i_params;  // borderWidth, mode, blur, unused
layout(location=6) in vec4 i_uv;      // u0, v0, u1, v1
layout(location=7) in vec4 i_clip;    // x, y, w, h

uniform vec2 u_view;   // logical size of the current render target
uniform vec2 u_origin; // logical offset of the target (0,0 for the canvas)
uniform float u_flipY; // -1 for the canvas, +1 for offscreen layers

out vec2 v_pos;
out vec2 v_uv;
flat out vec4 v_rect;
flat out vec4 v_radii;
flat out vec4 v_color;
flat out vec4 v_border;
flat out vec4 v_params;
flat out vec4 v_clip;

void main() {
  int mode = int(i_params.y + 0.5);
  // Expand the quad so AA (1px) and shadow falloff have room outside the rect.
  // Textured quads are exact: their uv mapping must not shift.
  float margin = mode == 1 ? i_params.z * 2.5 + 1.0 : (mode == 2 ? 0.0 : 1.0);
  vec2 pos = i_rect.xy - vec2(margin) + a_unit * (i_rect.zw + vec2(margin * 2.0));

  v_pos = pos;
  v_uv = mix(i_uv.xy, i_uv.zw, a_unit);
  v_rect = i_rect;
  v_radii = i_radii;
  v_color = i_color;
  v_border = i_border;
  v_params = i_params;
  v_clip = i_clip;

  vec2 ndc = (pos - u_origin) / u_view * 2.0 - 1.0;
  gl_Position = vec4(ndc.x, u_flipY * ndc.y, 0.0, 1.0);
}`;

export const FRAG = `#version 300 es
precision highp float;

uniform sampler2D u_tex;

in vec2 v_pos;
in vec2 v_uv;
flat in vec4 v_rect;
flat in vec4 v_radii;
flat in vec4 v_color;
flat in vec4 v_border;
flat in vec4 v_params;
flat in vec4 v_clip;

out vec4 o_color;

float sdRoundRect(vec2 p, vec2 hs, vec4 r) {
  // Pick this quadrant's radius: r = (tl, tr, br, bl), +x is right, +y is down.
  float rad = p.x >= 0.0 ? (p.y >= 0.0 ? r.z : r.y) : (p.y >= 0.0 ? r.w : r.x);
  rad = min(rad, min(hs.x, hs.y));
  vec2 q = abs(p) - hs + rad;
  return min(max(q.x, q.y), 0.0) + length(max(q, vec2(0.0))) - rad;
}

vec4 premul(vec4 c) { return vec4(c.rgb * c.a, c.a); }

void main() {
  int mode = int(v_params.y + 0.5);

  // Axis-aligned clip coverage with ~1px AA at the clip edge.
  vec2 cmax = v_clip.xy + v_clip.zw;
  vec2 cc = clamp(min(v_pos - v_clip.xy, cmax - v_pos) + 0.5, 0.0, 1.0);
  float clipCov = cc.x * cc.y;

  vec4 result;
  if (mode == 2) {
    result = texture(u_tex, v_uv) * premul(v_color);
    // Rounded corners for textured quads (images). Glyphs pass zero radii
    // and skip this, keeping their own edge anti-aliasing untouched.
    if (v_radii.x + v_radii.y + v_radii.z + v_radii.w > 0.0) {
      vec2 hs = v_rect.zw * 0.5;
      float d = sdRoundRect(v_pos - (v_rect.xy + hs), hs, v_radii);
      result *= clamp(0.5 - d, 0.0, 1.0);
    }
  } else {
    vec2 hs = v_rect.zw * 0.5;
    float d = sdRoundRect(v_pos - (v_rect.xy + hs), hs, v_radii);
    if (mode == 1) {
      float blur = max(v_params.z, 0.01);
      float t = 1.0 - smoothstep(-blur, blur, d);
      result = premul(v_color) * (t * t);
    } else {
      float cov = clamp(0.5 - d, 0.0, 1.0);
      float bw = v_params.x;
      if (bw > 0.0) {
        float inner = clamp(0.5 - (d + bw), 0.0, 1.0);
        result = premul(v_color) * inner + premul(v_border) * (cov - inner);
      } else {
        result = premul(v_color) * cov;
      }
    }
  }

  o_color = result * clipCov;
}`;

// vertex shader for custom-shader passes: layer composites and shader quads
export const QUAD_VERT = `#version 300 es
precision highp float;

layout(location=0) in vec2 a_unit;
uniform vec4 u_rect;   // x, y, w, h on the target, logical px
uniform vec4 u_uvrect; // uv sub-range (clipped shader quads)
uniform vec2 u_view;
uniform vec2 u_origin;
uniform float u_flipY;
out vec2 v_uv;

void main() {
  vec2 pos = u_rect.xy + a_unit * u_rect.zw;
  v_uv = mix(u_uvrect.xy, u_uvrect.zw, a_unit);
  vec2 ndc = (pos - u_origin) / u_view * 2.0 - 1.0;
  gl_Position = vec4(ndc.x, u_flipY * ndc.y, 0.0, 1.0);
}`;

/**
 * Wrap app-supplied GLSL. The app defines `vec4 effect(vec2 uv)`, uv has
 * (0,0) at the top-left, and may use u_time (seconds), u_res (logical px),
 * declared custom float uniforms, and for layer effects `src(uv)`: the
 * widget subtree rendered to a texture (premultiplied alpha, transparent
 * outside [0,1])
 */
export function wrapUserFrag(frag: string, uniformNames: string[], withContent: boolean): string {
  const decls = uniformNames
    .filter((n) => /^[a-zA-Z_][a-zA-Z0-9_]*$/.test(n))
    .map((n) => `uniform float ${n};`)
    .join('\n');
  const src = withContent
    ? `uniform sampler2D u_tex;
vec4 src(vec2 uv) {
  if (uv.x < 0.0 || uv.y < 0.0 || uv.x > 1.0 || uv.y > 1.0) return vec4(0.0);
  return texture(u_tex, uv);
}`
    : '';
  return `#version 300 es
precision highp float;
uniform vec2 u_res;
uniform float u_time;
in vec2 v_uv;
out vec4 o_color;
${src}
${decls}
${frag}
void main() { o_color = effect(v_uv); }`;
}

/** fallback when an app effect fails to compile: composite the layer unchanged */
export const PASSTHROUGH_FRAG = `vec4 effect(vec2 uv) { return src(uv); }`;
