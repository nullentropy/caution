/** axis-aligned rectangle in logical pixels */
export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export const rect = (x: number, y: number, w: number, h: number): Rect => ({ x, y, w, h });

export function intersect(a: Rect, b: Rect): Rect {
  const x = Math.max(a.x, b.x);
  const y = Math.max(a.y, b.y);
  const right = Math.min(a.x + a.w, b.x + b.w);
  const bottom = Math.min(a.y + a.h, b.y + b.h);
  return { x, y, w: Math.max(0, right - x), h: Math.max(0, bottom - y) };
}

export const inset = (r: Rect, d: number): Rect => ({
  x: r.x + d,
  y: r.y + d,
  w: r.w - d * 2,
  h: r.h - d * 2,
});

export const inflate = (r: Rect, d: number): Rect => inset(r, -d);

/** whether two rects share any interior area (edge-touching rects don't) */
export const overlaps = (a: Rect, b: Rect): boolean =>
  a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** bounding rect of both */
export function union(a: Rect, b: Rect): Rect {
  const x = Math.min(a.x, b.x);
  const y = Math.min(a.y, b.y);
  const right = Math.max(a.x + a.w, b.x + b.w);
  const bottom = Math.max(a.y + a.h, b.y + b.h);
  return { x, y, w: right - x, h: bottom - y };
}

/** sentinel "no clipping" rect. large enough for any viewport, small enough for f32 */
export const NO_CLIP: Rect = { x: -1e6, y: -1e6, w: 2e6, h: 2e6 };

/** per-corner radii, clockwise from top-left */
export interface Corners {
  tl: number;
  tr: number;
  br: number;
  bl: number;
}

export const corners = (r: number): Corners => ({ tl: r, tr: r, br: r, bl: r });

export type Radius = number | Corners;

export const resolveRadius = (r: Radius | undefined): Corners =>
  typeof r === 'number' ? corners(r) : r ?? corners(0);
