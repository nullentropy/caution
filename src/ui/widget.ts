import { TRANSPARENT, withAlpha } from '../gfx/color';
import { inflate, Rect, rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import type { SemanticSpec } from './semantics';
import { theme } from './theme';
import type { Ui } from './ui';

export interface Size {
  w: number;
  h: number;
}

/**
 * Composites a fading (freshly inserted) subtree. The pipeline is
 * premultiplied, so scaling the whole sample scales coverage and color
 * together.
 */
const FADE_FRAG = 'vec4 effect(vec2 uv) { return src(uv) * u_fade; }';

/**
 * Springs-and-struts pins against the parent's bounds. Pin one edge and the widget keeps its
 * intrinsic/explicit size. Pin both opposing edges and it stretches.
 */
export interface Anchors {
  left?: number;
  right?: number;
  top?: number;
  bottom?: number;
  /** Offset from the parent's center. */
  centerX?: number;
  centerY?: number;
}

export type DockSide = 'top' | 'bottom' | 'left' | 'right' | 'fill';

export type StackSize =
  | { kind: 'content' }
  | { kind: 'fixed'; px: number }
  | { kind: 'fill'; weight: number };

export const contains = (r: Rect, x: number, y: number): boolean =>
  x >= r.x && x < r.x + r.w && y >= r.y && y < r.y + r.h;

const rectSame = (a: Rect, b: Rect): boolean =>
  a === b || (a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h);

/**
 * Retained widget-tree node. Layout inputs (frame/anchors/dock/stackSize) are
 * interpreted by the *parent* container. `bounds` is the absolute result.
 * The base class's own child layout is Interface Builder-style: `anchors` if
 * present, else `frame`, else intrinsic size at the parent origin.
 */
export abstract class Widget {
  parent: Widget | null = null;
  readonly children: Widget[] = [];
  ui: Ui | null = null;

  // -- layout inputs --------------------------------------------------------
  frame: Rect | null = null;
  anchors: Anchors | null = null;
  dock: DockSide = 'fill';
  /** Window-move region under a hidden native titlebar (windowDrag prop).
   * Inert here (the browser's window chrome is the browser's own) but
   * parsed so the retained tree mirrors the wire. */
  windowDrag = false;
  stackSize: StackSize = { kind: 'content' };
  /** Column span when the parent is a Grid. */
  gridSpan = 1;
  width: number | null = null;
  height: number | null = null;

  /** Clip children to this widget's bounds while painting and hit-testing. */
  clips = false;

  /**
   * Optional fragment-shader post-effect: the subtree renders to an offscreen
   * texture and composites back through it. Visual only - hit-testing sees
   * the undistorted geometry. `animate` repaints continuously (u_time).
   */
  effect: { frag: string; uniforms?: Record<string, number>; animate?: boolean } | null = null;

  /** Tooling adorner: paints an accent ring over the widget (designer selection). */
  outline = false;

  /**
   * `sound` prop: replaces the token this widget's gestures fire. null keeps
   * each gesture's own, 'none' silences the widget.
   */
  soundToken: string | null = null;

  /**
   * Tooltip text (`tip` prop): client-local, shown by the UI after a short
   * hover idle. A tip makes an otherwise inert widget hover-targetable.
   */
  tip: string | null = null;

  /**
   * Right-click menu (`context` prop): opened client-locally by the UI; the
   * deepest carrier under the pointer wins. Picks flow through
   * onContextPick with the item's server-assigned id.
   */
  contextItems: import('./contextmenu').ContextItem[] = [];
  onContextPick: ((id: number) => void) | null = null;

  /** Last-seen values of the server's universal one-shot command props. */
  focusSeq = 0;
  revealSeq = 0;

  sound(token: string): void {
    if (this.soundToken === 'none') return;
    this.ui?.sound(this.soundToken ?? token);
  }

  /** Server-driven focus. Text fields override to rebind the input funnel. */
  grabFocus(): void {
    if (this.focusable) this.ui?.setFocus(this, false);
  }

  /** Absolute bounds in logical px; valid after layout. */
  bounds: Rect = rect(0, 0, 0, 0);

  // -- per-subtree retained display list (see paintTree) --
  /**
   * This widget's own painting may have changed; invalidate() sets it and the
   * marking pass clears it. needsPaint is that aggregated over the subtree,
   * false means the whole subtree can splice the commands it produced last
   * frame instead of being walked.
   */
  dirty = false;
  needsPaint = false;
  /**
   * Where this subtree's commands began in the last frame's list, relative to
   * the parent's own start (absolute for the Ui's roots), and how many there
   * were. Relative offsets survive a parent splicing its whole range
   * verbatim, which is what keeps a deep tree's caches valid frame after
   * frame. The parent records both.
   */
  cacheOff = 0;
  cacheLen = 0;
  cacheOK = false;
  /** The geometry and ambient clip the cached commands were recorded under. */
  cacheBounds: Rect = rect(0, 0, 0, 0);
  cacheClip: Rect = rect(0, 0, 0, 0);

  // -- layout skipping (see layoutSubtree) --
  /**
   * Something in this subtree changed what layout would produce.
   * invalidate() raises it here and on every ancestor, so a container can tell
   * a subtree that still fits its old geometry from one that has to be walked
   * again. laidOutAt is the bounds the subtree was last laid out for.
   */
  layoutDirty = false;
  laidOut = false;
  laidOutAt: Rect = rect(0, 0, 0, 0);

  // -- geometric animation (server-driven reflows only; see Ui.animGeometry) --
  // prevX/prevY track the laid-out position at last frame. When a structural
  // frame (insert/remove/move ops) moves a widget, its paint position decays
  // from the old spot to the new one; fadeR composites a freshly inserted
  // subtree through a fade-in layer. Zero values mean idle.
  hadFrame = false;
  prevX = 0;
  prevY = 0;
  animDx = 0;
  animDy = 0;
  /** Slide remaining: 1 -> 0, 0 = idle. */
  animR = 0;
  /** Fade-in remaining: 1 -> 0, 0 = opaque. */
  fadeR = 0;

  add<T extends Widget>(child: T): T {
    child.parent = this;
    this.children.push(child);
    this.invalidateLayout();
    return child;
  }

  /** Natural content size; containers consult this when no explicit size is set. */
  intrinsicSize(): Size {
    return { w: this.width ?? 0, h: this.height ?? 0 };
  }

  /**
   * Natural height at a given laid-out width, for widgets whose height
   * depends on it (wrapped labels). null = height doesn't depend on width.
   */
  heightForWidth(_w: number): number | null {
    return null;
  }

  protected adopt(): void {
    for (const c of this.children) c.ui = this.ui;
  }

  protected sizeOf(c: Widget): Size {
    if (c.width != null && c.height != null) return { w: c.width, h: c.height };
    const i = c.intrinsicSize();
    return { w: c.width ?? i.w, h: c.height ?? i.h };
  }

  layoutChildren(): void {
    this.adopt();
    this.layoutChildrenInto(this.bounds);
  }

  /** Frame/anchors placement against an arbitrary parent rect (ScrollView passes a virtual one). */
  protected layoutChildrenInto(p: Rect): void {
    for (const c of this.children) {
      const sz = this.sizeOf(c);
      let { w, h } = sz;
      let x: number;
      let y: number;
      const a = c.anchors;
      if (a) {
        // One-sided pins clamp to the parent's opposite edge: an anchored
        // child never lays out wider/taller than its parent, so labels
        // truncate at the boundary instead of overflowing. Explicit frames
        // remain the escape hatch for deliberate overflow.
        if (a.left != null && a.right != null) {
          x = p.x + a.left;
          w = Math.max(0, p.w - a.left - a.right);
        } else if (a.left != null) {
          x = p.x + a.left;
          w = Math.max(0, Math.min(w, p.w - a.left));
        } else if (a.right != null) {
          w = Math.max(0, Math.min(w, p.w - a.right));
          x = p.x + p.w - a.right - w;
        } else if (a.centerX != null) x = p.x + (p.w - w) / 2 + a.centerX;
        else x = p.x;
        // Height-for-width: once the width is settled, widgets whose height
        // depends on it (wrapped labels) get to restate their height.
        if (c.height == null) {
          const hfw = c.heightForWidth(w);
          if (hfw != null) h = hfw;
        }
        if (a.top != null && a.bottom != null) {
          y = p.y + a.top;
          h = Math.max(0, p.h - a.top - a.bottom);
        } else if (a.top != null) {
          y = p.y + a.top;
          h = Math.max(0, Math.min(h, p.h - a.top));
        } else if (a.bottom != null) {
          h = Math.max(0, Math.min(h, p.h - a.bottom));
          y = p.y + p.h - a.bottom - h;
        } else if (a.centerY != null) y = p.y + (p.h - h) / 2 + a.centerY;
        else y = p.y;
      } else if (c.frame) {
        x = p.x + c.frame.x;
        y = p.y + c.frame.y;
        w = c.frame.w;
        h = c.frame.h;
      } else {
        x = p.x;
        y = p.y;
      }
      c.bounds = rect(x, y, w, h);
      layoutSubtree(c);
    }
  }

  paintSelf(_dl: DisplayList): void {}

  /** Painted after children (scrollbars, adorners), still inside this widget's clip. */
  paintOverlay(_dl: DisplayList): void {}

  /**
   * Paint this widget and its subtree in painter's order. When nothing inside
   * can have changed the commands the subtree produced last frame are copied wholesale
   * instead of being rebuilt: the copy is the same as what the walk would have
   * produced, so everything downstream is unaffected.
   * `oldBase` is where those commands begin in the previous list.
   * -1 means there is nothing to reuse.
   */
  paintTree(dl: DisplayList, oldBase: number): void {
    const ui = this.ui;
    const reuse = ui?.reuseList;
    if (
      reuse && oldBase >= 0 && !this.needsPaint &&
      rectSame(this.cacheClip, dl.clip) && oldBase + this.cacheLen <= reuse.cmds.length
    ) {
      for (let i = 0; i < this.cacheLen; i++) dl.cmds.push(reuse.cmds[oldBase + i]!);
      ui!.noteReuse(this.cacheLen);
      return;
    }
    const start = dl.cmds.length;
    this.cacheBounds = this.bounds;
    this.cacheClip = dl.clip;
    // a fading (freshly inserted) subtree composites through a fade layer,
    // outside any app effect layer. the rect is inflated so drop shadows
    // fade with their widget instead of popping in at the end.
    const fading = this.fadeR > 0;
    if (fading) dl.beginLayer(inflate(this.bounds, 24));
    const fx = this.effect;
    if (fx?.frag) dl.beginLayer(this.bounds);
    this.paintSelf(dl);
    if (this.clips) dl.pushClip(this.bounds);
    // the parent records each child's range: it is the only participant that
    // knows both where its own range started and where the child's did.
    for (const c of this.children) {
      const at = dl.cmds.length;
      c.paintTree(dl, oldBase >= 0 && c.cacheOK ? oldBase + c.cacheOff : -1);
      c.cacheOff = at - start;
      c.cacheLen = dl.cmds.length - at;
      c.cacheOK = true;
    }
    this.paintOverlay(dl);
    if (this.clips) dl.popClip();
    if (this.outline) {
      dl.rect(inflate(this.bounds, 1), {
        color: TRANSPARENT,
        radius: 4,
        borderWidth: 2,
        borderColor: withAlpha(theme.accent, 0.9),
      });
    }
    if (fx?.frag) dl.endLayer(fx.frag, fx.uniforms ?? {}, fx.animate ?? false);
    if (fading) {
      const t = 1 - this.fadeR;
      dl.endLayer(FADE_FRAG, { u_fade: t * (2 - t) }, false);
    }
  }

  // -- interaction -----------------------------------------------------------
  get interactive(): boolean {
    return false;
  }

  get focusable(): boolean {
    return false;
  }

  /** Return true if the key was consumed. */
  onKey(_e: KeyboardEvent): boolean {
    return false;
  }

  onFocusChange(_focused: boolean): void {}

  /** What assistive technology should see for this widget; null = decorative. */
  semantics(): SemanticSpec | null {
    return null;
  }

  /** Semantic activation (screen reader press, Space/Enter). */
  activate(): void {}

  cursor(): string | null {
    return null;
  }

  /** Topmost interactive (or tipped) widget at (x, y), in paint order. */
  hitTest(x: number, y: number): Widget | null {
    const inside = contains(this.bounds, x, y);
    if (this.clips && !inside) return null;
    for (let i = this.children.length - 1; i >= 0; i--) {
      const hit = this.children[i]!.hitTest(x, y);
      if (hit) return hit;
    }
    return inside && (this.interactive || this.tip != null || this.contextItems.length > 0) ? this : null;
  }

  onPointerDown(_x: number, _y: number, _detail: number): void {}
  onPointerDrag(_x: number, _y: number): void {}
  onPointerUp(_x: number, _y: number): void {}
  onHoverChange(_hovered: boolean): void {}
  /** Pointer movement while this widget is the hover target (no buttons down). */
  onPointerHover(_x: number, _y: number): void {}

  /**
   * Mark this widget's own painting stale and ask for a frame. It is
   * *targeted*: the next frame repaints this widget and its ancestors' own
   * commands, and splices every clean subtree around it. Anything that changes
   * what a widget paints without coming through here has to use
   * Ui.invalidate() instead, which rebuilds the whole list.
   */
  invalidate(): void {
    this.dirty = true;
    this.invalidateLayout();
    this.ui?.wake();
  }

  /**
   * Mark this subtree's geometry stale, and every ancestor's with it, since a
   * child's size feeds every container above it. invalidate() already does
   * this. Call it directly only for a change that moves geometry without
   * changing what this widget paints.
   */
  invalidateLayout(): void {
    for (let w: Widget | null = this; w && !w.layoutDirty; w = w.parent) w.layoutDirty = true;
  }

  /**
   * The per-widget half of the marking pass: its own state changed, the layout
   * moved or resized it since its commands were recorded, it has never been
   * painted, or a geometric tween is moving it this frame.
   */
  selfNeedsPaint(): boolean {
    return (
      this.dirty || !this.cacheOK || !rectSame(this.bounds, this.cacheBounds) ||
      this.animR > 0 || this.fadeR > 0
    );
  }
}

/**
 * Lay out c unless its geometry is already settled: nothing in the subtree has
 * invalidated layout since the last pass, and the parent just assigned it the
 * bounds it was laid out for. Containers call this instead of layoutChildren,
 * so a frame that changed one label walks that label's spine and nothing else.
 *
 * Ui.layoutAll turns the skip off for a frame the flags cannot be trusted to
 * describe: a resize, an untargeted invalidation, or a geometric animation,
 * which moves bounds after layout and needs them re-derived every frame.
 */
export function layoutSubtree(c: Widget): void {
  if (c.ui && !c.ui.layoutAll && c.laidOut && !c.layoutDirty && rectSame(c.laidOutAt, c.bounds)) {
    return;
  }
  c.layoutChildren();
  c.laidOut = true;
  c.laidOutAt = c.bounds;
  c.layoutDirty = false;
}
