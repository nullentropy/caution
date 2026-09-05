// Node inflation: wire JSON -> widget instances from the fixed vocabulary.
// Prop application is idempotent so `set` ops reuse the same code path as
// initial construction.

import { Color, hex } from '../gfx/color';
import { font, MONO } from '../gfx/font';
import { Corners, Radius, Rect, rect } from '../gfx/geom';
import { Dock, HStack, ScrollView, Stack, StackAlign, VStack } from '../ui/containers';
import { ContextItem } from '../ui/contextmenu';
import { Progress, RadioGroup, Slider, Tabs } from '../ui/controls';
import { Dialog } from '../ui/dialog';
import { Glass } from '../ui/glass';
import { Grid, GridTrack } from '../ui/grid';
import { ImageView } from '../ui/imageview';
import { Select } from '../ui/select';
import { ShaderPane } from '../ui/shaderpane';
import { SplitView } from '../ui/splitview';
import { TableColumn, TableView } from '../ui/tableview';
import { TextArea } from '../ui/textarea';
import { TextField } from '../ui/textfield';
import { theme } from '../ui/theme';
import { Button, Checkbox, Label, Panel } from '../ui/widgets';
import { Anchors, DockSide, StackSize, Widget } from '../ui/widget';
import type { NodeJson, Props } from './types';

export interface EventSink {
  event(id: number, ev: string, value?: unknown): void;
}

const num = (v: unknown): number | null => (typeof v === 'number' ? v : null);
const str = (v: unknown): string | null => (typeof v === 'string' ? v : null);
const bool = (v: unknown): boolean | null => (typeof v === 'boolean' ? v : null);

function color(v: unknown): Color | null {
  const s = str(v);
  if (!s) return null;
  if (s.startsWith('$')) {
    const tok = theme[s.slice(1)];
    return tok ?? null;
  }
  return hex(s);
}

function radius(v: unknown): Radius | null {
  if (typeof v === 'number') return v;
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>;
    const c: Corners = { tl: num(o.tl) ?? 0, tr: num(o.tr) ?? 0, br: num(o.br) ?? 0, bl: num(o.bl) ?? 0 };
    return c;
  }
  return null;
}

function anchors(v: unknown): Anchors | null {
  if (!v || typeof v !== 'object') return null;
  const o = v as Record<string, unknown>;
  const a: Anchors = {};
  for (const k of ['left', 'right', 'top', 'bottom', 'centerX', 'centerY'] as const) {
    const n = num(o[k]);
    if (n != null) a[k] = n;
  }
  return a;
}

function frame(v: unknown): Rect | null {
  if (Array.isArray(v) && v.length === 4 && v.every((n) => typeof n === 'number')) {
    return rect(v[0] as number, v[1] as number, v[2] as number, v[3] as number);
  }
  return null;
}

function floatMap(v: unknown): Record<string, number> {
  const out: Record<string, number> = {};
  if (v && typeof v === 'object') {
    for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
      if (typeof val === 'number') out[k] = val;
    }
  }
  return out;
}

function effectSpec(v: unknown): Widget['effect'] {
  if (!v || typeof v !== 'object') return null;
  const o = v as Record<string, unknown>;
  const frag = str(o.frag);
  if (!frag) return null;
  return { frag, uniforms: floatMap(o.uniforms), animate: bool(o.animate) ?? false };
}

function stackSize(v: unknown): StackSize | null {
  if (!v || typeof v !== 'object') return null;
  const o = v as Record<string, unknown>;
  if (o.kind === 'fixed' && typeof o.px === 'number') return { kind: 'fixed', px: o.px };
  if (o.kind === 'fill') return { kind: 'fill', weight: num(o.weight) ?? 1 };
  if (o.kind === 'content') return { kind: 'content' };
  return null;
}

/** Layout props shared by every widget type. */
/**
 * Focus a widget once it is attached to a UI. A live widget focuses
 * synchronously; a node inserted by the same patch that focuses it has no
 * `ui` pointer until the next layout's adopt pass, so retry on a short
 * timer. Deliberately not requestAnimationFrame: throttled/occluded tabs
 * stop producing frames, and focus must not depend on paint.
 */
function whenReady(w: Widget, tries: number, fn: () => void): void {
  if (w.ui) {
    fn();
    return;
  }
  if (tries > 0) setTimeout(() => whenReady(w, tries - 1, fn), 16);
}

function contextItems(v: unknown): ContextItem[] {
  if (!Array.isArray(v)) return [];
  return v.map((it): ContextItem => {
    const o = (it ?? {}) as Record<string, unknown>;
    return { id: num(o.id) ?? undefined, title: str(o.title) ?? undefined, sep: bool(o.sep) ?? undefined };
  });
}

function applyCommon(w: Widget, p: Props): void {
  if ('frame' in p) w.frame = frame(p.frame);
  if ('anchors' in p) w.anchors = anchors(p.anchors);
  if ('dock' in p) w.dock = (str(p.dock) as DockSide) ?? 'fill';
  if ('stack' in p) w.stackSize = stackSize(p.stack) ?? { kind: 'content' };
  if ('span' in p) w.gridSpan = num(p.span) ?? 1;
  if ('width' in p) w.width = num(p.width);
  if ('height' in p) w.height = num(p.height);
  if ('clips' in p) w.clips = bool(p.clips) ?? false;
  if ('effect' in p) w.effect = effectSpec(p.effect);
  if ('windowDrag' in p) w.windowDrag = bool(p.windowDrag) ?? false;
  if ('outline' in p) w.outline = bool(p.outline) ?? false;
  if ('tip' in p) w.tip = str(p.tip) ?? null;
  if ('sound' in p) w.soundToken = str(p.sound) ?? null;
}

function applyLabelFont(w: Label, p: Props): void {
  const prev = w.font;
  const size = num(p.size) ?? prev.size;
  const weight = num(p.weight) ?? prev.weight;
  const italic = bool(p.italic) ?? prev.italic;
  const mono = bool(p.mono);
  const family = mono == null ? prev.family : mono ? MONO : undefined;
  w.font = font(size, { weight, italic, family: family ?? undefined });
}

/**
 * Registry of the widget vocabulary: construct by type, apply props, wire
 * subscribed events to the sink. Maintains the id->widget map used by patches.
 */
export class NodeStore {
  readonly byId = new Map<number, Widget>();

  constructor(private sink: EventSink) {}

  build(json: NodeJson): Widget {
    const w = this.construct(json);
    this.byId.set(json.id, w);
    this.apply(w, json.id, json.p ?? {});
    for (const kid of json.kids ?? []) {
      const c = this.build(kid);
      c.parent = w;
      w.children.push(c);
    }
    return w;
  }

  apply(w: Widget, id: number, p: Props): void {
    applyCommon(w, p);
    // context menus are type-agnostic, like tips but their pick needs the
    // node id, so they wire here rather than in applyCommon.
    if ('context' in p) w.contextItems = contextItems(p.context);
    if (this.subscribed(p, 'context')) {
      w.onContextPick = (cid) => this.sink.event(id, 'context', cid);
    }
    // universal one-shot commands, monotonic so they replay safely on
    // remount. The ~4s of 16ms retries: adoption waits on the first layout
    // pass, which a throttled/occluded tab can delay a long time
    if ('focusSeq' in p) {
      const seq = num(p.focusSeq) ?? 0;
      if (seq !== w.focusSeq) {
        w.focusSeq = seq;
        whenReady(w, 250, () => {
          w.grabFocus();
          w.ui!.revealWidget(w); // server focus also brings it into view
        });
      }
    }
    if ('revealSeq' in p) {
      const seq = num(p.revealSeq) ?? 0;
      if (seq !== w.revealSeq) {
        w.revealSeq = seq;
        whenReady(w, 250, () => w.ui!.revealWidget(w));
      }
    }
    if (w instanceof Panel) {
      if ('bg' in p) w.bg = color(p.bg);
      if ('borderColor' in p) w.borderColor = color(p.borderColor);
      if ('borderWidth' in p) w.borderWidth = num(p.borderWidth) ?? 1;
      if ('radius' in p) w.radius = radius(p.radius) ?? 0;
      if ('shadow' in p) {
        const s = p.shadow as Record<string, unknown> | null;
        w.dropShadow = s
          ? {
              blur: num(s.blur) ?? 10,
              color: color(s.color) ?? hex('#00000080'),
              offset: [num(s.dx) ?? 0, num(s.dy) ?? 0],
            }
          : null;
      }
    } else if (w instanceof Label) {
      if ('text' in p) w.text = str(p.text) ?? '';
      applyLabelFont(w, p);
      if ('color' in p) w.color = color(p.color) ?? theme.ink;
      if ('selectable' in p) w.selectable = bool(p.selectable) ?? false;
      if ('wrap' in p) w.wrap = bool(p.wrap) ?? false;
    } else if (w instanceof Button) {
      if ('label' in p) w.label = str(p.label) ?? '';
      if ('primary' in p) w.primary = bool(p.primary) ?? false;
      if ('radius' in p) w.radius = num(p.radius);
      if (this.subscribed(p, 'click')) w.onClick = () => this.sink.event(id, 'click');
    } else if (w instanceof Checkbox) {
      if ('label' in p) w.label = str(p.label) ?? '';
      if ('checked' in p) w.checked = bool(p.checked) ?? false;
      if (this.subscribed(p, 'toggle')) w.onToggle = (checked) => this.sink.event(id, 'toggle', checked);
    } else if (w instanceof TextField) {
      // covers TextArea too - it extends TextField and shares the contract.
      if (w instanceof TextArea && 'rows' in p) w.rows = num(p.rows) ?? 4;
      if ('radius' in p) w.radius = num(p.radius);
      // local echo: while the user is typing, the client's value is
      // authoritative, a focused field ignores server sets (README.md),
      // unless this patch carries a fresh overrideSeq (SetValueNow).
      const forced = 'overrideSeq' in p && (num(p.overrideSeq) ?? 0) !== w.overrideSeq;
      if (forced) w.overrideSeq = num(p.overrideSeq) ?? 0;
      if ('value' in p) {
        const v = str(p.value) ?? '';
        if (forced) w.forceValue(v);
        else if (!w.focused) w.value = v;
      }
      if ('placeholder' in p) w.placeholder = str(p.placeholder) ?? '';
      if ('size' in p) w.fontSize = num(p.size) ?? 14;
      if ('mono' in p) w.mono = bool(p.mono) ?? false;
      // resetSeq force-clears (even focused)
      if ('resetSeq' in p) {
        const seq = num(p.resetSeq) ?? 0;
        if (seq !== w.resetSeq) {
          w.resetSeq = seq;
          w.forceClear();
        }
      }
      if (this.subscribed(p, 'input') || this.subscribed(p, 'commit')) {
        const wantInput = this.subscribed(p, 'input');
        const wantCommit = this.subscribed(p, 'commit');
        let timer: ReturnType<typeof setTimeout> | null = null;
        let pending: string | null = null;
        if (wantInput) {
          w.onInput = (v) => {
            pending = v;
            if (timer) clearTimeout(timer);
            timer = setTimeout(() => {
              timer = null;
              if (pending != null) this.sink.event(id, 'input', pending);
              pending = null;
            }, 150);
          };
        }
        if (wantCommit) {
          w.onCommit = (v) => {
            if (timer) {
              clearTimeout(timer);
              timer = null;
              pending = null;
            }
            this.sink.event(id, 'commit', v);
          };
        }
      }
    } else if (w instanceof Stack) {
      if ('padding' in p) w.padding = num(p.padding) ?? 0;
      if ('spacing' in p) w.spacing = num(p.spacing) ?? 8;
      if ('align' in p) w.align = (str(p.align) as StackAlign) ?? 'start';
    } else if (w instanceof Dock) {
      if ('padding' in p) w.padding = num(p.padding) ?? 0;
      if ('gap' in p) w.gap = num(p.gap) ?? 0;
    } else if (w instanceof ScrollView) {
      if ('insetBottom' in p) w.contentInsetBottom = num(p.insetBottom) ?? 0;
    } else if (w instanceof TableView) {
      if ('columns' in p && Array.isArray(p.columns)) {
        w.columns = p.columns.map((c): TableColumn => {
          const o = (c ?? {}) as Record<string, unknown>;
          return {
            key: str(o.key) ?? '',
            title: str(o.title) ?? '',
            weight: num(o.weight) ?? undefined,
            width: num(o.width) ?? undefined,
            kind: str(o.kind) ?? undefined,
          };
        });
      }
      if ('rowCount' in p) w.rowCount = num(p.rowCount) ?? 0;
      if ('rowHeight' in p) w.rowHeight = num(p.rowHeight) ?? 0; // unparsable: defer to the row.height token
      if ('sortKey' in p) w.sortKey = str(p.sortKey);
      if ('sortDir' in p) w.sortDir = str(p.sortDir) === 'desc' ? 'desc' : 'asc';
      if ('selected' in p) w.selected = num(p.selected) ?? -1;
      if ('rowKey' in p) w.keyCol = num(p.rowKey) ?? -1;
      if ('selectedKey' in p) w.selectedKey = str(p.selectedKey);
      if (this.subscribed(p, 'visible-range')) {
        w.onVisibleRange = (start, end) => this.sink.event(id, 'visible-range', { start, end });
      }
      if (this.subscribed(p, 'sort')) w.onSort = (key, asc) => this.sink.event(id, 'sort', { key, asc });
      if (this.subscribed(p, 'row-select')) {
        w.onRowSelect = (row, key) =>
          this.sink.event(id, 'row-select', key != null ? { row, key } : row);
      }
      if (this.subscribed(p, 'row-activate')) {
        w.onRowActivate = (row, key) =>
          this.sink.event(id, 'row-activate', key != null ? { row, key } : row);
      }
      if (this.subscribed(p, 'toggle')) {
        w.onToggle = (row, key) => this.sink.event(id, 'toggle', { row, key });
      }
      if (this.subscribed(p, 'cell-activate')) {
        w.onCellActivate = (row, key, col, value) =>
          this.sink.event(id, 'cell-activate', { row, key, col, value });
      }
      if (this.subscribed(p, 'col-resize')) {
        w.onColResize = (key, width) => this.sink.event(id, 'col-resize', { key, width });
      }
    } else if (w instanceof Select) {
      if ('options' in p && Array.isArray(p.options)) w.options = p.options.map((o) => String(o));
      if ('selected' in p) w.selected = num(p.selected) ?? -1;
      if (this.subscribed(p, 'select')) w.onSelect = (i) => this.sink.event(id, 'select', i);
    } else if (w instanceof Progress) {
      if ('value' in p) w.value = num(p.value) ?? 0;
    } else if (w instanceof Slider) {
      if ('min' in p) w.min = num(p.min) ?? 0;
      if ('max' in p) w.max = num(p.max) ?? 1;
      if ('step' in p) w.step = num(p.step) ?? 0;
      // Local echo: mid-drag the client's value is authoritative.
      if ('value' in p && !w.dragging) w.value = num(p.value) ?? 0;
      if (this.subscribed(p, 'input')) {
        let timer: ReturnType<typeof setTimeout> | null = null;
        let pending: number | null = null;
        w.onInput = (v) => {
          pending = v;
          if (timer) return;
          timer = setTimeout(() => {
            timer = null;
            if (pending != null) this.sink.event(id, 'input', pending);
            pending = null;
          }, 100);
        };
      }
      if (this.subscribed(p, 'commit')) w.onCommit = (v) => this.sink.event(id, 'commit', v);
    } else if (w instanceof RadioGroup) {
      if ('options' in p && Array.isArray(p.options)) w.options = p.options.map((o) => String(o));
      if ('selected' in p) w.selected = num(p.selected) ?? -1;
      if (this.subscribed(p, 'select')) w.onSelect = (i) => this.sink.event(id, 'select', i);
    } else if (w instanceof Tabs) {
      if ('options' in p && Array.isArray(p.options)) w.options = p.options.map((o) => String(o));
      if ('selected' in p) w.selected = num(p.selected) ?? 0;
      if (this.subscribed(p, 'select')) w.onSelect = (i) => this.sink.event(id, 'select', i);
    } else if (w instanceof Dialog) {
      if ('title' in p) w.title = str(p.title) ?? '';
      if ('cardWidth' in p) w.cardW = num(p.cardWidth) ?? 420;
      if ('cardHeight' in p) w.cardH = num(p.cardHeight) ?? 200;
      if (this.subscribed(p, 'dismiss')) w.onDismiss = () => this.sink.event(id, 'dismiss');
    } else if (w instanceof SplitView) {
      if ('axis' in p) w.axis = str(p.axis) === 'v' ? 'v' : 'h';
      if ('pos' in p) w.pos = num(p.pos) ?? 300;
      if ('minA' in p) w.minA = num(p.minA) ?? 80;
      if ('minB' in p) w.minB = num(p.minB) ?? 80;
      if (this.subscribed(p, 'split-resize')) w.onResize = (pos) => this.sink.event(id, 'split-resize', pos);
    } else if (w instanceof ImageView) {
      if ('src' in p) w.src = str(p.src) ?? '';
      if ('fit' in p) {
        const f = str(p.fit);
        w.fit = f === 'cover' || f === 'fill' ? f : 'contain';
      }
      if ('radius' in p) w.radius = radius(p.radius) ?? 0;
      if ('alt' in p) w.alt = str(p.alt) ?? '';
    } else if (w instanceof ShaderPane) {
      if ('frag' in p) w.frag = str(p.frag) ?? '';
      // Merge-only: a set op carries just the changed keys (SetUniform), so
      // tweening one float never re-ships a pane's whole uniform map.
      if ('uniforms' in p) w.uniforms = { ...w.uniforms, ...floatMap(p.uniforms) };
      if ('animate' in p) w.animate = bool(p.animate) ?? false;
    } else if (w instanceof Glass) {
      // The store owns the id map; the glass resolves pick targets through
      // it (deepest widget → nearest server-known ancestor).
      w.idOf = (ww) => {
        for (const [wid, cand] of this.byId) if (cand === ww) return wid;
        return 0;
      };
      if (this.subscribed(p, 'pick')) {
        w.onPick = (x, y, target) => this.sink.event(id, 'pick', { x, y, target });
      }
      if (this.subscribed(p, 'drag')) {
        w.onDragTo = (x, y) => this.sink.event(id, 'drag', { x, y });
      }
      if (this.subscribed(p, 'drop')) {
        w.onDropAt = (x, y) => this.sink.event(id, 'drop', { x, y });
      }
      if (this.subscribed(p, 'wheel')) {
        w.onWheelAt = (x, y, dx, dy) => this.sink.event(id, 'wheel', { x, y, dx, dy });
      }
      if (this.subscribed(p, 'rpick')) {
        w.onRPick = (x, y) => this.sink.event(id, 'rpick', { x, y });
      }
      if (this.subscribed(p, 'rdrag')) {
        w.onRDragTo = (x, y) => this.sink.event(id, 'rdrag', { x, y });
      }
      if (this.subscribed(p, 'rdrop')) {
        w.onRDropAt = (x, y) => this.sink.event(id, 'rdrop', { x, y });
      }
    } else if (w instanceof Grid) {
      if ('columns' in p && Array.isArray(p.columns)) {
        w.columns = p.columns.map((c): GridTrack => {
          const o = (c ?? {}) as Record<string, unknown>;
          const kind = str(o.kind);
          return {
            kind: kind === 'fixed' || kind === 'fill' ? kind : 'content',
            px: num(o.px) ?? undefined,
            weight: num(o.weight) ?? undefined,
            align: (str(o.align) as GridTrack['align']) ?? undefined,
          };
        });
      }
      if ('colGap' in p) w.colGap = num(p.colGap) ?? 12;
      if ('rowGap' in p) w.rowGap = num(p.rowGap) ?? 10;
    }
  }

  remove(id: number): void {
    const w = this.byId.get(id);
    if (!w) return;
    const prune = (n: Widget) => {
      for (const [nid, nw] of this.byId) if (nw === n) this.byId.delete(nid);
      for (const c of n.children) prune(c);
    };
    prune(w);
    if (w.parent) {
      const i = w.parent.children.indexOf(w);
      if (i >= 0) w.parent.children.splice(i, 1);
      w.parent = null;
    }
  }

  private subscribed(p: Props, ev: string): boolean {
    return Array.isArray(p.on) && p.on.includes(ev);
  }

  private construct(json: NodeJson): Widget {
    switch (json.type) {
      case 'panel':
        return new Panel();
      case 'label':
        return new Label('');
      case 'button':
        return new Button('');
      case 'checkbox':
        return new Checkbox('');
      case 'textfield':
        return new TextField();
      case 'textarea':
        return new TextArea();
      case 'vstack':
        return new VStack();
      case 'hstack':
        return new HStack();
      case 'dock':
        return new Dock();
      case 'scroll':
        return new ScrollView();
      case 'table':
        return new TableView();
      case 'tree': {
        // a tree is the table widget in tree mode: same columns, same rows
        // protocol, plus per-row hierarchy meta and toggle round-trips.
        const t = new TableView();
        t.tree = true;
        return t;
      }
      case 'select':
        return new Select();
      case 'progress':
        return new Progress();
      case 'slider':
        return new Slider();
      case 'radio':
        return new RadioGroup();
      case 'tabs':
        return new Tabs();
      case 'dialog':
        return new Dialog();
      case 'split':
        return new SplitView();
      case 'grid':
        return new Grid();
      case 'shader':
        return new ShaderPane();
      case 'image':
        return new ImageView();
      case 'glass':
        return new Glass();
      default:
        console.warn(`caution: unknown node type "${json.type}", using panel`);
        return new Panel();
    }
  }
}
