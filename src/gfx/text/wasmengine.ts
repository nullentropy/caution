import { Font, MONO } from '../font';
import { ATLAS_SIZE, AtlasGlyph } from './atlas';
import type { TextEngine } from './engine';
import { clusterSlices, ShapedGlyph, TextRun } from './shaper';

/** the text exports cmd/termwasm hangs on globalThis.__cautionText (all synchronous) */
export interface WasmText {
  shape(size: number, weight: number, italic: boolean, mono: boolean, dpr: number, text: string): number[];
  glyph(
    size: number, weight: number, italic: boolean, mono: boolean,
    dpr: number, gid: number, phase: number, emoji: boolean,
  ): number[];
  atlasTake(dst: Uint8Array): boolean;
  reset(): void;
}

export class WasmEngine implements TextEngine {
  private runs = new Map<string, TextRun>();
  private glyphs = new Map<string, AtlasGlyph | null>();
  private pix = new Uint8Array(ATLAS_SIZE * ATLAS_SIZE * 4);

  constructor(private api: WasmText) {}

  shape(f: Font, dpr: number, text: string): TextRun {
    const key = `${f.key}|${dpr}|${text}`;
    const hit = this.runs.get(key);
    if (hit) return hit;
    if (this.runs.size > 4000) this.runs.clear();

    const mono = f.family === MONO;
    const flat = this.api.shape(f.size, f.weight, f.italic, mono, dpr, text);
    // cluster indices arrive as rune offsets (Go strings)
    // selection math here runs in code units
    // walk the string once to translate
    const cuOf: number[] = [];
    for (let cu = 0; cu < text.length; ) {
      cuOf.push(cu);
      cu += (text.codePointAt(cu) ?? 0) > 0xffff ? 2 : 1;
    }
    cuOf.push(text.length);

    const glyphs: ShapedGlyph[] = [];
    for (let i = 3; i + 3 < flat.length; i += 4) {
      glyphs.push({
        gid: flat[i]!,
        x: flat[i + 1]!,
        cluster: cuOf[flat[i + 2]!] ?? text.length,
        emoji: flat[i + 3] === 1,
        ch: '',
      });
    }
    // ch is the cluster's source slice: selection walks accumulate
    // ch.length, so ligature glyphs advance several code units at once and
    // mark glyphs (same cluster) advance zero. clusterSlices is bidi-aware
    // RTL runs arrive in visual order with descending clusters
    clusterSlices(glyphs, text);

    const run: TextRun = {
      glyphs,
      width: flat[0] ?? 0,
      ascent: flat[1] ?? 0,
      descent: flat[2] ?? 0,
    };
    this.runs.set(key, run);
    return run;
  }

  glyph(f: Font, dpr: number, g: ShapedGlyph, phase: number): AtlasGlyph | null {
    if (g.gid == null) return null;
    const key = `${f.key}|${dpr}|${phase}|${g.gid}|${g.emoji ? 1 : 0}`;
    const hit = this.glyphs.get(key);
    if (hit !== undefined) return hit;
    const mono = f.family === MONO;
    const r = this.api.glyph(f.size, f.weight, f.italic, mono, dpr, g.gid, phase, g.emoji ?? false);
    const glyph: AtlasGlyph | null =
      r.length < 8
        ? null
        : {
            x0: r[0]!,
            y0: r[1]!,
            x1: r[2]!,
            y1: r[3]!,
            w: r[4]!,
            h: r[5]!,
            bx: r[6]!,
            by: r[7]!,
            colored: false,
          };
    this.glyphs.set(key, glyph);
    return glyph;
  }

  upload(gl: WebGL2RenderingContext): boolean {
    if (!this.api.atlasTake(this.pix)) return false;
    // already premultiplied (coverage in every channel), no UNPACK flag.
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, ATLAS_SIZE, ATLAS_SIZE, 0, gl.RGBA, gl.UNSIGNED_BYTE, this.pix);
    return true;
  }

  reset(): void {
    this.api.reset();
    this.glyphs.clear();
    this.runs.clear();
  }
}
