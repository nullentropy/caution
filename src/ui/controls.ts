import { BLACK, mix, WHITE, withAlpha } from '../gfx/color';
import { font } from '../gfx/font';
import { rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { metrics } from './metrics';
import type { SemanticSpec } from './semantics';
import { theme } from './theme';
import { paintFocusRing } from './widgets';
import { contains, Size, Widget } from './widget';

const CTRL_FONT = font(13);

// -- progress -------------------------------------------------------------------

/** determinate progress bar; `value` is 0..1. Display-only, no events */
export class Progress extends Widget {
  value = 0;

  override intrinsicSize(): Size {
    return { w: this.width ?? 220, h: this.height ?? 6 };
  }

  override semantics(): SemanticSpec {
    return { role: 'text', label: `progress: ${Math.round(this.value * 100)}%` };
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    const r = b.h / 2;
    dl.rect(b, { color: theme.panelInset, radius: r, borderWidth: metrics.borderWidth, borderColor: theme.edgeSoft });
    const v = Math.max(0, Math.min(1, this.value));
    if (v > 0) {
      dl.rect(rect(b.x, b.y, Math.max(b.h, b.w * v), b.h), { color: theme.accent, radius: r });
    }
  }
}

// -- slider ---------------------------------------------------------------------

/**
 * Horizontal slider over [min, max], optionally stepped. Dragging is
 * client-authoritative (the server's value sets are ignored mid-drag, like
 * a focused text field). moves emit `input` (debounced upstream) and the
 * release, or a key press, emits `commit`.
 */
export class Slider extends Widget {
  min = 0;
  max = 1;
  step = 0;
  value = 0;
  onInput: ((v: number) => void) | null = null;
  onCommit: ((v: number) => void) | null = null;

  dragging = false;
  private hovered = false;

  override intrinsicSize(): Size {
    return { w: this.width ?? 220, h: this.height ?? Math.max(24, metrics.sliderThumb * 2 + 4) };
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

  override semantics(): SemanticSpec {
    return { role: 'text', label: `slider: ${this.value}` };
  }

  private span(): number {
    return this.max - this.min || 1;
  }

  private ratio(): number {
    return Math.max(0, Math.min(1, (this.value - this.min) / this.span()));
  }

  private valueAt(x: number): number {
    const b = this.bounds;
    const usable = Math.max(1, b.w - metrics.sliderThumb * 2);
    let v = this.min + Math.max(0, Math.min(1, (x - b.x - metrics.sliderThumb) / usable)) * this.span();
    if (this.step > 0) v = this.min + Math.round((v - this.min) / this.step) * this.step;
    return Math.max(this.min, Math.min(this.max, v));
  }

  private setValue(v: number, commit: boolean): void {
    if (v !== this.value) {
      this.value = v;
      this.invalidate();
      this.onInput?.(v);
    }
    if (commit) this.onCommit?.(this.value);
  }

  override onPointerDown(x: number): void {
    this.dragging = true;
    this.setValue(this.valueAt(x), false);
  }

  override onPointerDrag(x: number): void {
    if (this.dragging) this.setValue(this.valueAt(x), false);
  }

  override onPointerUp(): void {
    if (this.dragging) {
      this.dragging = false;
      this.setValue(this.value, true);
    }
  }

  override onKey(e: KeyboardEvent): boolean {
    const nudge = this.step > 0 ? this.step : this.span() / 100;
    let v: number | null = null;
    if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') v = this.value - nudge;
    else if (e.key === 'ArrowRight' || e.key === 'ArrowUp') v = this.value + nudge;
    else if (e.key === 'Home') v = this.min;
    else if (e.key === 'End') v = this.max;
    if (v == null) return false;
    this.setValue(Math.max(this.min, Math.min(this.max, v)), true);
    return true;
  }

  override onHoverChange(hovered: boolean): void {
    this.hovered = hovered;
    this.invalidate();
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    const cy = b.y + b.h / 2;
    const track = rect(b.x + metrics.sliderThumb, cy - metrics.sliderTrack / 2, b.w - metrics.sliderThumb * 2, metrics.sliderTrack);
    dl.rect(track, { color: theme.panelInset, radius: metrics.sliderTrack / 2, borderWidth: metrics.borderWidth, borderColor: theme.edgeSoft });
    const tx = track.x + track.w * this.ratio();
    if (tx > track.x) {
      dl.rect(rect(track.x, track.y, tx - track.x, track.h), { color: theme.accent, radius: metrics.sliderTrack / 2 });
    }
    const thumb = rect(tx - metrics.sliderThumb, cy - metrics.sliderThumb, metrics.sliderThumb * 2, metrics.sliderThumb * 2);
    if (this.ui?.showFocusRing(this)) paintFocusRing(dl, thumb, metrics.sliderThumb);
    dl.shadow(thumb, { blur: 6, color: withAlpha(BLACK, 0.4), offset: [0, 2], radius: metrics.sliderThumb });
    const fill = this.dragging ? mix(theme.accent, BLACK, 0.15) : this.hovered ? mix(theme.accent, WHITE, 0.12) : theme.accent;
    dl.rect(thumb, { color: fill, radius: metrics.sliderThumb, borderWidth: metrics.borderWidth * 2, borderColor: WHITE });
  }
}

// -- radio group ------------------------------------------------------------------

/**
 * The radio circle's diameter: checkbox.size - 2, because a circle reads
 * optically larger than a square of the same box.
 */
const radioD = (): number => metrics.checkboxSize - 2;

/** vertical radio group. picking emits `select` with the option index */
export class RadioGroup extends Widget {
  options: string[] = [];
  selected = -1;
  onSelect: ((i: number) => void) | null = null;
  private hoverRow = -1;

  override intrinsicSize(): Size {
    let w = 0;
    for (const o of this.options) w = Math.max(w, Math.ceil(this.ui!.measure(CTRL_FONT, o).width));
    return { w: radioD() + metrics.spaceGap + w, h: this.options.length * metrics.rowHeight };
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

  override semantics(): SemanticSpec {
    return { role: 'text', label: `radio group: ${this.options[this.selected] ?? 'none'}` };
  }

  private pick(i: number): void {
    if (i < 0 || i >= this.options.length || i === this.selected) return;
    this.selected = i; // local echo
    this.invalidate();
    this.onSelect?.(i);
  }

  override onKey(e: KeyboardEvent): boolean {
    if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
      this.pick(Math.min(this.options.length - 1, this.selected + 1));
      return true;
    }
    if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
      this.pick(Math.max(0, this.selected - 1));
      return true;
    }
    return false;
  }

  override onPointerUp(x: number, y: number): void {
    if (contains(this.bounds, x, y)) this.pick(this.rowAt(y));
  }

  override onPointerHover(_x: number, y: number): void {
    const i = this.rowAt(y);
    if (i !== this.hoverRow) {
      this.hoverRow = i;
      this.invalidate();
    }
  }

  override onHoverChange(hovered: boolean): void {
    if (!hovered && this.hoverRow !== -1) {
      this.hoverRow = -1;
      this.invalidate();
    }
  }

  private rowAt(y: number): number {
    const i = Math.floor((y - this.bounds.y) / metrics.rowHeight);
    return i >= 0 && i < this.options.length ? i : -1;
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    const d = radioD();
    const rowH = metrics.rowHeight;
    // the selected dot keeps a 5px ring of accent around it, floored so it
    // never vanishes under a small checkbox.size
    const dot = Math.max(4, d - 10);
    this.options.forEach((opt, i) => {
      const y = b.y + i * rowH;
      const cy = y + rowH / 2;
      const circle = rect(b.x, cy - d / 2, d, d);
      if (i === this.selected && this.ui?.showFocusRing(this)) paintFocusRing(dl, circle, d / 2);
      if (i === this.selected) {
        dl.rect(circle, { color: theme.accent, radius: d / 2 });
        dl.rect(rect(circle.x + (d - dot) / 2, circle.y + (d - dot) / 2, dot, dot), {
          color: WHITE,
          radius: dot / 2,
        });
      } else {
        const edge = i === this.hoverRow ? mix(theme.controlEdge, WHITE, 0.25) : theme.controlEdge;
        dl.rect(circle, { color: theme.panelInset, radius: d / 2, borderWidth: metrics.borderWidth * 1.5, borderColor: edge });
      }
      const m = this.ui!.measure(CTRL_FONT, opt);
      dl.text(opt, b.x + d + metrics.spaceGap, cy - (m.ascent + m.descent) / 2, CTRL_FONT, i === this.selected ? theme.ink : theme.inkDim);
    });
  }
}

// -- tabs ---------------------------------------------------------------------------

const TAB_FONT = font(13, { weight: 600 });

/**
 * A tab's horizontal text padding, and the bar's height: control.height plus
 * the 2px selection underline the bar reserves below the text.
 */
const tabPad = (): number => metrics.controlPadX;
const tabsH = (): number => metrics.controlHeight + 2;

/** Tab bar. Picking emits `select`. */
export class Tabs extends Widget {
  options: string[] = [];
  selected = 0;
  onSelect: ((i: number) => void) | null = null;
  private hoverTab = -1;

  override intrinsicSize(): Size {
    let w = 0;
    for (const o of this.options) w += Math.ceil(this.ui!.measure(TAB_FONT, o).width) + tabPad() * 2;
    return { w, h: tabsH() };
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

  override semantics(): SemanticSpec {
    return { role: 'text', label: `tabs: ${this.options[this.selected] ?? ''}` };
  }

  private pick(i: number): void {
    if (i < 0 || i >= this.options.length || i === this.selected) return;
    this.selected = i; // local echo
    this.invalidate();
    this.onSelect?.(i);
  }

  override onKey(e: KeyboardEvent): boolean {
    if (e.key === 'ArrowRight') {
      this.pick(Math.min(this.options.length - 1, this.selected + 1));
      return true;
    }
    if (e.key === 'ArrowLeft') {
      this.pick(Math.max(0, this.selected - 1));
      return true;
    }
    return false;
  }

  override onPointerUp(x: number, y: number): void {
    if (contains(this.bounds, x, y)) this.pick(this.tabAt(x));
  }

  override onPointerHover(x: number, _y: number): void {
    const i = this.tabAt(x);
    if (i !== this.hoverTab) {
      this.hoverTab = i;
      this.invalidate();
    }
  }

  override onHoverChange(hovered: boolean): void {
    if (!hovered && this.hoverTab !== -1) {
      this.hoverTab = -1;
      this.invalidate();
    }
  }

  private tabAt(x: number): number {
    let tx = this.bounds.x;
    for (let i = 0; i < this.options.length; i++) {
      const w = Math.ceil(this.ui!.measure(TAB_FONT, this.options[i]!).width) + tabPad() * 2;
      if (x >= tx && x < tx + w) return i;
      tx += w;
    }
    return -1;
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    const hair = metrics.borderWidth;
    dl.rect(rect(b.x, b.y + b.h - hair, b.w, hair), { color: theme.edgeSoft });
    let tx = b.x;
    this.options.forEach((opt, i) => {
      const m = this.ui!.measure(TAB_FONT, opt);
      const w = Math.ceil(m.width) + tabPad() * 2;
      const selectedTab = i === this.selected;
      if (selectedTab && this.ui?.showFocusRing(this)) {
        paintFocusRing(dl, rect(tx + 4, b.y + 4, w - 8, b.h - 8), metrics.radiusControl);
      }
      const color = selectedTab ? theme.ink : i === this.hoverTab ? theme.ink : theme.inkDim;
      dl.text(opt, tx + tabPad(), b.y + (b.h - (m.ascent + m.descent)) / 2 - 1, TAB_FONT, color);
      if (selectedTab) {
        dl.rect(rect(tx + tabPad() - 4, b.y + b.h - 2, w - tabPad() * 2 + 8, 2), { color: theme.accent, radius: 1 });
      }
      tx += w;
    });
  }
}
