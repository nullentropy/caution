import { mix, WHITE, withAlpha } from '../gfx/color';
import { BLACK } from '../gfx/color';
import { font } from '../gfx/font';
import { rect, type Rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { metrics } from './metrics';
import type { SemanticSpec } from './semantics';
import { theme } from './theme';
import { paintCheck, paintFocusRing } from './widgets';
import { contains, Size, Widget } from './widget';

const SELECT_FONT = font(13);
const PAD = 6;
// the closed control's pull-down zone (clip inset + mark)
const CHEVRON_W = 24;

/**
 * Dropdown select. The closed control is server-driven state. The open list
 * is a client-local popover on the UI overlay layer. Opening, hovering, and
 * closing don't touch the network. Picking an option emits `select`.
 */
export class Select extends Widget {
  options: string[] = [];
  selected = -1;
  onSelect: ((index: number) => void) | null = null;
  private hovered = false;

  get value(): string {
    return this.selected >= 0 ? this.options[this.selected] ?? '' : '';
  }

  override intrinsicSize(): Size {
    let w = 0;
    for (const o of this.options) w = Math.max(w, this.ui?.measure(SELECT_FONT, o).width ?? 0);
    return {
      w: this.width ?? Math.ceil(w) + metrics.spacePad * 2 + CHEVRON_W,
      h: this.height ?? metrics.controlHeight,
    };
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

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    if (this.ui?.showFocusRing(this)) paintFocusRing(dl, b, metrics.radiusControl);
    dl.rect(b, {
      color: this.hovered ? mix(theme.control, WHITE, 0.06) : theme.control,
      radius: metrics.radiusControl,
      borderWidth: metrics.borderWidth,
      borderColor: theme.controlEdge,
    });
    const m = this.ui!.measure(SELECT_FONT, this.value);
    dl.pushClip(rect(b.x, b.y, b.w - CHEVRON_W, b.h));
    dl.text(this.value, b.x + metrics.spacePad, b.y + (b.h - (m.ascent + m.descent)) / 2, SELECT_FONT, theme.ink);
    dl.popClip();
    paintChevron(dl, b);
  }

  override onPointerDown(): void {
    this.toggle();
  }

  override onKey(e: KeyboardEvent): boolean {
    if (e.key === ' ' || e.code === 'Space' || e.key === 'Enter') {
      this.toggle();
      return true;
    }
    return false;
  }

  override activate(): void {
    this.toggle();
  }

  override onHoverChange(hovered: boolean): void {
    this.hovered = hovered;
    this.invalidate();
  }

  override semantics(): SemanticSpec {
    return { role: 'button', label: `${this.value} - select` };
  }

  pick(index: number): void {
    if (index !== this.selected) {
      this.selected = index; // local echo and server syncs silently
      this.onSelect?.(index);
    }
    this.ui?.closeOverlay();
    this.invalidate();
  }

  private toggle(): void {
    const ui = this.ui;
    if (!ui || !this.options.length) return;
    if (ui.overlay instanceof SelectPopup && ui.overlay.owner === this) {
      ui.closeOverlay();
      return;
    }
    const popup = new SelectPopup(this);
    let w = this.bounds.w;
    for (const o of this.options) w = Math.max(w, (this.ui?.measure(SELECT_FONT, o).width ?? 0) + 40);
    const h = this.options.length * metrics.rowHeight + PAD * 2;
    let y = this.bounds.y + this.bounds.h + 4;
    const viewH = ui.canvas.clientHeight;
    if (y + h > viewH - 8) y = Math.max(8, this.bounds.y - 4 - h);
    popup.bounds = rect(this.bounds.x, y, w, h);
    ui.openOverlay(popup);
  }
}

// client-local option list. lives on the UI overlay layer above everything
export class SelectPopup extends Widget {
  private hoverRow = -1;

  constructor(readonly owner: Select) {
    super();
  }

  override get interactive(): boolean {
    return true;
  }

  override cursor(): string {
    return 'pointer';
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    dl.shadow(b, { radius: metrics.radiusPopover, blur: 24, offset: [0, 10], color: withAlpha(BLACK, 0.45) });
    dl.rect(b, { color: theme.panel, radius: metrics.radiusPopover, borderWidth: metrics.borderWidth, borderColor: theme.edge });
    this.owner.options.forEach((opt, i) => {
      const y = b.y + PAD + i * metrics.rowHeight;
      if (i === this.hoverRow) {
        dl.rect(rect(b.x + 4, y, b.w - 8, metrics.rowHeight), { color: withAlpha(theme.accent, 0.25), radius: metrics.radiusControl });
      }
      const m = this.ui!.measure(SELECT_FONT, opt);
      dl.text(opt, b.x + 26, y + (metrics.rowHeight - (m.ascent + m.descent)) / 2, SELECT_FONT, theme.ink);
      if (i === this.owner.selected) {
        paintCheck(dl, b.x + 14, y + metrics.rowHeight / 2, theme.accent);
      }
    });
  }

  override onPointerDown(_x: number, y: number): void {
    const i = this.rowAt(y);
    if (i >= 0) this.owner.pick(i);
  }

  override onPointerHover(_x: number, y: number): void {
    const i = this.rowAt(y);
    if (i !== this.hoverRow) {
      this.hoverRow = i;
      this.invalidate();
    }
  }

  private rowAt(y: number): number {
    const i = Math.floor((y - this.bounds.y - PAD) / metrics.rowHeight);
    return i >= 0 && i < this.owner.options.length ? i : -1;
  }
}

/**
 * The select's pull-down mark, drawn as geometry and not text: the Go fonts lack
 * U+25BE natively and system fonts each size it differently, so a glyph here
 * can never converge across terminals.
 * Five shrinking bars make the same stepped triangle everywhere.
 */
function paintChevron(dl: DisplayList, b: Rect): void {
  const cx = Math.floor(b.x + b.w - 15);
  const cy = Math.floor(b.y + b.h / 2);
  for (let i = 0; i < 5; i++) {
    const w = 9 - 2 * i;
    dl.rect(rect(cx - w / 2, cy - 3 + i, w, 1), { color: theme.inkDim });
  }
}
