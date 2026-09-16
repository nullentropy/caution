export interface ShapedGlyph {
  /**
   * The cluster's source text. Shaping may merge several code points into
   * one glyph (ligatures) or emit empty follow-ups (marks).
   */
  ch: string;
  /** pen offset from the run origin, logical px */
  x: number;
  /** source offset of the cluster, in code units */
  cluster: number;
  /** glyph id in the engine's face */
  gid?: number;
  /** the glyph shaped with the emoji fallback face. its gid belongs to that face */
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
