import { withAlpha } from '../gfx/color';
import { rect, Rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { theme } from './theme';
import { contains, layoutSubtree, Widget } from './widget';

// visual gutter between panes; the grab target extends SLOP px past it
const DIVIDER = 6;
const SLOP = 3;

/**
 * Two-pane split with a draggable divider (nest for more panes). Dragging is
 * client-local. The final position commits upstream as `split-resize` on
 * release. `pos` is the first pane's main-axis size in px, clamped by minA /
 * minB every layout, so window resizes keep both panes sane.
 */
export class SplitView extends Widget {
  axis: 'h' | 'v' = 'h';
  pos = 300;
  minA = 80;
  minB = 80;
  onResize: ((pos: number) => void) | null = null;

  private hoverDivider = false;
  private grab: number | null = null;

  override get interactive(): boolean {
    return true; // only the divider hits (see hitTest)
  }

  override layoutChildren(): void {
    this.adopt();
    const b = this.bounds;
    const main = this.axis === 'h' ? b.w : b.h;
    const maxPos = Math.max(this.minA, main - DIVIDER - this.minB);
    this.pos = Math.min(Math.max(this.pos, this.minA), maxPos);

    const [first, second, ...rest] = this.children;
    if (first) {
      first.bounds =
        this.axis === 'h' ? rect(b.x, b.y, this.pos, b.h) : rect(b.x, b.y, b.w, this.pos);
      layoutSubtree(first);
    }
    if (second) {
      const start = this.pos + DIVIDER;
      second.bounds =
        this.axis === 'h'
          ? rect(b.x + start, b.y, Math.max(0, b.w - start), b.h)
          : rect(b.x, b.y + start, b.w, Math.max(0, b.h - start));
      layoutSubtree(second);
    }
    for (const extra of rest) extra.bounds = rect(0, 0, 0, 0);
  }

  private dividerRect(): Rect {
    const b = this.bounds;
    return this.axis === 'h'
      ? rect(b.x + this.pos - SLOP, b.y, DIVIDER + SLOP * 2, b.h)
      : rect(b.x, b.y + this.pos - SLOP, b.w, DIVIDER + SLOP * 2);
  }

  override paintOverlay(dl: DisplayList): void {
    // the divider is invisible at rest and reveals itself on hover (with the
    // resize cursor) and stays visible while dragging.
    if (!this.hoverDivider && this.grab == null) return;
    const b = this.bounds;
    const c = this.pos + DIVIDER / 2;
    const line =
      this.axis === 'h'
        ? rect(b.x + c - 1.5, b.y, 3, b.h)
        : rect(b.x, b.y + c - 1.5, b.w, 3);
    dl.rect(line, { color: withAlpha(theme.accent, 0.7), radius: 1.5 });
  }

  override hitTest(x: number, y: number): Widget | null {
    if (!contains(this.bounds, x, y)) return null;
    if (contains(this.dividerRect(), x, y)) return this; // divider beats pane edges
    for (let i = this.children.length - 1; i >= 0; i--) {
      const hit = this.children[i]!.hitTest(x, y);
      if (hit) return hit;
    }
    return null;
  }

  override cursor(): string {
    return this.axis === 'h' ? 'col-resize' : 'row-resize';
  }

  override onPointerDown(x: number, y: number): void {
    this.grab = (this.axis === 'h' ? x : y) - this.pos;
    this.invalidate();
  }

  override onPointerDrag(x: number, y: number): void {
    if (this.grab == null) return;
    this.pos = (this.axis === 'h' ? x : y) - this.grab; // clamped in layout
    this.invalidate();
  }

  override onPointerUp(): void {
    if (this.grab == null) return;
    this.grab = null;
    this.invalidate();
    this.onResize?.(Math.round(this.pos)); // commit once, on release
  }

  override onHoverChange(hovered: boolean): void {
    this.hoverDivider = hovered;
    this.invalidate();
  }
}
