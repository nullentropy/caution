import { Font, cssFont } from '../font';

export interface AtlasGlyph {
  /** atlas region in device pixels (includes 1px transparent pad on each side) */
  x0: number;
  y0: number;
  x1: number;
  y1: number;
  w: number;
  h: number;
  /** offset from the (device-px) pen position at the baseline to the quad's top-left */
  bx: number;
  by: number;
  /** color glyph (emoji). must not be tinted by the text color */
  colored: boolean;
}

const EMOJI_RE = /\p{Extended_Pictographic}/u;

export const ATLAS_SIZE = 2048;

/**
 * Horizontal subpixel phases per glyph. Snapping every glyph to a whole
 * device pixel keeps each one crisp but quantizes the gaps between them, so
 * inter-letter spacing wobbles by up to half a pixel and the text reads as
 * badly kerned. Instead each glyph is rasterized at PHASES fractional
 * offsets and the renderer picks the nearest: spacing lands within 1/PHASES
 * of its true position, and the bitmap is still rasterized where it
 * is drawn, so it stays sharp.
 */
export const PHASES = 4;

/**
 * Canvas2D-rasterized glyph cache with shelf packing. Glyphs are drawn white on
 * transparent at device-pixel size; the shader tints them (emoji excepted).
 * When full it resets wholesale - cheap, and acceptable until incremental
 * eviction is needed.
 */
export class GlyphAtlas {
  readonly canvas: HTMLCanvasElement;
  /** set when the canvas has new glyphs the GPU texture hasn't seen */
  dirty = false;

  private ctx: CanvasRenderingContext2D;
  private map = new Map<string, AtlasGlyph | null>();
  private x = 1;
  private y = 1;
  private rowH = 0;

  constructor() {
    this.canvas = document.createElement('canvas');
    this.canvas.width = ATLAS_SIZE;
    this.canvas.height = ATLAS_SIZE;
    const ctx = this.canvas.getContext('2d');
    if (!ctx) throw new Error('Canvas2D unavailable (needed for glyph rasterization)');
    this.ctx = ctx;
    this.ctx.textBaseline = 'alphabetic';
  }

  /** `phase` is a subpixel offset index in [0, PHASES). */
  get(f: Font, dpr: number, ch: string, phase = 0): AtlasGlyph | null {
    const key = `${f.key}|${dpr}|${phase}|${ch}`;
    const hit = this.map.get(key);
    if (hit !== undefined) return hit;

    const off = phase / PHASES;
    const ctx = this.ctx;
    ctx.font = cssFont(f, dpr);
    ctx.fillStyle = '#ffffff';
    const m = ctx.measureText(ch);
    const left = Math.ceil(m.actualBoundingBoxLeft);
    // the subpixel shift moves ink right, so the box has to grow with it
    const right = Math.ceil(m.actualBoundingBoxRight + off);
    const asc = Math.ceil(m.actualBoundingBoxAscent);
    const desc = Math.ceil(m.actualBoundingBoxDescent);

    // no ink (spaces etc.). cache the miss so we skip measurement next time
    if (left + right <= 0 || asc + desc <= 0) {
      this.map.set(key, null);
      return null;
    }

    const w = left + right + 2;
    const h = asc + desc + 2;
    if (w > ATLAS_SIZE - 2 || h > ATLAS_SIZE - 2) return null;

    if (this.x + w + 1 > ATLAS_SIZE) {
      this.x = 1;
      this.y += this.rowH + 1;
      this.rowH = 0;
    }
    if (this.y + h + 1 > ATLAS_SIZE) {
      console.warn('caution: glyph atlas full, resetting');
      this.reset();
      return this.get(f, dpr, ch, phase);
    }

    const gx = this.x;
    const gy = this.y;
    this.x += w + 1;
    this.rowH = Math.max(this.rowH, h);

    ctx.fillText(ch, gx + 1 + left + off, gy + 1 + asc);

    const glyph: AtlasGlyph = {
      x0: gx,
      y0: gy,
      x1: gx + w,
      y1: gy + h,
      w,
      h,
      bx: -(left + 1),
      by: -(asc + 1),
      colored: EMOJI_RE.test(ch),
    };
    this.map.set(key, glyph);
    this.dirty = true;
    return glyph;
  }

  reset(): void {
    this.map.clear();
    this.ctx.clearRect(0, 0, ATLAS_SIZE, ATLAS_SIZE);
    this.x = 1;
    this.y = 1;
    this.rowH = 0;
    this.dirty = true;
  }
}
