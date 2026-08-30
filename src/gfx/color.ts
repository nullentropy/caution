/** Straight (non-premultiplied) RGBA, all channels 0..1. Premultiplication happens in the shader. */
export interface Color {
  r: number;
  g: number;
  b: number;
  a: number;
}

export const rgba = (r: number, g: number, b: number, a = 1): Color => ({ r, g, b, a });

/** Parse '#rgb', '#rrggbb', or '#rrggbbaa'. */
export function hex(s: string): Color {
  let h = s.replace('#', '');
  if (h.length === 3) h = h.split('').map((c) => c + c).join('');
  const n = parseInt(h.slice(0, 6), 16);
  const a = h.length === 8 ? parseInt(h.slice(6, 8), 16) / 255 : 1;
  return rgba(((n >> 16) & 0xff) / 255, ((n >> 8) & 0xff) / 255, (n & 0xff) / 255, a);
}

export const withAlpha = (c: Color, a: number): Color => ({ ...c, a });

/** Linear blend a->b by t (0..1), all channels including alpha. */
export const mix = (a: Color, b: Color, t: number): Color =>
  rgba(a.r + (b.r - a.r) * t, a.g + (b.g - a.g) * t, a.b + (b.b - a.b) * t, a.a + (b.a - a.a) * t);

export const WHITE = rgba(1, 1, 1, 1);
export const BLACK = rgba(0, 0, 0, 1);
export const TRANSPARENT = rgba(0, 0, 0, 0);
