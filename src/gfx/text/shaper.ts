import { Font, cssFont } from '../font';

export interface ShapedGlyph {
  /**
   * The cluster's source text. Canvas shaping emits one glyph per code
   * point. The wasm engine may merge several (ligatures) or emit empty
   * follow-ups (marks).
   */
  ch: string;
  /** pen offset from the run origin, logical px */
  x: number;
  /** source offset of the cluster, in code units */
  cluster: number;
  /** glyph id in the engine's face (wasm engine only) */
  gid?: number;
  /** the glyph shaped with the emoji fallback face (wasm engine only). its gid belongs to that face. */
  emoji?: boolean;
}

export interface TextRun {
  glyphs: ShapedGlyph[];
  width: number;
  ascent: number;
  descent: number;
}

/**
 * Fill each glyph's `ch` with its cluster's source slice. Order-independent:
 * the glyph array is VISUAL order under bidi (an RTL run's clusters descend),
 * so a cluster's logical extent runs to the smallest cluster start greater
 * than its own, not to the next glyph in the array. Within one cluster
 * (ligature follow-ups, marks) only the last glyph carries the slice.
 */
export function clusterSlices(glyphs: ShapedGlyph[], text: string): void {
  const starts = [...new Set(glyphs.map((g) => g.cluster))].sort((a, b) => a - b);
  const nextOf = new Map<number, number>();
  for (let i = 0; i < starts.length; i++) nextOf.set(starts[i]!, starts[i + 1] ?? text.length);
  for (let i = 0; i < glyphs.length; i++) {
    const g = glyphs[i]!;
    const lastOfCluster = i + 1 >= glyphs.length || glyphs[i + 1]!.cluster !== g.cluster;
    g.ch = lastOfCluster ? text.slice(g.cluster, nextOf.get(g.cluster) ?? text.length) : '';
  }
}

/**
 * Stage-1 "shaping": per-code-point positions via cumulative prefix measurement,
 * which preserves kerning but loses ligatures and complex-script shaping. Measured
 * at device-pixel size so positions agree with atlas rasterization.
 */
export class TextShaper {
  private ctx: CanvasRenderingContext2D;
  private cache = new Map<string, TextRun>();

  constructor() {
    const ctx = document.createElement('canvas').getContext('2d');
    if (!ctx) throw new Error('Canvas2D unavailable (needed for text measurement)');
    this.ctx = ctx;
  }

  shape(f: Font, dpr: number, text: string): TextRun {
    const key = `${f.key}|${dpr}|${text}`;
    const hit = this.cache.get(key);
    if (hit) return hit;
    if (this.cache.size > 4000) this.cache.clear();

    const ctx = this.ctx;
    ctx.font = cssFont(f, dpr);
    const whole = ctx.measureText(text);
    const ascent = (whole.fontBoundingBoxAscent ?? f.size * dpr * 0.8) / dpr;
    const descent = (whole.fontBoundingBoxDescent ?? f.size * dpr * 0.25) / dpr;

    const glyphs: ShapedGlyph[] = [];
    let prefix = '';
    for (const ch of text) {
      glyphs.push({ ch, x: prefix ? ctx.measureText(prefix).width / dpr : 0, cluster: prefix.length });
      prefix += ch;
    }

    const run: TextRun = { glyphs, width: whole.width / dpr, ascent, descent };
    this.cache.set(key, run);
    return run;
  }
}
