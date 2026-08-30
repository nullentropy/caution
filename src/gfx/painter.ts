import type { Color } from './color';
import type { Font } from './font';
import { Corners, NO_CLIP, Radius, Rect, intersect, resolveRadius } from './geom';

/** shared "unmeasured" box, so every text command keeps one object shape */
const ZERO_RECT: Rect = { x: 0, y: 0, w: 0, h: 0 };

export interface RectCmd {
  kind: 'rect';
  rect: Rect;
  radii: Corners;
  color: Color;
  borderWidth: number;
  borderColor: Color;
  clip: Rect;
}

export interface ShadowCmd {
  kind: 'shadow';
  rect: Rect;
  radii: Corners;
  color: Color;
  blur: number;
  clip: Rect;
}

export interface TextCmd {
  kind: 'text';
  /** top-left of the line box. the renderer derives the baseline from font metrics */
  x: number;
  y: number;
  text: string;
  font: Font;
  color: Color;
  clip: Rect;
  /**
   * The measured line box, when the list was built with a `measure` hook,
   * a text command's painted extent, which is what lets a partial frame skip
   * the text it does not touch. Zero-sized means "unmeasured": the extent
   * falls back to the clip.
   */
  box: Rect;
}

/** begin rendering into an offscreen layer (pixel-snapped for glyph crispness) */
export interface LayerBeginCmd {
  kind: 'layer-begin';
  rect: Rect;
}

/** composite the layer back through a custom fragment shader */
export interface LayerEndCmd {
  kind: 'layer-end';
  rect: Rect;
  frag: string;
  uniforms: Record<string, number>;
  /** continuous u_time animation (see damage.ts) */
  animated: boolean;
}

/** a quad painted entirely by a custom fragment shader */
export interface ShaderQuadCmd {
  kind: 'shader-quad';
  rect: Rect;
  /** uv sub-range so ambient clipping doesn't distort the shader's space */
  uv: [number, number, number, number];
  frag: string;
  uniforms: Record<string, number>;
  /** continuous u_time animation (see damage.ts) */
  animated: boolean;
}

/** a bitmap image drawn from the async texture store, with aspect fitting */
export interface ImageCmd {
  kind: 'image';
  rect: Rect;
  src: string;
  fit: 'contain' | 'cover' | 'fill';
  radii: Corners;
  clip: Rect;
}

export type Cmd = RectCmd | ShadowCmd | TextCmd | LayerBeginCmd | LayerEndCmd | ShaderQuadCmd | ImageCmd;

/**
 * Retained draw-command list in logical pixels, painter's-algorithm ordered.
 * Clips are resolved (intersected) at record time so the renderer never
 * replays a stack.
 */
export class DisplayList {
  readonly cmds: Cmd[] = [];
  /**
   * True while geometric animations (slides/fades) are mid-flight this
   * frame. Those move widget bounds every frame, so a retained list must
   * not be replayed, every frame rebuilds until they settle.
   */
  geomAnimation = false;

  /**
   * Whether this frame asks for another one: any command wanting continuous
   * u_time, or a geometric tween mid-flight.
   *
   * Derived from the commands rather than accumulated while recording them. An
   * accumulator only sees the commands a frame actually built, and a spliced
   * subtree copies its retained commands without calling shaderQuad or
   * endLayer.
   */
  get wantsAnimation(): boolean {
    if (this.geomAnimation) return true;
    for (const c of this.cmds) {
      if ((c.kind === 'shader-quad' || c.kind === 'layer-end') && c.animated) return true;
    }
    return false;
  }
  /**
   * Returns a string's advance width and vertical metrics, so `text` can
   * record the line box it will paint into. Without it text commands carry no
   * box and partial frames fall back to replaying them whenever their clip is
   * in range. The UI wires it to the shaper.
   */
  measure: ((f: Font, s: string) => { w: number; ascent: number; descent: number }) | null = null;
  /**
   * The theme-mutation counter this list was recorded under (see
   * ui/theme.ts). Token Colors mutate in place, so two lists built either side
   * of a theme change cannot be compared command by command.
   */
  palette = 0;
  private clips: Rect[] = [];
  private layers: Rect[] = [];

  /**
   * The ambient clip commands recorded right now will carry. A retained
   * subtree's commands are only reusable under the clip they were recorded
   * with, so the paint layer compares against this.
   */
  get clip(): Rect {
    return this.clips.length ? this.clips[this.clips.length - 1]! : NO_CLIP;
  }

  rect(
    r: Rect,
    opts: { color: Color; radius?: Radius; borderWidth?: number; borderColor?: Color },
  ): void {
    this.cmds.push({
      kind: 'rect',
      rect: r,
      radii: resolveRadius(opts.radius),
      color: opts.color,
      borderWidth: opts.borderWidth ?? 0,
      borderColor: opts.borderColor ?? opts.color,
      clip: this.clip,
    });
  }

  shadow(
    r: Rect,
    opts: { color: Color; blur: number; radius?: Radius; offset?: [number, number] },
  ): void {
    const [dx, dy] = opts.offset ?? [0, 0];
    this.cmds.push({
      kind: 'shadow',
      rect: { x: r.x + dx, y: r.y + dy, w: r.w, h: r.h },
      radii: resolveRadius(opts.radius),
      color: opts.color,
      blur: opts.blur,
      clip: this.clip,
    });
  }

  text(text: string, x: number, y: number, f: Font, color: Color): void {
    let box = ZERO_RECT;
    if (this.measure) {
      const m = this.measure(f, text);
      box = { x, y, w: m.w, h: m.ascent + m.descent };
    }
    this.cmds.push({ kind: 'text', x, y, text, font: f, color, clip: this.clip, box });
  }

  image(r: Rect, src: string, fit: 'contain' | 'cover' | 'fill', radius?: Radius): void {
    this.cmds.push({ kind: 'image', rect: r, src, fit, radii: resolveRadius(radius), clip: this.clip });
  }

  pushClip(r: Rect): void {
    this.clips.push(intersect(this.clip, r));
  }

  popClip(): void {
    this.clips.pop();
  }

  /**
   * Everything until the matching endLayer renders into an offscreen texture.
   * The rect is snapped to whole logical pixels so glyphs stay crisp.
   */
  beginLayer(r: Rect): void {
    const x = Math.floor(r.x);
    const y = Math.floor(r.y);
    const snapped = { x, y, w: Math.ceil(r.x + r.w) - x, h: Math.ceil(r.y + r.h) - y };
    this.layers.push(snapped);
    this.cmds.push({ kind: 'layer-begin', rect: snapped });
  }

  endLayer(frag: string, uniforms: Record<string, number>, animate: boolean): void {
    const r = this.layers.pop();
    if (!r) return;
    this.cmds.push({ kind: 'layer-end', rect: r, frag, uniforms, animated: animate });
  }

  shaderQuad(r: Rect, frag: string, uniforms: Record<string, number>, animate: boolean): void {
    const c = intersect(this.clip, r);
    if (c.w <= 0 || c.h <= 0 || r.w <= 0 || r.h <= 0) return;
    this.cmds.push({
      kind: 'shader-quad',
      rect: c,
      uv: [(c.x - r.x) / r.w, (c.y - r.y) / r.h, (c.x + c.w - r.x) / r.w, (c.y + c.h - r.y) / r.h],
      frag,
      uniforms,
      animated: animate,
    });
  }
}
