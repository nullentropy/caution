import { withAlpha } from '../gfx/color';
import { inset, rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { wrapText, WrapLine } from '../gfx/text/wrap';
import { metrics } from './metrics';
import type { SemanticSpec } from './semantics';
import { funnel } from './textinput';
import { theme } from './theme';
import { TextField } from './textfield';
import { paintFocusRing } from './widgets';
import { Size } from './widget';

const PAD_Y = 8;

/**
 * Multi-line text editor. Same hidden-textarea funnel as TextField but the display wraps
 * logically at the widget's width, <enter> inserts a newline (commit stays on
 * blur), and Up/Down move by *visual* lines.
 */
export class TextArea extends TextField {
  // visible line count for the intrinsic height (`rows` prop)
  rows = 4;
  scrollY = 0;
  // goal x preserved across consecutive vertical caret moves
  private goalX: number | null = null;
  private verticalHop = false;
  private lastLines: { text: string; fontKey: string; w: number; lines: WrapLine[] } | null = null;

  override get multiline(): boolean {
    return true;
  }

  private metrics(): { lineH: number; advance: number } {
    const m = this.ui!.measure(this.fieldFont(), 'Mg');
    const lineH = m.ascent + m.descent;
    return { lineH, advance: lineH + Math.round(lineH * 0.25) };
  }

  override intrinsicSize(): Size {
    const { lineH, advance } = this.metrics();
    const rows = Math.max(1, this.rows);
    return { w: this.width ?? 280, h: this.height ?? Math.ceil(PAD_Y * 2 + lineH + (rows - 1) * advance) };
  }

  override semantics(): SemanticSpec {
    return { role: 'textbox', label: this.placeholder || 'text area', value: this.value };
  }

  override forceClear(): void {
    super.forceClear();
    this.scrollY = 0;
    this.goalX = null;
  }

  override syncSelection(selStart: number, selEnd: number): void {
    if (!this.verticalHop) this.goalX = null; // any non-vertical move resets the goal column
    super.syncSelection(selStart, selEnd);
  }

  // -- wrapped geometry ---------------------------------------------------------

  private lines(): WrapLine[] {
    const w = Math.max(1, this.bounds.w - metrics.spacePad * 2);
    const c = this.lastLines;
    const key = this.fieldFont().key;
    if (c && c.text === this.value && c.fontKey === key && Math.abs(c.w - w) < 0.5) return c.lines;
    const lines = wrapText((s) => this.ui!.measure(this.fieldFont(), s).width, this.value, w);
    this.lastLines = { text: this.value, fontKey: key, w, lines };
    return lines;
  }

  private lineIndexOfCU(cu: number): number {
    const ls = this.lines();
    let li = 0;
    for (let i = 0; i < ls.length; i++) {
      if (ls[i]!.start <= cu) li = i;
      else break;
    }
    return li;
  }

  private xInLine(lineText: string, cu: number): number {
    const run = this.ui!.measure(this.fieldFont(), lineText);
    let units = 0;
    for (const g of run.glyphs) {
      if (units >= cu) return g.x;
      units += g.ch.length;
    }
    return run.width;
  }

  private cuInLineAtX(lineText: string, x: number): number {
    const run = this.ui!.measure(this.fieldFont(), lineText);
    let best = 0;
    let bestD = Infinity;
    let units = 0;
    const consider = (bx: number, cu: number) => {
      const d = Math.abs(x - bx);
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
    return best;
  }

  private caretPos(cu: number): { li: number; x: number } {
    const ls = this.lines();
    if (!ls.length) return { li: 0, x: 0 };
    const li = this.lineIndexOfCU(cu);
    const ln = ls[li]!;
    const local = Math.max(0, Math.min(cu - ln.start, ln.end - ln.start));
    return { li, x: this.xInLine(this.value.slice(ln.start, ln.end), local) };
  }

  private cuAtPoint(x: number, y: number): number {
    const ls = this.lines();
    if (!ls.length) return 0;
    const { advance } = this.metrics();
    const localY = y - (this.bounds.y + PAD_Y) + this.scrollY;
    const li = Math.max(0, Math.min(ls.length - 1, Math.floor(localY / advance)));
    const ln = ls[li]!;
    return ln.start + this.cuInLineAtX(this.value.slice(ln.start, ln.end), x - (this.bounds.x + metrics.spacePad));
  }

  override verticalMove(dir: 1 | -1, extend: boolean): void {
    const ls = this.lines();
    const pos = this.caretPos(this.selEnd);
    const x = this.goalX ?? pos.x;
    this.goalX = x;
    let cu: number;
    const target = pos.li + dir;
    if (target < 0) cu = 0;
    else if (target >= ls.length) cu = this.value.length;
    else {
      const ln = ls[target]!;
      cu = ln.start + this.cuInLineAtX(this.value.slice(ln.start, ln.end), x);
    }
    if (extend) {
      if (this.selStart === this.selEnd) this.anchor = this.selEnd;
      this.verticalHop = true;
      if (cu >= this.anchor) funnel.setSelection(this.anchor, cu);
      else funnel.setSelection(cu, this.anchor, 'backward');
      this.verticalHop = false;
    } else {
      this.anchor = cu;
      this.verticalHop = true;
      funnel.setSelection(cu, cu);
      this.verticalHop = false;
    }
  }

  // -- scrolling ------------------------------------------------------------------

  private contentH(): number {
    const { lineH, advance } = this.metrics();
    const n = Math.max(1, this.lines().length);
    return lineH + (n - 1) * advance;
  }

  get scrollable(): boolean {
    return this.ui != null && this.contentH() > this.bounds.h - PAD_Y * 2;
  }

  scrollBy(dy: number): void {
    const max = Math.max(0, this.contentH() - (this.bounds.h - PAD_Y * 2));
    const next = Math.min(Math.max(this.scrollY + dy, 0), max);
    if (next !== this.scrollY) {
      this.scrollY = next;
      this.invalidate();
    }
  }

  // -- painting ---------------------------------------------------------------------

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    if (this.ui?.showFocusRing(this)) paintFocusRing(dl, b, metrics.radiusControl);
    dl.rect(b, {
      color: theme.panelInset,
      radius: metrics.radiusControl,
      borderWidth: this.focused ? metrics.borderWidth * 1.5 : metrics.borderWidth,
      borderColor: this.focused ? theme.accent : theme.controlEdge,
    });

    const { lineH, advance } = this.metrics();
    const ls = this.lines();
    const innerH = b.h - PAD_Y * 2;

    // Caret-follow: keep the caret's line inside the viewport.
    if (this.focused && ls.length) {
      const cy = this.caretPos(this.selEnd).li * advance;
      if (cy - this.scrollY < 0) this.scrollY = cy;
      if (cy + lineH - this.scrollY > innerH) this.scrollY = cy + lineH - innerH;
    }
    this.scrollY = Math.min(Math.max(this.scrollY, 0), Math.max(0, this.contentH() - innerH));

    const textX = b.x + metrics.spacePad;
    const topY = b.y + PAD_Y - this.scrollY;

    dl.pushClip(inset(b, 1.5));
    if (this.value === '' && this.placeholder) {
      dl.text(this.placeholder, textX, b.y + PAD_Y, this.fieldFont(), theme.inkFaint);
    }
    const s = Math.min(this.selStart, this.selEnd);
    const e = Math.max(this.selStart, this.selEnd);
    ls.forEach((ln, i) => {
      const y = topY + i * advance;
      if (y + lineH < b.y || y > b.y + b.h) return; // off-viewport line
      const t = this.value.slice(ln.start, ln.end);
      if (this.focused && e > s) {
        const a = Math.max(s, ln.start);
        const bb = Math.min(e, ln.end);
        if (bb > a || (ln.start === ln.end && s <= ln.start && e > ln.end)) {
          const x0 = this.xInLine(t, Math.max(0, a - ln.start));
          const x1 = bb > a ? this.xInLine(t, bb - ln.start) : x0 + 4;
          dl.rect(rect(textX + x0 - 1, y - 1, x1 - x0 + 2, lineH + 2), {
            color: withAlpha(theme.accent, 0.35),
            radius: 2,
          });
        }
      }
      dl.text(t, textX, y, this.fieldFont(), theme.ink);
    });
    if (this.focused && s === e && this.caretOn) {
      const pos = this.caretPos(this.selEnd);
      dl.rect(rect(textX + pos.x, topY + pos.li * advance - 1, 1.5, lineH + 2), { color: theme.accent });
    }
    dl.popClip();
  }

  // -- mouse ---------------------------------------------------------------------------

  override onPointerDown(x: number, y: number, detail: number): void {
    const cu = this.cuAtPoint(x, y);
    if (detail >= 3) {
      // Triple-click selects the hard paragraph around the point.
      let s = this.value.lastIndexOf('\n', Math.max(0, cu - 1)) + 1;
      let e = this.value.indexOf('\n', cu);
      if (e < 0) e = this.value.length;
      funnel.setSelection(s, e);
      this.anchor = s;
    } else if (detail === 2) {
      const [s, e] = this.wordRangeCU(cu);
      funnel.setSelection(s, e);
      this.anchor = s;
    } else {
      funnel.setSelection(cu, cu);
      this.anchor = cu;
    }
  }

  override onPointerDrag(x: number, y: number): void {
    const cu = this.cuAtPoint(x, y);
    if (cu >= this.anchor) funnel.setSelection(this.anchor, cu);
    else funnel.setSelection(cu, this.anchor, 'backward');
  }
}
