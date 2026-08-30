import { BLACK, withAlpha } from '../gfx/color';
import { font } from '../gfx/font';
import { rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { metrics } from './metrics';
import { theme } from './theme';
import type { Ui } from './ui';
import { Widget } from './widget';

/** one entry of a node's `context` prop (flat list; no nesting yet) */
export interface ContextItem {
  id?: number;
  title?: string;
  sep?: boolean;
}

const MENU_FONT = font(13);
const SEP_H = 9;
const PAD = 6;

/**
 * client-local right-click menu. lives on the Ui overlay layer, so
 * click-away and Escape close it for free. picking calls back with the
 * item's server-assigned id
 */
export class ContextMenu extends Widget {
  private hoverRow = -1;

  constructor(
    readonly items: ContextItem[],
    private readonly onPick: (id: number) => void,
  ) {
    super();
  }

  static open(ui: Ui, items: ContextItem[], onPick: (id: number) => void, x: number, y: number): void {
    const menu = new ContextMenu(items, onPick);
    menu.ui = ui;
    let w = 140;
    for (const it of items) {
      if (it.title) w = Math.max(w, Math.ceil(ui.measure(MENU_FONT, it.title).width) + 40);
    }
    w = Math.min(w, 340);
    let h = PAD * 2;
    for (const it of items) h += it.sep ? SEP_H : metrics.rowHeight;
    const vw = ui.canvas.clientWidth;
    const vh = ui.canvas.clientHeight;
    let px = Math.min(x, vw - w - 8);
    let py = y + h > vh - 8 ? Math.max(8, y - h) : y; // flip up at the bottom edge
    menu.bounds = rect(Math.max(8, px), py, w, h);
    ui.openOverlay(menu);
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
    let y = b.y + PAD;
    this.items.forEach((it, i) => {
      if (it.sep) {
        dl.rect(rect(b.x + 8, y + SEP_H / 2, b.w - 16, metrics.borderWidth), { color: theme.edgeSoft });
        y += SEP_H;
        return;
      }
      if (i === this.hoverRow) {
        dl.rect(rect(b.x + 4, y, b.w - 8, metrics.rowHeight), { color: withAlpha(theme.accent, 0.25), radius: metrics.radiusControl });
      }
      const title = it.title ?? '';
      const m = this.ui!.measure(MENU_FONT, title);
      dl.text(title, b.x + 14, y + (metrics.rowHeight - (m.ascent + m.descent)) / 2, MENU_FONT, theme.ink);
      y += metrics.rowHeight;
    });
  }

  override onPointerDown(_x: number, y: number): void {
    const i = this.rowAt(y);
    const it = i >= 0 ? this.items[i] : undefined;
    if (it?.id != null) {
      this.ui?.closeOverlay();
      this.onPick(it.id);
    }
  }

  override onPointerHover(_x: number, y: number): void {
    const i = this.rowAt(y);
    if (i !== this.hoverRow) {
      this.hoverRow = i;
      this.invalidate();
    }
  }

  /** item index at an absolute y (separators are dead space) */
  private rowAt(absY: number): number {
    let y = this.bounds.y + PAD;
    for (let i = 0; i < this.items.length; i++) {
      const it = this.items[i]!;
      const h = it.sep ? SEP_H : metrics.rowHeight;
      if (absY >= y && absY < y + h) return it.sep ? -1 : i;
      y += h;
    }
    return -1;
  }
}
