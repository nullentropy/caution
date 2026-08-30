// clusterSlices fixtures: the wasm engine's cluster->source-text mapping must
// survive bidi (RTL runs arrive in visual order with DESCENDING clusters) and
// still tile the text only once across carriers.
import { clusterSlices, type ShapedGlyph } from '../gfx/text/shaper';
import { eq, test } from './harness';

const g = (cluster: number): ShapedGlyph => ({ ch: '', x: 0, cluster, gid: 1 });
const chs = (glyphs: ShapedGlyph[]) => glyphs.map((x) => x.ch);

test('clusterSlices: plain LTR is one char per glyph', () => {
  const glyphs = [g(0), g(1), g(2)];
  clusterSlices(glyphs, 'abc');
  eq(chs(glyphs), ['a', 'b', 'c']);
});

test('clusterSlices: ligature carries its whole cluster', () => {
  const glyphs = [g(0), g(3)]; // "ffi" ligature + "."
  clusterSlices(glyphs, 'ffi.');
  eq(chs(glyphs), ['ffi', '.']);
});

test('clusterSlices: same-cluster follow-ups carry nothing, last carries all', () => {
  const glyphs = [g(0), g(0), g(2)]; // base+mark share cluster 0 ("é"), then "x"
  clusterSlices(glyphs, 'éx');
  eq(chs(glyphs), ['', 'é', 'x']);
});

test('clusterSlices: RTL descending clusters still map each glyph to its own slice', () => {
  const glyphs = [g(2), g(1), g(0)]; // visual order of a 3-rune RTL run
  clusterSlices(glyphs, 'אבג');
  eq(chs(glyphs), ['ג', 'ב', 'א']);
  eq(glyphs.reduce((n, x) => n + x.ch.length, 0), 3, 'carriers tile the text');
});

test('clusterSlices: mixed-direction visual order tiles the text only once', () => {
  // "ab XY cd" where XY is RTL: visual cluster order 0,1,2,4,3,5,6,7
  const text = 'abאב cd';
  const glyphs = [g(0), g(1), g(3), g(2), g(4), g(5), g(6)];
  clusterSlices(glyphs, text);
  eq(chs(glyphs), ['a', 'b', 'ב', 'א', ' ', 'c', 'd']);
  eq(glyphs.reduce((n, x) => n + x.ch.length, 0), text.length, 'carriers tile the text');
});
