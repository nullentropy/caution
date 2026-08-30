import type { DisplayList } from '../gfx/painter';
import { contains, Widget } from './widget';

/**
 * Transparent pointer-capture surface for custom interaction.
 * It paints nothing, swallows presses over its bounds, and
 * reports them upstream: a press as `pick` with the position (glass-relative,
 * logical px) and the id of the deepest server node under the point, drags
 * as coalesced `drag` positions, release as `drop`.
 *
 * Mirrored in the native terminal (go/terminal/ui/glass.go).
 */

/* minimum gap between drag events: pointer moves arrive at display rate,
 * and the session's event bucket (120/s sustained) must absorb long drags */
const DRAG_COALESCE_MS = 33;

export class Glass extends Widget {
  /** wired by the store when the node subscribes to "pick" */
  onPick: ((x: number, y: number, target: number) => void) | null = null;
  onDragTo: ((x: number, y: number) => void) | null = null;
  onDropAt: ((x: number, y: number) => void) | null = null;
  // wheel over the glass, when subscribed: glass-relative position plus
  // deltas. Coalesced like drags, but deltas ACCUMULATE between sends.
  // dropping a wheel event would lose scroll distance.
  onWheelAt: ((x: number, y: number, dx: number, dy: number) => void) | null = null;
  // right-button gestures, when subscribed: press/drag/release with glass-relative positions
  //
  // while a glass subscribes to rpick, right-press over it starts a captured
  // right-drag INSTEAD of a context menu
  onRPick: ((x: number, y: number) => void) | null = null;
  onRDragTo: ((x: number, y: number) => void) | null = null;
  onRDropAt: ((x: number, y: number) => void) | null = null;
  // resolves a widget to its server node id (0 = none) injected by the store, which owns the id map
  idOf: ((w: Widget) => number) | null = null;

  private dragging = false;
  private lastSent = 0;
  private wheelDx = 0;
  private wheelDy = 0;
  private wheelSent = 0;
  private rSent = 0;

  // routed by the UI when a right-press happens on this glass
  rPickAt(x: number, y: number): void {
    this.onRPick?.(x - this.bounds.x, y - this.bounds.y);
    this.rSent = performance.now();
  }

  // right-drag positions, coalesced on the drag clock
  rDragAt(x: number, y: number): void {
    if (!this.onRDragTo) return;
    const now = performance.now();
    if (now - this.rSent < DRAG_COALESCE_MS) return;
    this.rSent = now;
    this.onRDragTo(x - this.bounds.x, y - this.bounds.y);
  }

  // right release. always the true endpoint, like drop
  rDropAt(x: number, y: number): void {
    this.onRDropAt?.(x - this.bounds.x, y - this.bounds.y);
  }

  // Routed by the Ui's wheel dispatch when this glass is the hit target.
  // A sub-coalesce-window remainder rides the next event (or, at gesture
  // end, is at most one tick's worth, which is noise for any camera/zoom consumer).
  wheelAt(x: number, y: number, dx: number, dy: number): void {
    if (!this.onWheelAt) return;
    this.wheelDx += dx;
    this.wheelDy += dy;
    const now = performance.now();
    if (now - this.wheelSent < DRAG_COALESCE_MS) return;
    this.wheelSent = now;
    this.onWheelAt(x - this.bounds.x, y - this.bounds.y, this.wheelDx, this.wheelDy);
    this.wheelDx = 0;
    this.wheelDy = 0;
  }

  // interactive only when someone listens
  override get interactive(): boolean {
    return this.onPick != null;
  }

  override paintSelf(_dl: DisplayList): void {
    // always invisible
  }

  override onPointerDown(x: number, y: number): void {
    if (!this.onPick) return;
    this.onPick(x - this.bounds.x, y - this.bounds.y, this.targetAt(x, y));
    this.dragging = this.onDragTo != null || this.onDropAt != null;
    this.lastSent = performance.now();
  }

  override onPointerDrag(x: number, y: number): void {
    if (!this.dragging || !this.onDragTo) return;
    const now = performance.now();
    if (now - this.lastSent < DRAG_COALESCE_MS) return;
    this.lastSent = now;
    this.onDragTo(x - this.bounds.x, y - this.bounds.y);
  }

  override onPointerUp(x: number, y: number): void {
    if (!this.dragging) return;
    this.dragging = false;
    this.onDropAt?.(x - this.bounds.x, y - this.bounds.y);
  }

  /**
   * The deepest server node under an absolute point: the widget tree's
   * paint-order-last bounds hit (the hitTest walk minus its interactivity
   * filter), excluding the glass itself. When the deepest widget is
   * client-internal, the nearest server-known ancestor answers instead.
   */
  private targetAt(x: number, y: number): number {
    if (!this.idOf) return 0;
    let root: Widget = this;
    while (root.parent) root = root.parent;
    let hit = this.deepestAt(root, x, y);
    for (; hit; hit = hit.parent) {
      const id = this.idOf(hit);
      if (id !== 0) return id;
    }
    return 0;
  }

  private deepestAt(w: Widget, x: number, y: number): Widget | null {
    if (w === (this as Widget)) return null;
    const inside = contains(w.bounds, x, y);
    if (w.clips && !inside) return null;
    for (let i = w.children.length - 1; i >= 0; i--) {
      const hit = this.deepestAt(w.children[i]!, x, y);
      if (hit) return hit;
    }
    return inside ? w : null;
  }
}
