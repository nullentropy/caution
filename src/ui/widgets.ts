import { BLACK, Color, mix, TRANSPARENT, WHITE, withAlpha } from '../gfx/color';
import { Font, font } from '../gfx/font';
import { inflate, Radius, Rect, rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import type { TextRun } from '../gfx/text/shaper';
import { wrapText, WrapLine } from '../gfx/text/wrap';
import { checkboxRadius, metrics } from './metrics';
import type { SemanticSpec } from './semantics';
import { theme } from './theme';
import { contains, Size, Widget } from './widget';

export class Panel extends Widget {
  bg: Color | null = null;
  borderColor: Color | null = null;
  borderWidth = 1;
  radius: Radius = 0;
  dropShadow: { blur: number; color: Color; offset?: [number, number] } | null = null;

  override paintSelf(dl: DisplayList): void {
    if (this.dropShadow) {
      dl.shadow(this.bounds, {
        blur: this.dropShadow.blur,
        color: this.dropShadow.color,
        offset: this.dropShadow.offset,
        radius: this.radius,
      });
    }
    if (this.bg || this.borderColor) {
      dl.rect(this.bounds, {
        color: this.bg ?? withAlpha(BLACK, 0),
        radius: this.radius,
        borderWidth: this.borderColor ? this.borderWidth : 0,
        borderColor: this.borderColor ?? undefined,
      });
    }
  }
}

const WORD_CH = /[\p{L}\p{N}_]/u;

export class Label extends Widget {
  text: string;
  font: Font;
  color: Color;
  /** Enables drag/double-click selection and ⌘C copy. */
  selectable = false;
  /**
   * Break across lines at the laid-out width instead of truncating; the
   * intrinsic height follows the line count (heightForWidth). A wrapped
   * label's selection boundaries are UTF-16 offsets into the source text
   * (cross-line drags work; copies are exact), where a single-line label's
   * are glyph indices.
   */
  wrap = false;

  private lastFit: { text: string; fontKey: string; epoch: number; w: number; display: string } | null = null;
  private lastLines: { text: string; fontKey: string; epoch: number; w: number; lines: WrapLine[] } | null = null;
  /**
   * Memoized natural size: layout asks every widget on every frame, and each
   * ask costs a shaping-cache lookup.
   */
  private lastSize: { text: string; fontKey: string; epoch: number; size: Size } | null = null;

  constructor(text: string, f: Font = font(13), color: Color = theme.ink) {
    super();
    this.text = text;
    this.font = f;
    this.color = color;
  }

  /**
   * What actually paints: the full text when it fits the laid-out bounds,
   * else the longest prefix + '...' that does (NSTextField-style tail
   * truncation). Labels sized by their intrinsic width never truncate. Ones
   * stretched narrower by anchors/stacks/grids clamp instead of overflowing.
   */
  private displayText(): string {
    const avail = this.bounds.w;
    if (avail <= 0) return this.text;
    if (this.ui!.measure(this.font, this.text).width <= avail) return this.text;
    const f = this.lastFit;
    const epoch = this.ui!.measureEpoch();
    if (f && f.text === this.text && f.fontKey === this.font.key && f.epoch === epoch &&
        Math.abs(f.w - avail) < 0.5) {
      return f.display;
    }
    const cps = [...this.text];
    let lo = 0;
    let hi = cps.length;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (this.ui!.measure(this.font, cps.slice(0, mid).join('') + '…').width <= avail) lo = mid;
      else hi = mid - 1;
    }
    const display = cps.slice(0, lo).join('') + '…';
    this.lastFit = { text: this.text, fontKey: this.font.key, epoch, w: avail, display };
    return display;
  }

  /** The displayed run. Selection and painting agree on what's visible. */
  private get run(): TextRun {
    return this.ui!.measure(this.font, this.displayText());
  }

  override intrinsicSize(): Size {
    const epoch = this.ui!.measureEpoch();
    const c = this.lastSize;
    if (c && c.text === this.text && c.fontKey === this.font.key && c.epoch === epoch) return c.size;
    const r = this.ui!.measure(this.font, this.text); // natural size = full text
    const size = { w: Math.ceil(r.width), h: Math.ceil(r.ascent + r.descent) };
    this.lastSize = { text: this.text, fontKey: this.font.key, epoch, size };
    return size;
  }

  // -- wrapped layout ---------------------------------------------------------

  private wrappedLines(w: number): WrapLine[] {
    const c = this.lastLines;
    const epoch = this.ui!.measureEpoch();
    if (c && c.text === this.text && c.fontKey === this.font.key && c.epoch === epoch &&
        Math.abs(c.w - w) < 0.5) {
      return c.lines;
    }
    const lines = wrapText((s) => this.ui!.measure(this.font, s).width, this.text, Math.max(1, w));
    this.lastLines = { text: this.text, fontKey: this.font.key, epoch, w, lines };
    return lines;
  }

  /** Per-font line metrics. The advance adds 25% leading between lines. */
  private lineMetrics(): { lineH: number; advance: number } {
    const m = this.ui!.measure(this.font, 'Mg');
    const lineH = m.ascent + m.descent;
    return { lineH, advance: lineH + Math.round(lineH * 0.25) };
  }

  override heightForWidth(w: number): number | null {
    if (!this.wrap) return null;
    const n = Math.max(1, this.wrappedLines(w).length);
    const { lineH, advance } = this.lineMetrics();
    return Math.ceil(lineH + (n - 1) * advance);
  }

  /** Boundary x by UTF-16 offset within one line's display text. */
  private xInLine(lineText: string, cu: number): number {
    const run = this.ui!.measure(this.font, lineText);
    let units = 0;
    for (const g of run.glyphs) {
      if (units >= cu) return g.x;
      units += g.ch.length;
    }
    return run.width;
  }

  /** Nearest source-offset boundary to a point, for wrapped labels. */
  private cuAtPoint(x: number, y: number): number {
    const lines = this.wrappedLines(this.bounds.w);
    if (!lines.length) return 0;
    const { advance } = this.lineMetrics();
    const li = Math.max(0, Math.min(lines.length - 1, Math.floor((y - this.bounds.y) / advance)));
    const ln = lines[li]!;
    const run = this.ui!.measure(this.font, this.text.slice(ln.start, ln.end));
    const lx = x - this.bounds.x;
    let best = 0;
    let bestD = Infinity;
    let units = 0;
    const consider = (bx: number, cu: number) => {
      const d = Math.abs(lx - bx);
      if (d < bestD) {
        bestD = d;
        best = cu;
      }
    };
    for (const g of run.glyphs) {
      consider(g.x, units);
      units += g.ch.length;
    }
    consider(run.width, units);
    return ln.start + best;
  }

  private wordRangeAtCU(cu: number): [number, number] {
    const cps: { ch: string; off: number }[] = [];
    let off = 0;
    for (const ch of this.text) {
      cps.push({ ch, off });
      off += ch.length;
    }
    if (!cps.length) return [0, 0];
    let i = cps.findIndex((c) => c.off + c.ch.length > cu);
    if (i < 0) i = cps.length - 1;
    const isWord = (s: string) => WORD_CH.test(s);
    const target = isWord(cps[i]!.ch);
    let s = i;
    let e = i + 1;
    while (s > 0 && isWord(cps[s - 1]!.ch) === target) s--;
    while (e < cps.length && isWord(cps[e]!.ch) === target) e++;
    return [cps[s]!.off, e < cps.length ? cps[e]!.off : this.text.length];
  }

  private paintWrapped(dl: DisplayList): void {
    const lines = this.wrappedLines(this.bounds.w);
    const { lineH, advance } = this.lineMetrics();
    const sel = this.ui?.selectionFor(this);
    const s = sel ? Math.min(sel.start, sel.end) : 0;
    const e = sel ? Math.max(sel.start, sel.end) : 0;
    lines.forEach((ln, i) => {
      const y = this.bounds.y + i * advance;
      const t = this.text.slice(ln.start, ln.end);
      if (sel && e > s) {
        const a = Math.max(s, ln.start);
        const b = Math.min(e, ln.end);
        if (b > a || (ln.start === ln.end && s <= ln.start && e > ln.end)) {
          const x0 = this.xInLine(t, Math.max(0, a - ln.start));
          const x1 = b > a ? this.xInLine(t, b - ln.start) : x0 + 4; // stub marks empty lines
          dl.rect(rect(this.bounds.x + x0 - 1, y - 1, x1 - x0 + 2, lineH + 2), {
            color: withAlpha(theme.accent, 0.35),
            radius: 2,
          });
        }
      }
      dl.text(t, this.bounds.x, y, this.font, this.color);
    });
  }

  override get interactive(): boolean {
    return this.selectable;
  }

  override cursor(): string | null {
    return this.selectable ? 'text' : null;
  }

  override paintSelf(dl: DisplayList): void {
    if (this.wrap) {
      this.paintWrapped(dl);
      return;
    }
    const sel = this.ui?.selectionFor(this);
    if (sel) {
      const run = this.run;
      const s = Math.min(sel.start, sel.end);
      const e = Math.max(sel.start, sel.end);
      if (e > s) {
        const x0 = this.boundaryX(s);
        const x1 = this.boundaryX(e);
        dl.rect(rect(this.bounds.x + x0 - 1, this.bounds.y - 1, x1 - x0 + 2, run.ascent + run.descent + 2), {
          color: withAlpha(theme.accent, 0.35),
          radius: 2,
        });
      }
    }
    dl.text(this.displayText(), this.bounds.x, this.bounds.y, this.font, this.color);
  }

  /** X offset of the boundary before glyph i (or the run end), logical px. */
  private boundaryX(i: number): number {
    const run = this.run;
    return i < run.glyphs.length ? run.glyphs[i]!.x : run.width;
  }

  /** Nearest glyph boundary to an absolute x coordinate. */
  boundaryAt(absX: number): number {
    const local = absX - this.bounds.x;
    const n = this.run.glyphs.length;
    let best = 0;
    let bestD = Infinity;
    for (let i = 0; i <= n; i++) {
      const d = Math.abs(local - this.boundaryX(i));
      if (d < bestD) {
        bestD = d;
        best = i;
      }
    }
    return best;
  }

  glyphText(start: number, end: number): string {
    const s = Math.min(start, end);
    const e = Math.max(start, end);
    if (this.wrap) return this.text.slice(s, e); // wrapped boundaries are source offsets
    const glyphs = this.run.glyphs;
    if (s >= e || glyphs.length === 0) return '';
    // Map the boundary range to a source range via clusters (mirrors the
    // native GlyphText): under bidi the glyph array is visual order, so the
    // endpoints can arrive swapped in source space.
    const dt = this.displayText();
    let from = glyphs[Math.min(s, glyphs.length - 1)]!.cluster;
    let to = e < glyphs.length ? glyphs[e]!.cluster : dt.length;
    if (from > to) [from, to] = [to, from];
    return dt.slice(from, to);
  }

  private wordRangeAt(boundary: number): [number, number] {
    const glyphs = this.run.glyphs;
    const n = glyphs.length;
    if (n === 0) return [0, 0];
    const i = Math.min(boundary, n - 1);
    const isWord = (ch: string) => WORD_CH.test(ch);
    const target = isWord(glyphs[i]!.ch);
    let s = i;
    let e = i + 1;
    while (s > 0 && isWord(glyphs[s - 1]!.ch) === target) s--;
    while (e < n && isWord(glyphs[e]!.ch) === target) e++;
    return [s, e];
  }

  override onPointerDown(x: number, y: number, detail: number): void {
    if (!this.selectable || !this.ui) return;
    if (this.wrap) {
      if (detail >= 3) this.ui.setSelection(this, 0, this.text.length);
      else if (detail === 2) {
        const [s, e] = this.wordRangeAtCU(this.cuAtPoint(x, y));
        this.ui.setSelection(this, s, e);
      } else {
        const i = this.cuAtPoint(x, y);
        this.ui.setSelection(this, i, i);
      }
      return;
    }
    if (detail >= 3) this.ui.setSelection(this, 0, this.run.glyphs.length);
    else if (detail === 2) {
      const [s, e] = this.wordRangeAt(this.boundaryAt(x));
      this.ui.setSelection(this, s, e);
    } else {
      const i = this.boundaryAt(x);
      this.ui.setSelection(this, i, i);
    }
  }

  override onPointerDrag(x: number, y: number): void {
    if (!this.selectable) return;
    if (this.wrap) this.ui?.moveSelectionEnd(this, this.cuAtPoint(x, y));
    else this.ui?.moveSelectionEnd(this, this.boundaryAt(x));
  }

  override semantics(): SemanticSpec | null {
    return this.text ? { role: 'text', label: this.text } : null;
  }
}

const BTN_FONT = font(13, { weight: 600 });

export class Button extends Widget {
  label: string;
  primary: boolean;
  /** Per-node radius override (null = the radius.control metric): circular
   * chrome controls in a custom titlebar, pill CTAs. */
  radius: number | null = null;
  onClick: (() => void) | null = null;
  private hovered = false;
  private pressed = false;

  constructor(label: string, opts: { primary?: boolean; onClick?: () => void } = {}) {
    super();
    this.label = label;
    this.primary = opts.primary ?? false;
    this.onClick = opts.onClick ?? null;
  }

  /** Memoized natural size. Layout asks every frame (see Label.lastSize). */
  private lastSize: { label: string; epoch: number; size: Size } | null = null;

  override intrinsicSize(): Size {
    // Keyed by measureEpoch, which a metrics change bumps, the memo may
    // never outlive the token table it was computed from.
    const epoch = this.ui!.measureEpoch();
    const c = this.lastSize;
    if (c && c.label === this.label && c.epoch === epoch) return c.size;
    const size = {
      w: Math.ceil(this.ui!.measure(BTN_FONT, this.label).width) + metrics.controlPadX * 2,
      h: metrics.controlHeight,
    };
    this.lastSize = { label: this.label, epoch, size };
    return size;
  }

  override get interactive(): boolean {
    return true;
  }

  override get focusable(): boolean {
    return true;
  }

  override cursor(): string {
    return 'pointer';
  }

  override onKey(e: KeyboardEvent): boolean {
    if (e.key === ' ' || e.code === 'Space' || e.key === 'Enter') {
      this.activate();
      return true;
    }
    return false;
  }

  override activate(): void {
    this.pressed = true;
    this.invalidate();
    setTimeout(() => {
      this.pressed = false;
      this.invalidate();
    }, 100);
    this.sound('press');
    this.onClick?.();
  }

  override semantics(): SemanticSpec {
    return { role: 'button', label: this.label };
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    const rad = this.radius ?? metrics.radiusControl;
    if (this.ui?.showFocusRing(this)) paintFocusRing(dl, b, rad);
    let bg: Color;
    let border: Color | null = null;
    if (this.primary) {
      bg = this.pressed ? mix(theme.accent, BLACK, 0.18) : this.hovered ? mix(theme.accent, WHITE, 0.1) : theme.accent;
      dl.shadow(b, { radius: rad, blur: 10, offset: [0, 3], color: withAlpha(theme.accent, 0.35) });
    } else {
      bg = this.pressed ? mix(theme.control, BLACK, 0.2) : this.hovered ? mix(theme.control, WHITE, 0.06) : theme.control;
      border = theme.controlEdge;
    }
    dl.rect(b, { color: bg, radius: rad, borderWidth: border ? metrics.borderWidth : 0, borderColor: border ?? undefined });
    const m = this.ui!.measure(BTN_FONT, this.label);
    dl.text(
      this.label,
      b.x + (b.w - m.width) / 2,
      b.y + (b.h - (m.ascent + m.descent)) / 2,
      BTN_FONT,
      this.primary ? WHITE : hexInk(),
    );
  }

  override onPointerDown(): void {
    this.pressed = true;
    this.invalidate();
  }

  override onPointerUp(x: number, y: number): void {
    const wasInside = contains(this.bounds, x, y);
    this.pressed = false;
    this.invalidate();
    if (wasInside) {
      this.sound('press');
      this.onClick?.();
    }
  }

  override onHoverChange(hovered: boolean): void {
    this.hovered = hovered;
    this.invalidate();
  }
}

const hexInk = (): Color => theme.ink;

/**
 * The keyboard-focus adorner around a rect. `radius` is the *widget's* corner
 * radius (uninflated - pass metrics.radiusControl for standard controls, or
 * half the size for circular things); the ring offsets itself outward by its
 * stroke width plus a 1px gap and rounds to match.
 */
export function paintFocusRing(dl: DisplayList, around: Rect, radius: number): void {
  const w = metrics.focusRing;
  const off = w + 1;
  dl.rect(inflate(around, off), {
    color: TRANSPARENT,
    radius: radius + off,
    borderWidth: w,
    borderColor: withAlpha(theme.accent, 0.8),
  });
}

export class Checkbox extends Widget {
  checked: boolean;
  label: string;
  onToggle: ((checked: boolean) => void) | null = null;
  private hovered = false;

  constructor(label: string, checked = false) {
    super();
    this.label = label;
    this.checked = checked;
  }

  override intrinsicSize(): Size {
    const m = this.ui!.measure(font(13), this.label);
    const box = metrics.checkboxSize;
    return { w: box + metrics.spaceGap + Math.ceil(m.width), h: Math.max(box, Math.ceil(m.ascent + m.descent)) };
  }

  override get interactive(): boolean {
    return true;
  }

  override get focusable(): boolean {
    return true;
  }

  override cursor(): string {
    return 'pointer';
  }

  override onKey(e: KeyboardEvent): boolean {
    if (e.key === ' ' || e.code === 'Space') {
      this.activate();
      return true;
    }
    return false;
  }

  override activate(): void {
    this.checked = !this.checked;
    this.sound('toggle');
    this.onToggle?.(this.checked);
    this.invalidate();
  }

  override semantics(): SemanticSpec {
    return { role: 'checkbox', label: this.label, checked: this.checked };
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    const cb = metrics.checkboxSize;
    const boxY = b.y + (b.h - cb) / 2;
    const box = rect(b.x, boxY, cb, cb);
    if (this.ui?.showFocusRing(this)) paintFocusRing(dl, box, checkboxRadius());
    if (this.checked) {
      dl.rect(box, { color: this.hovered ? mix(theme.accent, WHITE, 0.1) : theme.accent, radius: checkboxRadius() });
      paintCheck(dl, b.x + cb / 2, boxY + cb / 2, WHITE);
    } else {
      dl.rect(box, {
        color: theme.panelInset,
        radius: checkboxRadius(),
        borderWidth: metrics.borderWidth * 1.5,
        borderColor: this.hovered ? mix(theme.controlEdge, WHITE, 0.25) : theme.controlEdge,
      });
    }
    const f = font(13);
    const m = this.ui!.measure(f, this.label);
    dl.text(this.label, b.x + cb + metrics.spaceGap, b.y + (b.h - (m.ascent + m.descent)) / 2, f, this.checked ? theme.ink : theme.inkDim);
  }

  override onPointerUp(x: number, y: number): void {
    if (contains(this.bounds, x, y)) this.activate();
  }

  override onHoverChange(hovered: boolean): void {
    this.hovered = hovered;
    this.invalidate();
  }
}

/**
 * The checkmark as geometry. Stepped diagonal strokes, like the select
 * chevron and tree disclosure: widget chrome never depends on a font
 * shipping a glyph.
 */
export function paintCheck(dl: DisplayList, cx: number, cy: number, c: Color): void {
  cx = Math.floor(cx);
  cy = Math.floor(cy);
  for (let i = 0; i < 8; i++) {
    const y = i <= 2 ? cy - 1 + i : cy + 3 - i;
    dl.rect(rect(cx - 4 + i, y, 1, 3), { color: c });
  }
}
