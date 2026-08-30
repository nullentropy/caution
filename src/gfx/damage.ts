import type { Color } from './color';
import { inflate, intersect, overlaps, union, type Corners, type Rect } from './geom';
import type { Font } from './font';
import type {
  Cmd,
  DisplayList,
  ImageCmd,
  LayerBeginCmd,
  LayerEndCmd,
  RectCmd,
  ShaderQuadCmd,
  ShadowCmd,
  TextCmd,
} from './painter';

/**
 * Mirrored in the native terminal (go/terminal/gfx/damage.go).
 */

/** one beginLayer..endLayer range by command index */
export interface LayerSpan {
  /** index of the matching layer-end command */
  end: number;
  /**
   * Nothing strictly inside the range animates: the layer's texture is
   * identical on every replay of this list, so partial frames keep it and
   * re-run only the composite. The layer-end's own `animated` flag doesn't
   * break cacheability.
   */
  cacheable: boolean;
}

export interface DamagePlan {
  /**
   * Merged damage regions: the rects of animated commands, each promoted to
   * its outermost enclosing layer (a composite shader may map any texel
   * anywhere, so damage inside a layer dirties the whole composite
   * footprint). Empty means the list has no self-animating content and
   * cannot drive partial frames.
   */
  rects: Rect[];
  /** each layer-begin command index -> its span */
  layers: Map<number, LayerSpan>;
}

/** scissor passes per partial frame are capped */
const MAX_DAMAGE_RECTS = 12;

/** scan a display list once. view clamps damage to the viewport. */
export function planDamage(dl: DisplayList, view: Rect): DamagePlan {
  const layers = new Map<number, LayerSpan>();
  const open: number[] = [];
  const cacheable = new Map<number, boolean>();
  let rects: Rect[] = [];

  // record an animated command's footprint, promoted to the outermost
  // enclosing layer when nested
  const damage = (r: Rect): void => {
    const outer = open.length ? dl.cmds[open[0]!] : undefined;
    if (outer?.kind === 'layer-begin') r = outer.rect;
    r = intersect(r, view);
    if (r.w > 0 && r.h > 0) rects.push(r);
  };
  // an animated command inside a layer means that layer's texture changes
  // every frame so it and every enclosing layer stop being cacheable
  const poison = (): void => {
    for (const b of open) cacheable.set(b, false);
  };

  for (let i = 0; i < dl.cmds.length; i++) {
    const c = dl.cmds[i]!;
    switch (c.kind) {
      case 'layer-begin':
        open.push(i);
        cacheable.set(i, true);
        break;
      case 'layer-end': {
        const begin = open.pop();
        if (begin == null) break; // unbalanced. paintTree can't produce this
        layers.set(begin, { end: i, cacheable: cacheable.get(begin) ?? false });
        if (c.animated) {
          poison();
          damage(c.rect);
        }
        break;
      }
      case 'shader-quad':
        if (c.animated) {
          poison();
          damage(c.rect);
        }
        break;
    }
  }

  return { rects: mergeRects(rects), layers };
}

/**
 * Union overlapping rects to a fixpoint (a rect never needs replaying twice,
 * though scissored clear-and-replay is idempotent, so merging is an efficiency
 * choice, not a correctness one), then apply the pass cap.
 */
function mergeRects(rects: Rect[]): Rect[] {
  for (;;) {
    let merged = false;
    outer: for (let i = 0; i < rects.length; i++) {
      for (let j = i + 1; j < rects.length; j++) {
        if (overlaps(rects[i]!, rects[j]!)) {
          rects[i] = union(rects[i]!, rects[j]!);
          rects.splice(j, 1);
          merged = true;
          break outer;
        }
      }
    }
    if (!merged) break;
  }
  return rects.length > MAX_DAMAGE_RECTS ? [rects.reduce(union)] : rects;
}

/**
 * The slack around a measured line box that covers ink the box does not:
 * accents and marks that reach above the face's ascent, left and right side
 * bearings, italic overhang.
 */
export const textInk = (f: Font): number => Math.max(2, f.size);

/**
 * Conservative bound on the pixels a command can write: the vertex shader
 * inflates rect-mode quads by 1px for AA and shadow quads by blur*2.5+1
 * (see webgl/shaders.ts), and the fragment clip has ~1px of AA slack. Text is
 * its measured line box plus textInk, or (when the list was built without a
 * `measure` hook) its clip alone, which for unclipped text matches every
 * damage rect. Including too much is correct, just slower; excluding too much
 * is a stale pixel.
 */
export function paintedBounds(c: Cmd): Rect {
  switch (c.kind) {
    case 'text':
      if (c.box.w > 0 && c.box.h > 0) {
        return intersect(inflate(c.box, textInk(c.font)), inflate(c.clip, 1));
      }
      return inflate(c.clip, 1);
    case 'shadow':
      return intersect(inflate(c.rect, c.blur * 2.5 + 1), inflate(c.clip, 1));
    case 'shader-quad':
    case 'layer-begin':
    case 'layer-end':
      // these record no clip: a shader quad's rect is clip-intersected at
      // record time
      return c.rect;
    default:
      return intersect(inflate(c.rect, 1), inflate(c.clip, 1));
  }
}

// -- real damage -------------------------------------------------------------

/**
 * Bounds the accumulator while diffing. A frame that changed everything (a
 * theme tween) must not pay a quadratic merge, so past this many regions the
 * accumulator collapses to its bounding box and the area guard below decides
 * whether scissoring is still worth it.
 */
const MAX_DIFF_RECTS = 64;

/** The fraction of the view past which a partial frame stops paying. */
const DIFF_AREA_LIMIT = 0.6;

/**
 * Compare the list about to be rendered against the one whose pixels the
 * scene framebuffer already holds, and return the regions where the two can
 * differ: the painted bounds of every command that changed, taken from *both*
 * lists (a command that moved dirties where it was and where it went) and
 * promoted to the outermost enclosing layer, since a composite shader may map
 * any texel anywhere in its output.
 *
 * Outside those rects, every command that touches a pixel is identical and in
 * the same painter's order, so the scene already holds the right pixels. That is
 * what lets an op touching one label repaint one label instead of the world.
 *
 * `animated` seeds the result with the new list's animation damage: those
 * commands are identical frame to frame, but their u_time is not.
 *
 * null means "render the whole frame instead": nothing retained, layer
 * structure moved (the one case the index-aligned walk cannot reason about),
 * or the damage covers so much of the view that scissoring it is a loss.
 */
export function diffDamage(
  prev: DisplayList | null,
  next: DisplayList,
  view: Rect,
  animated: readonly Rect[],
): Rect[] | null {
  if (!prev) return null;
  const pc = prev.cmds;
  const nc = next.cmds;
  let rects: Rect[] = [...animated];
  if (pc.length === nc.length) {
    // the common case: a widget repainting itself produces the same shape of
    // list, so the two are index-aligned and every difference is exact
    const pOpen: number[] = [];
    const nOpen: number[] = [];
    for (let i = 0; i < pc.length; i++) {
      if (!cmdEqual(pc[i]!, nc[i]!)) {
        if (isLayerCmd(pc[i]!) !== isLayerCmd(nc[i]!)) return null;
        rects = addDamage(rects, pc, pOpen, i, view);
        rects = addDamage(rects, nc, nOpen, i, view);
      }
      trackLayers(pOpen, pc, i);
      trackLayers(nOpen, nc, i);
    }
  } else {
    // a command appeared or vanished, so indexes past it shifted: trim the
    // common prefix and suffix and damage everything between, in both lists
    const n = Math.min(pc.length, nc.length);
    let p = 0;
    while (p < n && cmdEqual(pc[p]!, nc[p]!)) p++;
    let suf = 0;
    while (suf < n - p && cmdEqual(pc[pc.length - 1 - suf]!, nc[nc.length - 1 - suf]!)) suf++;
    if (pc.slice(p, pc.length - suf).some(isLayerCmd) || nc.slice(p, nc.length - suf).some(isLayerCmd)) {
      return null; // as above: the target stack itself moved
    }
    rects = addRange(rects, pc, p, pc.length - suf, view);
    rects = addRange(rects, nc, p, nc.length - suf, view);
  }
  rects = mergeRects(rects);
  let area = 0;
  for (const r of rects) area += r.w * r.h;
  if (area > DIFF_AREA_LIMIT * view.w * view.h) return null;
  return rects;
}

const isLayerCmd = (c: Cmd): boolean => c.kind === 'layer-begin' || c.kind === 'layer-end';

/**
 * Maintain the open-layer stack across a single-pass walk. A layer-end closes
 * at its own index, so a walker damages *before* calling this.
 */
function trackLayers(open: number[], cmds: readonly Cmd[], i: number): void {
  const k = cmds[i]!.kind;
  if (k === 'layer-begin') open.push(i);
  else if (k === 'layer-end') open.pop();
}

function addDamage(out: Rect[], cmds: readonly Cmd[], open: readonly number[], i: number, view: Rect): Rect[] {
  const outer = open.length ? cmds[open[0]!]! : null;
  let r = outer?.kind === 'layer-begin' ? outer.rect : paintedBounds(cmds[i]!);
  r = intersect(r, view);
  if (r.w <= 0 || r.h <= 0) return out;
  out.push(r);
  if (out.length > MAX_DIFF_RECTS) return [out.reduce(union)];
  return out;
}

/**
 * Damage cmds[lo, hi). The layer stack has to be re-derived from the start of
 * the list so a range's enclosing layers are whatever is open when the walk
 * reaches it.
 */
function addRange(out: Rect[], cmds: readonly Cmd[], lo: number, hi: number, view: Rect): Rect[] {
  const open: number[] = [];
  for (let i = 0; i < hi; i++) {
    if (i >= lo) out = addDamage(out, cmds, open, i, view);
    trackLayers(open, cmds, i);
  }
  return out;
}

const rectEq = (a: Rect, b: Rect): boolean => a === b || (a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h);
const colorEq = (a: Color, b: Color): boolean => a === b || (a.r === b.r && a.g === b.g && a.b === b.b && a.a === b.a);
const cornersEq = (a: Corners, b: Corners): boolean =>
  a === b || (a.tl === b.tl && a.tr === b.tr && a.br === b.br && a.bl === b.bl);

function uniformsEq(a: Record<string, number>, b: Record<string, number>): boolean {
  if (a === b) return true;
  const ka = Object.keys(a);
  if (ka.length !== Object.keys(b).length) return false;
  for (const k of ka) if (a[k] !== b[k]) return false;
  return true;
}

/**
 * Whether two commands paint identically.
 */
export function cmdEqual(a: Cmd, b: Cmd): boolean {
  if (a === b) return true;
  if (a.kind !== b.kind) return false;
  switch (a.kind) {
    case 'rect': {
      const o = b as RectCmd;
      return (
        rectEq(a.rect, o.rect) && cornersEq(a.radii, o.radii) && colorEq(a.color, o.color) &&
        a.borderWidth === o.borderWidth && colorEq(a.borderColor, o.borderColor) && rectEq(a.clip, o.clip)
      );
    }
    case 'shadow': {
      const o = b as ShadowCmd;
      return (
        rectEq(a.rect, o.rect) && cornersEq(a.radii, o.radii) && colorEq(a.color, o.color) &&
        a.blur === o.blur && rectEq(a.clip, o.clip)
      );
    }
    case 'text': {
      const o = b as TextCmd;
      return (
        a.x === o.x && a.y === o.y && a.text === o.text && a.font.key === o.font.key &&
        colorEq(a.color, o.color) && rectEq(a.clip, o.clip) && rectEq(a.box, o.box)
      );
    }
    case 'layer-begin':
      return rectEq(a.rect, (b as LayerBeginCmd).rect);
    case 'layer-end': {
      const o = b as LayerEndCmd;
      return rectEq(a.rect, o.rect) && a.frag === o.frag && a.animated === o.animated && uniformsEq(a.uniforms, o.uniforms);
    }
    case 'shader-quad': {
      const o = b as ShaderQuadCmd;
      return (
        rectEq(a.rect, o.rect) && a.frag === o.frag && a.animated === o.animated &&
        a.uv[0] === o.uv[0] && a.uv[1] === o.uv[1] && a.uv[2] === o.uv[2] && a.uv[3] === o.uv[3] &&
        uniformsEq(a.uniforms, o.uniforms)
      );
    }
    case 'image': {
      const o = b as ImageCmd;
      return (
        rectEq(a.rect, o.rect) && a.src === o.src && a.fit === o.fit &&
        cornersEq(a.radii, o.radii) && rectEq(a.clip, o.clip)
      );
    }
  }
}
