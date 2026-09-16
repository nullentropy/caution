import type { Font } from '../font';
import type { AtlasGlyph } from './atlas';
import type { ShapedGlyph, TextRun } from './shaper';

/**
 * The text engine the renderer draws with: the terminal's shaper and outline
 * rasterizer (go-text over the embedded fonts), reached through the wasm
 * module's text exports. Same engine, fonts, and pixels as the native
 * terminal.
 */
export interface TextEngine {
  shape(f: Font, dpr: number, text: string): TextRun;
  glyph(f: Font, dpr: number, g: ShapedGlyph, phase: number): AtlasGlyph | null;
  /** upload the atlas into the *bound* texture if it changed since last call */
  upload(gl: WebGL2RenderingContext): boolean;
  /** DPR changed: drop every rasterized glyph */
  reset(): void;
}
