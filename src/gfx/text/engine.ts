import type { Font } from '../font';
import { AtlasGlyph, GlyphAtlas } from './atlas';
import { ShapedGlyph, TextRun, TextShaper } from './shaper';

/**
 * The text engine. Two implementations:
 *
 * - CanvasEngine (the fallback): Canvas2D `measureText` positions and
 *   `fillText` rasterization over the system font stack. Correct for
 *   Latin-ish scripts but wrong for ligatures and complex shaping.
 * - WasmEngine: the native terminal's shaper + outline rasterizer
 *   (go-text over the embedded Go fonts) compiled to WebAssembly. Same
 *   engine, fonts, and pixels as the native terminal.
 *
 * The choice happens at boot (see terminal.ts).
 */
export interface TextEngine {
  shape(f: Font, dpr: number, text: string): TextRun;
  glyph(f: Font, dpr: number, g: ShapedGlyph, phase: number): AtlasGlyph | null;
  /** upload the atlas into the *bound* texture if it changed since last call */
  upload(gl: WebGL2RenderingContext): boolean;
  /** DPR changed: drop every rasterized glyph */
  reset(): void;
}

export class CanvasEngine implements TextEngine {
  private shaper = new TextShaper();
  private atlas = new GlyphAtlas();

  shape(f: Font, dpr: number, text: string): TextRun {
    return this.shaper.shape(f, dpr, text);
  }

  glyph(f: Font, dpr: number, g: ShapedGlyph, phase: number): AtlasGlyph | null {
    return this.atlas.get(f, dpr, g.ch, phase);
  }

  upload(gl: WebGL2RenderingContext): boolean {
    if (!this.atlas.dirty) return false;
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, true);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, this.atlas.canvas);
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false);
    this.atlas.dirty = false;
    return true;
  }

  reset(): void {
    this.atlas.reset();
  }
}
