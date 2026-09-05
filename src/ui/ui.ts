import { BLACK, withAlpha } from '../gfx/color';
import { font, Font } from '../gfx/font';
import { rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { FrameEnv, Surface } from '../gfx/surface';
import type { TextEngine } from '../gfx/text/engine';
import type { TextRun } from '../gfx/text/shaper';
import { Scroller } from './containers';
import { ContextMenu } from './contextmenu';
import { Dialog } from './dialog';
import { Glass } from './glass';
import { MenuBar, menubarH, type MenuSpec } from './menubar';
import { SemanticsLayer } from './semantics';
import { TextArea } from './textarea';
import { TextField } from './textfield';
import { applyMetricTokens, metrics } from './metrics';
import { theme, themeEpoch } from './theme';
import type { Label } from './widgets';
import { contains, layoutSubtree, Widget } from './widget';

export interface TextSelection {
  label: Label;
  /** Glyph-boundary indices. start may exceed end while dragging backwards. */
  start: number;
  end: number;
}

/** Geometric animation pacing - mirrored in the native terminal's ui.go. */
const SLIDE_DUR = 0.16;
const FADE_DUR = 0.2;
const easeOut = (t: number): number => t * (2 - t);

/**
 * The client runtime shell.
 */
export class Ui {
  readonly surface: Surface;
  readonly semantics: SemanticsLayer;
  root: Widget | null = null;
  /** Client-local overlay (popovers, menus): painted above the tree, hit first. */
  overlay: Widget | null = null;
  banner: Widget | null = null;
  menubar: MenuBar | null = null;
  private menuCombos = new Map<string, number>();
  // runs before layout on every frame
  beforePaint: ((env: FrameEnv) => void) | null = null;
  private lastSize = { w: 0, h: 0 };

  private hovered: Widget | null = null;
  private active: Widget | null = null;
  private sel: TextSelection | null = null;
  /** Pending/visible tooltip; client-local, driven by hover idle. */
  private tipState: { w: Widget; x: number; y: number; shown: boolean } | null = null;
  private tipTimer: ReturnType<typeof setTimeout> | null = null;

  onAux: ((button: number) => void) | null = null;

  sounds: Record<string, string> = {};
  playSound: ((src: string) => void) | null = null;

  /** Session-registered shortcuts (canonical combo string -> id). */
  private keyCombos = new Map<string, number>();
  private keyComboCb: ((id: number) => void) | null = null;
  private focused: Widget | null = null;
  /** Ring shows only for keyboard-driven focus (":focus-visible" semantics). */
  private focusRing = false;
  /** Own multi-click detection (like native toolkits), e.detail is unreliable. */
  private lastDown = { t: 0, x: 0, y: 0, count: 0, target: null as Widget | null };
  /** The glass that captured the in-flight right-button gesture (null = none;
   * right-click then falls through to context menus). */
  private rightGlass: Glass | null = null;
  /** True from a captured right-press until the matching contextmenu event
   * consumes it. The browser fires contextmenu AFTER pointerup (which clears
   * rightGlass), so this flag, not rightGlass, is what suppresses the menu. */
  private rightCaptured = false;

  // -- per-subtree retained display lists (see Widget.paintTree) --
  /** The list the last build produced. */
  private prev: DisplayList | null = null;
  /** prev when this frame may splice from it, null when everything rebuilds. */
  reuseList: DisplayList | null = null;
  /** Set by the untargeted invalidate: the frame after it takes no shortcuts. */
  private full = true;
  /**
   * Turns off per-subtree layout skipping for this frame, for the cases the
   * dirty flags do not describe: a resize, an untargeted invalidation, or a
   * geometric animation, which offsets bounds after layout and needs every one
   * of them re-derived next frame. See layoutSubtree.
   */
  layoutAll = true;
  /** True while a slide or fade is mid-flight (set by animGeometry). */
  private animating = false;
  private reused = 0;
  /**
   * The device pixel ratio text was last measured at, and its change counter.
   * Widget-level text caches (a label's truncation, its wrapped lines, its
   * natural size) key on it: the same string at the same font measures a hair
   * differently at a different DPR.
   */
  private dpr = 0;
  private measureEpochN = 0;

  /** How many commands the last build copied instead of rebuilding. */
  get reusedCommands(): number {
    return this.reused;
  }

  noteReuse(n: number): void {
    this.reused += n;
  }

  /** Changes whenever text starts measuring differently under the widgets. */
  measureEpoch(): number {
    return this.measureEpochN;
  }

  constructor(
    readonly canvas: HTMLCanvasElement,
    engine?: TextEngine,
  ) {
    this.surface = new Surface(canvas, engine);
    this.semantics = new SemanticsLayer(this);
    this.surface.onBuild = (dl, env) => {
      // text commands record the line box they will paint into, so a partial
      // frame can tell which text a damage rect actually touches
      dl.palette = themeEpoch();
      dl.measure ??= (f, str) => {
        const r = this.measure(f, str);
        return { w: r.width, ascent: r.ascent, descent: r.descent };
      };
      this.beforePaint?.(env);
      this.surface.background = theme.bg;
      const resized = env.width !== this.lastSize.w || env.height !== this.lastSize.h;
      if (resized) {
        this.lastSize = { w: env.width, h: env.height };
        this.overlay = null; // anchored popovers don't survive a resize
        this.clearTip();
      }
      if (env.dpr > 0 && env.dpr !== this.dpr) {
        // text measures a hair differently per DPR, and widgets cache
        // measurements: drop every one of them and rebuild
        this.dpr = env.dpr;
        this.measureEpochN++;
        this.full = true;
      }
      // the reuse gate. splicing is only sound when every change since the
      // last frame was attributed to a widget, so anything global
      // turns it off for the frame and every widget re-records its range
      this.reuseList =
        this.full || resized || this.structural || this.prev?.palette !== dl.palette ? null : this.prev;
      this.layoutAll = this.full || resized || this.structural || this.animating;
      this.full = false;
      this.reused = 0;
      if (!this.root) {
        this.prev = dl;
        return;
      }
      const barH = this.menubar ? menubarH() : 0;
      this.root.ui = this;
      this.root.bounds = rect(0, barH, env.width, env.height - barH);
      layoutSubtree(this.root);
      this.animGeometry(dl); // lays down the marking too
      this.paintRoot(dl, this.root);
      if (this.menubar) {
        this.menubar.ui = this;
        this.menubar.bounds = rect(0, 0, env.width, barH);
        this.markPaint(this.menubar);
        this.paintRoot(dl, this.menubar);
      }
      if (this.overlay) {
        this.overlay.ui = this;
        layoutSubtree(this.overlay);
        this.markPaint(this.overlay);
        this.paintRoot(dl, this.overlay);
      }
      this.paintTip(dl, env.width, env.height);
      if (this.banner) {
        this.banner.ui = this;
        this.banner.bounds = rect(0, 0, env.width, env.height);
        layoutSubtree(this.banner);
        this.markPaint(this.banner);
        this.paintRoot(dl, this.banner);
      }
      this.prev = dl;
      this.semantics.schedule();
    };

    canvas.addEventListener('contextmenu', (e) => {
      e.preventDefault(); // the framework owns right-click
      // fires after pointerup, so rightGlass is already cleared. the
      // capture flag is what tells us this right-press was a glass gesture
      if (this.rightCaptured) {
        this.rightCaptured = false;
        return;
      }
      const [x, y] = this.pos(e);
      this.contextClick(x, y);
    });

    // unprevented, the browser navigates its history and leaves the app
    const AUX = new Set([3, 4]);
    for (const name of ['pointerup', 'auxclick', 'mouseup'] as const) {
      canvas.addEventListener(name, (e) => {
        if (AUX.has((e as MouseEvent).button)) e.preventDefault();
      });
    }

    canvas.addEventListener('pointerdown', (e) => {
      if (AUX.has(e.button)) {
        e.preventDefault();
        this.auxDown(e.button + 1); // the DOM counts from 0, the SDK from 1
        return;
      }
      if (e.button === 2) {
        // a glass subscribed to right gestures captures the right button
        // its context menu never opens (the app chose orbit/measure/etc)
        const [x, y] = this.pos(e);
        const g = this.glassRightAt(this.root, x, y);
        if (g) {
          this.rightGlass = g;
          this.rightCaptured = true; // suppress the contextmenu that follows
          canvas.setPointerCapture(e.pointerId);
          g.rPickAt(x, y);
        }
        e.preventDefault();
        return;
      }
      if (e.button !== 0) return; // middle press: nothing routes it
      const [x, y] = this.pos(e);
      // any press either starts a new selection (selectable targets re-set it
      // in onPointerDown) or clears the old one
      this.clearSelection();
      this.clearTip();
      if (this.overlay) {
        const overlayHit = this.overlay.hitTest(x, y);
        if (!overlayHit) {
          // click-away closes and swallows the press
          // except on the menu bar, where the press opens the next menu in
          // the same gesture
          this.closeOverlay();
          const barHit = this.menubar?.hitTest(x, y);
          if (barHit) barHit.onPointerDown(x, y, 1);
          e.preventDefault();
          return;
        }
        this.active = overlayHit;
        canvas.setPointerCapture(e.pointerId);
        overlayHit.onPointerDown(x, y, 1);
        e.preventDefault();
        return;
      }
      const hit = this.menubar?.hitTest(x, y) ?? this.root?.hitTest(x, y) ?? null;
      const now = performance.now();
      const d = this.lastDown;
      d.count = now - d.t < 450 && Math.hypot(x - d.x, y - d.y) < 6 && hit === d.target ? d.count + 1 : 1;
      d.t = now;
      d.x = x;
      d.y = y;
      d.target = hit;
      this.active = hit;
      this.setFocus(hit?.focusable ? hit : null, false);
      if (hit) {
        canvas.setPointerCapture(e.pointerId);
        hit.onPointerDown(x, y, d.count);
      }
      e.preventDefault();
    });

    canvas.addEventListener('pointermove', (e) => {
      const [x, y] = this.pos(e);
      if (this.rightGlass) {
        this.rightGlass.rDragAt(x, y);
        return;
      }
      if (this.active) {
        this.active.onPointerDrag(x, y);
        return;
      }
      const hit =
        this.overlay?.hitTest(x, y) ?? this.menubar?.hitTest(x, y) ?? this.root?.hitTest(x, y) ?? null;
      if (hit !== this.hovered) {
        this.hovered?.onHoverChange(false);
        this.hovered = hit;
        hit?.onHoverChange(true);
      }
      this.tipCandidate(hit, x, y);
      // hover first, cursor second: positional cursors (a table's column
      // dividers) update their state in onPointerHover
      hit?.onPointerHover(x, y);
      canvas.style.cursor = hit?.cursor() ?? 'default';
    });

    canvas.addEventListener('pointerup', (e) => {
      const [x, y] = this.pos(e);
      if (e.button === 2) {
        this.rightGlass?.rDropAt(x, y);
        this.rightGlass = null;
        return;
      }
      this.active?.onPointerUp(x, y);
      this.active = null;
    });

    canvas.addEventListener(
      'wheel',
      (e) => {
        const [x, y] = this.pos(e);
        this.clearTip();
        const scale = e.deltaMode === 1 ? 16 : 1;
        let dx = e.deltaX * scale;
        let dy = e.deltaY * scale;
        if (e.shiftKey && dx === 0) {
          // mouse-wheel convention: shift turns vertical into horizontal
          dx = dy;
          dy = 0;
        }
        // a modal dialog confines the wheel to itself, just as it does clicks:
        // scrolling the scrim (or anywhere outside the dialog's own scroll) is
        // swallowed rather than leaking to the background
        const scope = this.lastDialog(this.root) ?? this.root;
        // a subscribed glass under the point owns the wheel: custom
        // interaction (cameras, zoomable canvases) outranks whatever
        // scroll-view sits beneath the pane
        const g = this.glassWheelAt(scope, x, y);
        if (g) {
          g.wheelAt(x, y, dx, dy);
          e.preventDefault();
          return;
        }
        const sv = this.scrollViewAt(scope, x, y);
        if (sv) {
          if (dy) sv.scrollBy(dy);
          if (dx && sv instanceof Scroller) sv.scrollByX(dx);
          e.preventDefault();
        } else if (scope !== this.root) {
          e.preventDefault(); // a modal is open: swallow the wheel, never scroll the background
        }
      },
      { passive: false },
    );

    window.addEventListener('keydown', (e) => {
      if (e.key === 'Tab') {
        this.moveFocus(e.shiftKey ? -1 : 1);
        e.preventDefault();
        return;
      }
      if (this.focused?.onKey(e)) {
        e.preventDefault();
        return;
      }
      // menu key equivalents fire once the focused widget declines, before
      // app-registered combos (NSMenu's spirit, minus its absolute priority),
      // then app combos, then built-ins.
      const combo = comboOf(e);
      if (combo) {
        const menuId = this.menuCombos.get(combo);
        if (menuId != null && this.menubar?.perform(menuId)) {
          e.preventDefault();
          return;
        }
        const id = this.keyCombos.get(combo);
        if (id != null) {
          this.keyComboCb?.(id);
          e.preventDefault();
          return;
        }
      }
      if ((e.metaKey || e.ctrlKey) && e.key === 'c') {
        const text = this.selectedText();
        if (text) {
          void navigator.clipboard.writeText(text).catch(() => {});
          e.preventDefault();
        }
      } else if (e.key === 'Escape') {
        if (this.overlay) {
          this.closeOverlay();
        } else {
          const dialog = this.lastDialog(this.root);
          if (dialog?.onDismiss) {
            dialog.sound('close');
            dialog.onDismiss();
          } else this.clearSelection();
        }
      }
    });

    // belt and braces: honor a native copy gesture if the browser fires one
    document.addEventListener('copy', (e) => {
      const text = this.selectedText();
      if (text && e.clipboardData) {
        e.clipboardData.setData('text/plain', text);
        e.preventDefault();
      }
    });
  }

  setRoot(w: Widget): void {
    // a tree swap (mount/remount) invalidates every stateful reference
    this.setFocus(null, false);
    this.clearSelection();
    this.clearTip();
    this.hovered = null;
    this.active = null;
    this.overlay = null;
    this.root = w;
    w.ui = this;
    this.invalidate();
  }

  // -- tooltips -----------------------------------------------------------------

  /** Track the hover target. A widget with a tip shows it after ~600ms idle. */
  private tipCandidate(hit: Widget | null, x: number, y: number): void {
    if (!hit?.tip) {
      this.clearTip();
      return;
    }
    const t = this.tipState;
    if (t && t.w === hit) {
      if (!t.shown) {
        t.x = x;
        t.y = y; // follow the pointer until shown, then hold still
      }
      return;
    }
    this.clearTip();
    this.tipState = { w: hit, x, y, shown: false };
    this.tipTimer = setTimeout(() => {
      this.tipTimer = null;
      if (this.tipState) {
        this.tipState.shown = true;
        this.invalidate();
      }
    }, 600);
  }

  private clearTip(): void {
    if (this.tipTimer != null) {
      clearTimeout(this.tipTimer);
      this.tipTimer = null;
    }
    if (this.tipState) {
      const wasShown = this.tipState.shown;
      this.tipState = null;
      if (wasShown) this.invalidate();
    }
  }

  /** Painted above the tree and overlay, below the connection banner. */
  private paintTip(dl: DisplayList, vw: number, vh: number): void {
    const t = this.tipState;
    const text = t?.shown ? t.w.tip : null;
    if (!t || !text) return;
    const f = font(12);
    const m = this.measure(f, text);
    const padX = 8;
    const padY = 5;
    const w = Math.ceil(m.width) + padX * 2;
    const h = Math.ceil(m.ascent + m.descent) + padY * 2;
    const x = Math.max(4, Math.min(t.x + 12, vw - w - 4));
    const y = Math.max(4, Math.min(t.y + 18, vh - h - 4));
    const box = rect(x, y, w, h);
    dl.shadow(box, { blur: 10, color: withAlpha(BLACK, 0.35), offset: [0, 2], radius: metrics.radiusControl });
    dl.rect(box, {
      color: theme.panelAlt,
      radius: metrics.radiusControl,
      borderWidth: metrics.borderWidth,
      borderColor: theme.edgeSoft,
    });
    dl.text(text, x + padX, y + padY, f, theme.ink);
  }

  openOverlay(w: Widget): void {
    w.ui = this;
    this.overlay = w;
    this.invalidate();
  }

  /** Install the session's registered shortcuts (replaces the previous set). */
  setKeyCombos(combos: Map<string, number>, cb: (id: number) => void): void {
    this.keyCombos = combos;
    this.keyComboCb = cb;
  }

  /**
   * Install (or clear) the in-window menu bar from the server's menu spec.
   * The bar is client-local and picks flow through onPick as session-level `menu` events.
   */
  setMenubar(menus: MenuSpec[], onPick: (id: number) => void): void {
    const withItems = menus.filter((m) => m.items?.length);
    if (!withItems.length) {
      this.menubar = null;
      this.menuCombos.clear();
    } else {
      this.menubar = new MenuBar(withItems, onPick);
      this.menubar.ui = this;
      this.menuCombos = this.menubar.combos();
    }
    this.invalidate();
  }

  setSounds(tokens: Record<string, string>): void {
    this.sounds = tokens;
  }

  sound(token: string): void {
    const src = this.sounds[token];
    if (src && this.playSound) this.playSound(src);
  }

  auxDown(button: number): void {
    this.clearTip();
    if (this.overlay) {
      this.closeOverlay();
      return;
    }
    this.onAux?.(button);
  }

  /** Right-click: open the context menu of the deepest carrier under (x, y). */
  contextClick(x: number, y: number): void {
    this.clearTip();
    this.dismissOverlay(); // a previous menu/popover yields to the new press
    const hit = this.root?.hitTest(x, y) ?? null;
    for (let w: Widget | null = hit; w; w = w.parent) {
      if (w.contextItems.length) {
        const pick = w.onContextPick ?? (() => {});
        ContextMenu.open(this, w.contextItems, pick, x, y);
        return;
      }
    }
  }

  closeOverlay(): void {
    if (this.overlay) {
      this.sound('close');
      this.dismissOverlay();
    }
  }

  dismissOverlay(): void {
    if (this.overlay) {
      this.overlay = null;
      this.invalidate();
    }
  }

  private lastDialog(w: Widget | null): Dialog | null {
    if (!w) return null;
    let found: Dialog | null = w instanceof Dialog ? w : null;
    for (const c of w.children) {
      const d = this.lastDialog(c);
      if (d) found = d;
    }
    return found;
  }

  /**
   * Apply metric tokens (see ui/metrics.ts) and invalidate everything derived
   * from them: sizes memoized under measureEpoch recompute, and the next frame
   * rebuilds and re-lays-out the whole tree. Retained display lists hold
   * resolved numbers, so nothing recorded before the change may be spliced
   * after it. Metrics snap rather than tween: animating geometry would
   * relayout continuously for no design payoff, where a color fade costs
   * nothing. Mirrors the native Ui.ApplyMetrics.
   */
  applyMetrics(tokens: Record<string, number>): void {
    applyMetricTokens(tokens);
    this.measureEpochN++;
    this.invalidate();
  }

  /**
   * Ask for a frame that rebuilds the whole display list
   */
  invalidate(): void {
    this.full = true;
    this.surface.invalidate();
  }

  /**
   * Ask for a frame in which only w (and whatever else is marked) repaints
   */
  damage(w: Widget | null): void {
    if (w) {
      w.dirty = true;
      w.invalidateLayout();
    }
    this.surface.invalidate();
  }

  /** Ask for a frame without forcing a rebuild (a widget marked itself) */
  wake(): void {
    this.surface.invalidate();
  }

  // -- geometric animation ------------------------------------------------------

  private structural = false;
  private frameAt = 0;

  /**
   * Mark the next frame as one that applied structural server ops
   * (insert/remove/move): layout deltas in that frame animate. Local causes
   * of reflow never set it
   */
  noteStructural(): void {
    this.structural = true;
  }

  /**
   * Runs between layout and paint. On a structural frame it starts a
   * decaying paint offset for every widget the reflow moved and a fade-in
   * for every widget it introduced. On every frame it advances the tweens
   * and applies the offsets. Bounds mutated here are re-derived by layout
   * next frame, so the offset never contaminates real geometry, and
   * because prevX/prevY track *every* frame, scroll-induced movement is
   * already absorbed by the time a structural frame compares against them.
   */
  private animGeometry(dl: DisplayList): void {
    const now = performance.now();
    let dt = (now - this.frameAt) / 1000;
    if (dt <= 0 || dt > 0.05) dt = 0.016; // first frame, or waking from idle
    this.frameAt = now;
    this.animating = this.animWalk(this.root!, dt);
    if (this.animating) {
      // geometric tweens move bounds every frame. the surface must rebuild,
      // never replay a retained list (geomAnimation gates renderPartial, and
      // implies wantsAnimation)
      dl.geomAnimation = true;
    }
    this.structural = false;
  }

  /**
   * Aggregate needsPaint bottom-up and consume the dirty bits. A widget left
   * false is one whose whole subtree can splice last frame's commands. Every
   * root is marked before it is painted. A widget marked true makes every
   * ancestor true, so a dirty widget is always reached by the walk that clears
   * it.
   */
  private markPaint(w: Widget): boolean {
    let need = w.selfNeedsPaint();
    w.dirty = false;
    for (const c of w.children) if (this.markPaint(c)) need = true;
    w.needsPaint = need;
    return need;
  }

  /**
   * Paint one of the four roots and record the command range it produced, so
   * the next frame can splice it.
   */
  private paintRoot(dl: DisplayList, w: Widget): void {
    const at = dl.cmds.length;
    w.paintTree(dl, this.reuseList && w.cacheOK ? w.cacheOff : -1);
    w.cacheOff = at;
    w.cacheLen = dl.cmds.length - at;
    w.cacheOK = true;
  }

  private animWalk(w: Widget, dt: number): boolean {
    let animating = false;
    const b = w.bounds;
    if (!w.hadFrame) {
      w.hadFrame = true;
      if (this.structural) w.fadeR = 1; // introduced by an op - fade in
    } else if (this.structural && (b.x !== w.prevX || b.y !== w.prevY)) {
      // retarget from the current *visual* position, so a mid-slide widget
      // moved again glides on without snapping
      const k = 1 - easeOut(1 - w.animR);
      w.animDx = w.prevX + w.animDx * k - b.x;
      w.animDy = w.prevY + w.animDy * k - b.y;
      w.animR = 1;
    }
    w.prevX = b.x;
    w.prevY = b.y;
    if (w.animR > 0) {
      w.animR = Math.max(0, w.animR - dt / SLIDE_DUR);
      const k = 1 - easeOut(1 - w.animR);
      b.x += w.animDx * k;
      b.y += w.animDy * k;
      if (w.animR > 0) animating = true;
    }
    if (w.fadeR > 0) {
      w.fadeR = Math.max(0, w.fadeR - dt / FADE_DUR);
      if (w.fadeR > 0) animating = true;
    }
    // marking rides along with the pass that already visits every widget: the
    // animation bookkeeping above is what settles the bounds a splice gets
    // compared against
    let need = w.selfNeedsPaint();
    w.dirty = false;
    for (const c of w.children) {
      if (this.animWalk(c, dt)) animating = true;
      if (c.needsPaint) need = true;
    }
    w.needsPaint = need;
    return animating;
  }

  measure(f: Font, text: string): TextRun {
    return this.surface.renderer.measure(f, text);
  }

  /** Warm the image cache ahead of first paint (the `resource` op). */
  preloadImage(src: string): void {
    this.surface.renderer.images.get(src);
  }

  // -- focus ------------------------------------------------------------------

  setFocus(w: Widget | null, viaKeyboard: boolean): void {
    if (this.focused !== w || this.focusRing !== viaKeyboard) {
      const old = this.focused;
      this.focused = w;
      this.focusRing = viaKeyboard;
      if (old !== w) {
        old?.onFocusChange(false);
        w?.onFocusChange(true);
      }
      // mirror focus into the semantics layer so AT announces it, except for
      // text fields, whose DOM focus belongs to the input funnel
      if (!(w instanceof TextField)) this.semantics.focusWidget(w);
      this.invalidate();
    }
  }

  isFocused(w: Widget): boolean {
    return this.focused === w;
  }

  showFocusRing(w: Widget): boolean {
    return this.focused === w && this.focusRing;
  }

  private moveFocus(dir: 1 | -1): void {
    const order: Widget[] = [];
    const collect = (w: Widget) => {
      if (w.focusable) order.push(w);
      for (const c of w.children) collect(c);
    };
    // focus is trapped inside the topmost dialog while one is open, Tab
    // must not wander into the inert background
    const scope = this.lastDialog(this.root) ?? this.root;
    if (scope) collect(scope);
    if (!order.length) return;
    const idx = this.focused ? order.indexOf(this.focused) : -1;
    const next = order[(idx + dir + order.length) % order.length]!;
    this.setFocus(next, true);
    this.revealWidget(next);
  }

  /** Scroll the enclosing scrollers to make a widget visible (Tab focus,
   * and the server's `revealSeq` command). */
  revealWidget(w: Widget): void {
    for (let p = w.parent; p; p = p.parent) {
      if (p instanceof Scroller && p.scrollable) {
        // bounds are from the last layout; adjusting an outer scroller after
        // an inner one uses slightly stale offsets
        const v = p.bounds;
        const top = w.bounds.y;
        const bot = w.bounds.y + w.bounds.h;
        if (top < v.y) p.scrollBy(top - v.y - 8);
        else if (bot > v.y + v.h) p.scrollBy(bot - (v.y + v.h) + 8);
      }
    }
  }

  /** The deepest wheel-subscribed glass under the pointer. It cannot ride
   * hitTest: glass hit-interactivity is gated on onPick so an unsubscribed
   * pane never steals presses, and a wheel-only glass (a zoomable canvas
   * with no pick) must own the wheel without starting to swallow clicks. */
  private glassWheelAt(w: Widget | null, x: number, y: number): Glass | null {
    if (!w) return null;
    if (w.clips && !contains(w.bounds, x, y)) return null;
    for (let i = w.children.length - 1; i >= 0; i--) {
      const found = this.glassWheelAt(w.children[i]!, x, y);
      if (found) return found;
    }
    return w instanceof Glass && w.onWheelAt && contains(w.bounds, x, y) ? w : null;
  }

  /** The deepest right-gesture-subscribed glass under the pointer, same
   * shape as glassWheelAt, gated on rpick so inert glass never eats the
   * context menu. */
  private glassRightAt(w: Widget | null, x: number, y: number): Glass | null {
    if (!w) return null;
    if (w.clips && !contains(w.bounds, x, y)) return null;
    for (let i = w.children.length - 1; i >= 0; i--) {
      const found = this.glassRightAt(w.children[i]!, x, y);
      if (found) return found;
    }
    return w instanceof Glass && w.onRPick && contains(w.bounds, x, y) ? w : null;
  }

  private scrollViewAt(w: Widget | null, x: number, y: number): Scroller | TextArea | null {
    if (!w) return null;
    if (w.clips && !contains(w.bounds, x, y)) return null;
    for (let i = w.children.length - 1; i >= 0; i--) {
      const found = this.scrollViewAt(w.children[i]!, x, y);
      if (found) return found;
    }
    const scrolls =
      (w instanceof Scroller && (w.scrollable || w.scrollableX)) ||
      (w instanceof TextArea && w.scrollable);
    return scrolls && contains(w.bounds, x, y) ? (w as Scroller | TextArea) : null;
  }

  // -- selection --------------------------------------------------------------

  selectionFor(label: Label): TextSelection | null {
    return this.sel?.label === label ? this.sel : null;
  }

  setSelection(label: Label, start: number, end: number): void {
    this.sel = { label, start, end };
    this.invalidate();
  }

  moveSelectionEnd(label: Label, end: number): void {
    if (this.sel?.label === label && this.sel.end !== end) {
      this.sel.end = end;
      this.invalidate();
    }
  }

  clearSelection(): void {
    if (this.sel) {
      this.sel = null;
      this.invalidate();
    }
  }

  selectedText(): string {
    return this.sel ? this.sel.label.glyphText(this.sel.start, this.sel.end) : '';
  }

  private pos(e: { clientX: number; clientY: number }): [number, number] {
    const r = this.canvas.getBoundingClientRect();
    return [e.clientX - r.left, e.clientY - r.top];
  }
}

/**
 * Canonical combo string for a key event (cmd+ctrl+alt+shift+<key>)
 * matching the server SDK's normalization. null when no modifier is held
 * (bare keys belong to widgets and text input, never to app shortcuts).
 */
function comboOf(e: KeyboardEvent): string | null {
  if (!e.metaKey && !e.ctrlKey && !e.altKey) return null;
  const key = e.key.toLowerCase();
  if (key === 'meta' || key === 'control' || key === 'alt' || key === 'shift') return null;
  let out = '';
  if (e.metaKey) out += 'cmd+';
  if (e.ctrlKey) out += 'ctrl+';
  if (e.altKey) out += 'alt+';
  if (e.shiftKey) out += 'shift+';
  const name = key === ' ' ? 'space' : key.replace(/^arrow/, '');
  return out + name;
}
