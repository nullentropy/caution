import { BLACK, hex, withAlpha } from '../gfx/color';
import { font } from '../gfx/font';
import { TableView } from '../ui/tableview';
import { bumpThemeEpoch, theme } from '../ui/theme';
import { Ui } from '../ui/ui';
import { Label, Panel } from '../ui/widgets';
import { Widget } from '../ui/widget';
import { NodeStore } from './inflate';
import type { ClientEvent, PatchOp, ServerMsg } from './types';

/**
 * Client half of the wire protocol: connects, applies mount/patch messages to
 * the widget tree, and ships subscribed semantic events back. Reconnects with
 * backoff. The server re-sends a full mount on a fresh connection.
 */
export class Session {
  private ws: WebSocket | null = null;
  private store: NodeStore;
  private lastSeq = 0;
  private retryMs = 500;
  private mounted = false;
  /** True once any mount landed - from then on the tree outlives the socket. */
  private everMounted = false;

  constructor(
    private ui: Ui,
    private url: string,
  ) {
    this.store = new NodeStore({ event: (id, ev, value) => this.sendEvent(id, ev, value) });
    ui.onAux = (button) => this.sendEvent(0, 'aux', button);
    this.showStatus(`connecting to ${url} …`);
    this.connect();
    // Viewport reporting: the connect URL carries the initial size (so it is
    // known at mount); later changes flow as a debounced session-level event
    // (node id 0 - no widget owns the viewport).
    let timer: ReturnType<typeof setTimeout> | null = null;
    window.addEventListener('resize', () => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        timer = null;
        this.sendEvent(0, 'resize', { w: window.innerWidth, h: window.innerHeight });
      }, 250);
    });
  }

  private connect(): void {
    // Session resume: the server keeps a disconnected session's state briefly;
    // presenting its id on reconnect (or after a page reload, sessionStorage
    // survives F5, not new tabs) reattaches instead of starting over.
    const sid = sessionStorage.getItem('caution:sid');
    const q = new URLSearchParams({
      vw: String(window.innerWidth),
      vh: String(window.innerHeight),
    });
    if (sid) {
      q.set('resume', sid);
      // Present the last seq we applied: if nothing changed server-side, the
      // resume comes back as a bare ack and our whole world stays put.
      q.set('seq', String(this.lastSeq));
    }
    const ws = new WebSocket(`${this.url}?${q}`);
    this.ws = ws;
    ws.onopen = () => {
      this.retryMs = 500;
    };
    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data as string) as ServerMsg;
      this.handle(msg);
    };
    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.mounted = false;
      // A tree we've shown stays up, visible and locally interactive
      // (scroll, selection), under a reconnect banner. Only a session that
      // never mounted falls back to the full status screen.
      if (this.everMounted) this.showBanner('connection lost - reconnecting…');
      else this.showStatus(`server unreachable at ${this.url} - retrying`);
      setTimeout(() => this.connect(), this.retryMs);
      this.retryMs = Math.min(this.retryMs * 1.6, 8000);
    };
  }

  private handle(msg: ServerMsg): void {
    this.lastSeq = msg.seq;
    if (msg.t === 'mount') {
      if (msg.sid) sessionStorage.setItem('caution:sid', msg.sid);
      if (msg.theme) this.applyTheme(msg.theme);
      if (msg.metrics) this.ui.applyMetrics(msg.metrics);
      if (msg.title) document.title = msg.title;
      if (msg.keys) this.applyKeys(msg.keys);
      this.applyMenu(msg.menu ?? []);
      if (msg.resources?.images) for (const src of msg.resources.images) this.ui.preloadImage(src);
      this.store.byId.clear();
      const root = this.store.build(msg.root);
      this.mounted = true;
      this.everMounted = true;
      this.ui.setRoot(root);
      this.hideBanner();
      // headless capture (go/cmd/goldens) waits for this before screenshotting
      ((window as any).__caution ??= { frames: 0, mounted: false }).mounted = true;
    } else if (msg.t === 'resume') {
      // nothing changed while we were away: the tree we're showing is right
      this.mounted = true;
      this.everMounted = true;
      this.hideBanner();
    } else if (msg.t === 'patch' && this.mounted) {
      // ops that name the node they touch mark just that node (the paint layer
      // splices everything around it). ops that reshape the tree or the
      // client's chrome fall back to rebuilding the whole frame.
      let full = false;
      for (const op of msg.ops) if (this.applyOp(op)) full = true;
      if (full) this.ui.invalidate();
    }
  }

  /** Apply one op. True if the frame after it has to be rebuilt whole. */
  private applyOp(op: PatchOp): boolean {
    if (op.op === 'set') {
      const w = this.store.byId.get(op.id);
      if (w) {
        this.store.apply(w, op.id, op.p);
        this.ui.damage(w);
      }
    } else if (op.op === 'insert') {
      const parent = this.store.byId.get(op.parent);
      if (!parent) return true;
      this.ui.noteStructural();
      const w = this.store.build(op.node);
      w.parent = parent;
      parent.children.splice(Math.min(op.index, parent.children.length), 0, w);
      return true;
    } else if (op.op === 'remove') {
      this.ui.noteStructural();
      this.store.remove(op.id);
      return true;
    } else if (op.op === 'rows') {
      const w = this.store.byId.get(op.id);
      if (w instanceof TableView) w.applyRows(op.start, op.rows, op.meta, op.reset ?? false); // marks itself
    } else if (op.op === 'theme') {
      this.applyTheme(op.tokens); // every widget's colors: whole frame
    } else if (op.op === 'metrics') {
      // geometry snaps (no tween) and invalidates measurement caches along
      // with the frame
      this.ui.applyMetrics(op.metrics);
    } else if (op.op === 'title') {
      document.title = op.title;
    } else if (op.op === 'keys') {
      this.applyKeys(op.keys);
    } else if (op.op === 'menu') {
      this.applyMenu(op.menu ?? []);
      return true;
    } else if (op.op === 'resource') {
      for (const src of op.images ?? []) this.ui.preloadImage(src);
    } else if (op.op === 'move') {
      const w = this.store.byId.get(op.id);
      const parent = this.store.byId.get(op.parent);
      if (!w || !parent) return true;
      this.ui.noteStructural();
      if (w.parent) {
        const i = w.parent.children.indexOf(w);
        if (i >= 0) w.parent.children.splice(i, 1);
      }
      w.parent = parent;
      parent.children.splice(Math.min(op.index, parent.children.length), 0, w);
      return true;
    }
    return false;
  }

  private applyKeys(keys: { id: number; key: string }[]): void {
    this.ui.setKeyCombos(
      new Map(keys.map((k) => [k.key, k.id])),
      (id) => this.sendEvent(0, 'key', id),
    );
  }

  /** Realize the server's menu spec as the in-window menu bar. Picks are
   * session-level `menu` events like NSMenu picks natively. */
  private applyMenu(menu: { title: string; items: import('./types').MenuItemSpec[] }[]): void {
    this.ui.setMenubar(menu, (id) => this.sendEvent(0, 'menu', id));
  }

  private themeTween: number | null = null;

  /**
   * Retarget theme tokens. Widgets hold references to token Colors resolved
   * at inflate time, so tokens always mutate in place. The first application
   * (mount) snaps, since first paint has no before, and later ones fade over
   * ~180ms.
   */
  private applyTheme(tokens: Record<string, string>): void {
    const targets: { c: { r: number; g: number; b: number; a: number }; from: number[]; to: number[] }[] = [];
    for (const [k, v] of Object.entries(tokens)) {
      const c = hex(v);
      const existing = theme[k];
      if (existing) {
        targets.push({ c: existing, from: [existing.r, existing.g, existing.b, existing.a], to: [c.r, c.g, c.b, c.a] });
      } else {
        theme[k] = c; // brand-new tokens have nothing to fade from
      }
    }
    const snap = (): void => {
      for (const { c, to } of targets) {
        [c.r, c.g, c.b, c.a] = to as [number, number, number, number];
      }
      bumpThemeEpoch();
    };
    if (this.themeTween != null) {
      cancelAnimationFrame(this.themeTween);
      this.themeTween = null;
    }
    if (!this.everMounted || !targets.length) {
      snap();
      return;
    }
    const start = performance.now();
    const dur = 180;
    const step = () => {
      const t = Math.min(1, (performance.now() - start) / dur);
      const e = t * (2 - t); // ease-out
      for (const { c, from, to } of targets) {
        c.r = from[0]! + (to[0]! - from[0]!) * e;
        c.g = from[1]! + (to[1]! - from[1]!) * e;
        c.b = from[2]! + (to[2]! - from[2]!) * e;
        c.a = from[3]! + (to[3]! - from[3]!) * e;
      }
      bumpThemeEpoch();
      this.ui.invalidate();
      this.themeTween = t < 1 ? requestAnimationFrame(step) : null;
    };
    step();
  }

  private sendEvent(id: number, ev: string, value?: unknown): void {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    const msg: ClientEvent = { t: 'ev', id, ev, seq: this.lastSeq };
    if (value !== undefined) msg.value = value;
    this.ws.send(JSON.stringify(msg));
  }

  /**
   * Small client-owned pill over the live tree while reconnecting. The tree
   * beneath keeps working locally.
   */
  private showBanner(text: string): void {
    const f = font(12, { weight: 500 });
    const m = this.ui.measure(f, text);
    const padL = 12;
    const dot = 8;
    const gap = 8;
    const padR = 14;
    const root = new Panel(); // transparent, full-viewport carrier
    const pill = root.add(new Panel());
    pill.width = padL + dot + gap + Math.ceil(m.width) + padR;
    pill.height = 30;
    pill.anchors = { centerX: 0, top: 10 };
    pill.bg = theme.panelAlt;
    pill.radius = 15;
    pill.borderColor = theme.edge;
    pill.dropShadow = { blur: 14, color: withAlpha(BLACK, 0.4), offset: [0, 3] };
    const dotW = pill.add(new Panel());
    dotW.width = dot;
    dotW.height = dot;
    dotW.radius = dot / 2;
    dotW.bg = theme.accent;
    dotW.anchors = { left: padL, centerY: 0 };
    const label = pill.add(new Label(text, f, theme.inkDim));
    label.anchors = { left: padL + dot + gap, centerY: 0 };
    this.ui.banner = root;
    this.ui.invalidate();
  }

  private hideBanner(): void {
    if (this.ui.banner) {
      this.ui.banner = null;
      this.ui.invalidate();
    }
  }

  /** Minimal client-owned screen for the disconnected state. */
  private showStatus(text: string): void {
    const root: Widget = new Panel();
    const card = root.add(new Panel());
    card.anchors = { centerX: 0, centerY: -40 };
    card.width = 560;
    card.height = 120;
    card.bg = theme.panelAlt;
    card.radius = 12;
    card.borderColor = theme.edgeSoft;
    const title = card.add(new Label('caution', font(18, { weight: 600 })));
    title.anchors = { left: 24, top: 24 };
    const detail = card.add(new Label(text, font(13), theme.inkDim));
    detail.anchors = { left: 24, top: 58 };
    detail.selectable = true;
    this.ui.setRoot(root);
  }
}
