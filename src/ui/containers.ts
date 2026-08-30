import { withAlpha } from '../gfx/color';
import { inset, rect, Rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { theme } from './theme';
import { contains, layoutSubtree, Size, Widget } from './widget';

/** height a child wants at width w: explicit > height-for-width > fallback */
const heightAt = (c: Widget, w: number, fallback: number): number =>
  c.height ?? c.heightForWidth(Math.max(0, w)) ?? fallback;

/**
 * WinForms/WPF-style DockPanel: children consume space from the remaining
 * rect in declaration order according to their `dock` side. a 'fill' child
 * takes whatever is left
 */
export class Dock extends Widget {
  padding = 0;
  gap = 0;

  override layoutChildren(): void {
    this.adopt();
    const rem: Rect = inset(this.bounds, this.padding);
    for (const c of this.children) {
      const sz = this.sizeOf(c);
      let b: Rect;
      switch (c.dock) {
        case 'top': {
          const h = Math.min(heightAt(c, rem.w, sz.h), rem.h);
          b = rect(rem.x, rem.y, rem.w, h);
          rem.y += h + this.gap;
          rem.h = Math.max(0, rem.h - h - this.gap);
          break;
        }
        case 'bottom': {
          const h = Math.min(heightAt(c, rem.w, sz.h), rem.h);
          b = rect(rem.x, rem.y + rem.h - h, rem.w, h);
          rem.h = Math.max(0, rem.h - h - this.gap);
          break;
        }
        case 'left': {
          const w = Math.min(sz.w, rem.w);
          b = rect(rem.x, rem.y, w, rem.h);
          rem.x += w + this.gap;
          rem.w = Math.max(0, rem.w - w - this.gap);
          break;
        }
        case 'right': {
          const w = Math.min(sz.w, rem.w);
          b = rect(rem.x + rem.w - w, rem.y, w, rem.h);
          rem.w = Math.max(0, rem.w - w - this.gap);
          break;
        }
        case 'fill':
          b = rect(rem.x, rem.y, rem.w, rem.h);
          break;
      }
      c.bounds = b;
      layoutSubtree(c);
    }
  }
}

export type StackAlign = 'start' | 'center' | 'end' | 'stretch';

/**
 * NSStackView-style run of children along one axis. Main-axis size per child
 * comes from its `stackSize` (content | fixed | fill-by-weight). the cross
 * axis follows `align`
 */
export class Stack extends Widget {
  padding = 0;
  spacing = 8;
  align: StackAlign = 'start';

  constructor(readonly axis: 'h' | 'v') {
    super();
  }

  override intrinsicSize(): Size {
    this.adopt();
    let main = this.spacing * Math.max(0, this.children.length - 1);
    let cross = 0;
    for (const c of this.children) {
      const sz = this.sizeOf(c);
      const ss = c.stackSize;
      main += ss.kind === 'fixed' ? ss.px : this.axis === 'h' ? sz.w : sz.h;
      cross = Math.max(cross, this.axis === 'h' ? sz.h : sz.w);
    }
    const pad = this.padding * 2;
    return this.axis === 'h' ? { w: main + pad, h: cross + pad } : { w: cross + pad, h: main + pad };
  }

  override layoutChildren(): void {
    this.adopt();
    if (!this.children.length) return;
    const r = inset(this.bounds, this.padding);
    const mainAvail = this.axis === 'h' ? r.w : r.h;
    const crossAvail = this.axis === 'h' ? r.h : r.w;

    let used = this.spacing * (this.children.length - 1);
    let weightSum = 0;
    const mains: number[] = [];
    for (const c of this.children) {
      const ss = c.stackSize;
      if (ss.kind === 'fill') {
        mains.push(0);
        weightSum += ss.weight;
      } else {
        const sz = this.sizeOf(c);
        let m: number;
        if (ss.kind === 'fixed') m = ss.px;
        else if (this.axis === 'h') m = sz.w;
        else {
          // a v-stack knows each child's eventual width, so wrapped labels
          // can restate their height before the main-axis pass
          const cw = this.align === 'stretch' ? crossAvail : Math.min(sz.w, crossAvail);
          m = heightAt(c, cw, sz.h);
        }
        mains.push(m);
        used += m;
      }
    }
    const leftover = Math.max(0, mainAvail - used);

    let pos = this.axis === 'h' ? r.x : r.y;
    this.children.forEach((c, i) => {
      const ss = c.stackSize;
      const main = ss.kind === 'fill' ? (weightSum > 0 ? (leftover * ss.weight) / weightSum : 0) : mains[i]!;
      const sz = this.sizeOf(c);
      // cross-axis children never exceed the stack's extent (labels truncate
      // at the boundary instead of overflowing)
      const cross =
        this.align === 'stretch' ? crossAvail : Math.min(this.axis === 'h' ? sz.h : sz.w, crossAvail);
      let crossPos = this.axis === 'h' ? r.y : r.x;
      if (this.align === 'center') crossPos += (crossAvail - cross) / 2;
      else if (this.align === 'end') crossPos += crossAvail - cross;
      c.bounds = this.axis === 'h' ? rect(pos, crossPos, main, cross) : rect(crossPos, pos, cross, main);
      pos += main + this.spacing;
      layoutSubtree(c);
    });
  }
}

export class HStack extends Stack {
  constructor() {
    super('h');
  }
}

export class VStack extends Stack {
  constructor() {
    super('v');
  }
}

/**
 * Base for anything with a vertical scroll offset: clamping, wheel deltas
 * (routed here by the UI shell), and the draggable overlay thumb. Subclasses 
 * define the scrolled viewport and set contentH during layout.
 */
export abstract class Scroller extends Widget {
  scrollY = 0;
  scrollX = 0;
  protected contentH = 0;
  protected contentW = 0;
  protected dragGrab: number | null = null;
  protected dragGrabX: number | null = null;

  /** the region content scrolls within (a table excludes its header) */
  protected viewport(): Rect {
    return this.bounds;
  }

  protected get maxScroll(): number {
    return Math.max(0, this.contentH - this.viewport().h);
  }

  protected get maxScrollX(): number {
    return Math.max(0, this.contentW - this.viewport().w);
  }

  get scrollable(): boolean {
    return this.maxScroll > 0.5;
  }

  get scrollableX(): boolean {
    return this.maxScrollX > 0.5;
  }

  protected clampScroll(): void {
    this.scrollY = Math.min(Math.max(this.scrollY, 0), this.maxScroll);
    this.scrollX = Math.min(Math.max(this.scrollX, 0), this.maxScrollX);
  }

  scrollBy(dy: number): void {
    const next = Math.min(Math.max(this.scrollY + dy, 0), this.maxScroll);
    if (next !== this.scrollY) {
      this.scrollY = next;
      this.invalidate();
    }
  }

  scrollByX(dx: number): void {
    const next = Math.min(Math.max(this.scrollX + dx, 0), this.maxScrollX);
    if (next !== this.scrollX) {
      this.scrollX = next;
      this.invalidate();
    }
  }

  protected thumbRect(): Rect | null {
    if (!this.scrollable) return null;
    const v = this.viewport();
    const track = v.h - 8;
    const h = Math.max(28, track * (v.h / this.contentH));
    const y = v.y + 4 + (this.scrollY / this.maxScroll) * (track - h);
    return rect(v.x + v.w - 10, y, 6, h);
  }

  protected hThumbRect(): Rect | null {
    if (!this.scrollableX) return null;
    const v = this.viewport();
    const track = v.w - 8;
    const w = Math.max(28, track * (v.w / this.contentW));
    const x = v.x + 4 + (this.scrollX / this.maxScrollX) * (track - w);
    return rect(x, v.y + v.h - 10, w, 6);
  }

  override paintOverlay(dl: DisplayList): void {
    const t = this.thumbRect();
    if (t) dl.rect(t, { color: withAlpha(theme.ink, this.dragGrab != null ? 0.5 : 0.25), radius: 3 });
    const ht = this.hThumbRect();
    if (ht) dl.rect(ht, { color: withAlpha(theme.ink, this.dragGrabX != null ? 0.5 : 0.25), radius: 3 });
  }

  protected thumbDown(x: number, y: number): boolean {
    const t = this.thumbRect();
    if (t && contains(t, x, y)) {
      this.dragGrab = y - t.y;
      this.invalidate();
      return true;
    }
    const ht = this.hThumbRect();
    if (ht && contains(ht, x, y)) {
      this.dragGrabX = x - ht.x;
      this.invalidate();
      return true;
    }
    return false;
  }

  protected thumbDrag(x: number, y: number): boolean {
    if (this.dragGrab != null) {
      const v = this.viewport();
      const track = v.h - 8;
      const h = Math.max(28, track * (v.h / this.contentH));
      const range = track - h;
      if (range > 0) {
        const ratio = (y - this.dragGrab - (v.y + 4)) / range;
        const next = Math.min(Math.max(ratio * this.maxScroll, 0), this.maxScroll);
        if (next !== this.scrollY) {
          this.scrollY = next;
          this.invalidate();
        }
      }
      return true;
    }
    if (this.dragGrabX != null) {
      const v = this.viewport();
      const track = v.w - 8;
      const w = Math.max(28, track * (v.w / this.contentW));
      const range = track - w;
      if (range > 0) {
        const ratio = (x - this.dragGrabX - (v.x + 4)) / range;
        const next = Math.min(Math.max(ratio * this.maxScrollX, 0), this.maxScrollX);
        if (next !== this.scrollX) {
          this.scrollX = next;
          this.invalidate();
        }
      }
      return true;
    }
    return false;
  }

  protected thumbUp(): void {
    if (this.dragGrab != null || this.dragGrabX != null) {
      this.dragGrab = null;
      this.dragGrabX = null;
      this.invalidate();
    }
  }
}

/**
 * Vertically scrollable viewport for ordinary widget children. Children lay
 * out (frame/anchors) against a virtual content rect whose height is their
 * combined extent. The scroll offset just shifts it.
 */
export class ScrollView extends Scroller {
  /** Extra space below the last child. */
  contentInsetBottom = 0;

  constructor() {
    super();
    this.clips = true;
  }

  override layoutChildren(): void {
    this.adopt();
    let extent = 0;
    let extentW = 0;
    for (const c of this.children) {
      const sz = this.sizeOf(c);
      const top = c.frame ? c.frame.y : c.anchors?.top ?? 0;
      const a = c.anchors;
      const wCtx = a && a.left != null && a.right != null ? this.bounds.w - a.left - a.right : sz.w;
      const h = c.frame ? c.frame.h : heightAt(c, wCtx, sz.h);
      extent = Math.max(extent, top + h);
      // width extent from left-pinned/framed children only. right-anchored
      // children track the viewport, not the content.
      const left = c.frame ? c.frame.x : a?.left ?? 0;
      const w = c.frame ? c.frame.w : sz.w;
      if (!a || a.right == null) extentW = Math.max(extentW, left + w);
    }
    this.contentH = extent + this.contentInsetBottom;
    this.contentW = extentW;
    this.clampScroll();
    this.layoutChildrenInto(
      rect(
        this.bounds.x - this.scrollX,
        this.bounds.y - this.scrollY,
        Math.max(this.contentW, this.bounds.w),
        Math.max(this.contentH, this.bounds.h),
      ),
    );
  }

  override hitTest(x: number, y: number): Widget | null {
    if (!contains(this.bounds, x, y)) return null;
    const t = this.thumbRect();
    if (t && contains(t, x, y)) return this; // thumbs paint on top, so they hit first
    const ht = this.hThumbRect();
    if (ht && contains(ht, x, y)) return this;
    for (let i = this.children.length - 1; i >= 0; i--) {
      const hit = this.children[i]!.hitTest(x, y);
      if (hit) return hit;
    }
    return null;
  }

  override onPointerDown(x: number, y: number): void {
    this.thumbDown(x, y);
  }

  override onPointerDrag(x: number, y: number): void {
    this.thumbDrag(x, y);
  }

  override onPointerUp(): void {
    this.thumbUp();
  }
}
