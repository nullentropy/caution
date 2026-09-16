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
