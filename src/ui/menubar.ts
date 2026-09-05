import { BLACK, withAlpha } from '../gfx/color';
import { font } from '../gfx/font';
import { rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import type { MenuItemSpec } from '../proto/types';
import { metrics } from './metrics';
import { theme } from './theme';
import type { Ui } from './ui';
import { Widget } from './widget';

// one top-level menu
export interface MenuSpec {
  title: string;
  items: MenuItemSpec[];
}

/**
 * The in-window menu bar. Modeled after NSMenu.
 *
 * Mirrored in the native terminal (go/terminal/ui/menubar.go) with geometry
 * and painting identical so goldens can gate the bar.
 */

// the bar's reserved height in logical px
export const menubarH = (): number => metrics.rowHeight;

const BAR_FONT = font(13);
const TITLE_PAD = 10;
const SEP_H = 9;
const PAD = 6;
const SECTION_FONT = font(11);

export class MenuBar extends Widget {
  private hoverIdx = -1;
  private openIdx = -1;
  private popover: MenuPopover | null = null;

  constructor(
    readonly menus: MenuSpec[],
    readonly onPick: (id: number) => void,
  ) {
    super();
  }

  // menu key equivalents, canonicalized like registered combos
  combos(): Map<string, number> {
    const out = new Map<string, number>();
    const walk = (items: MenuItemSpec[]) => {
      for (const it of items) {
        if (it.items) walk(it.items);
        else if (it.id != null && it.key) out.set(canonicalMenuCombo(it.key), it.id);
      }
    };
    for (const m of this.menus) walk(m.items);
    return out;
  }

  // fire a pick by id (the probe hook, and the keyboard path)
  perform(id: number): boolean {
    let found = false;
    const walk = (items: MenuItemSpec[]) => {
      for (const it of items) {
        if (it.items) walk(it.items);
        else if (it.id === id) found = true;
      }
    };
    for (const m of this.menus) walk(m.items);
    if (found) {
      this.sound('select');
      this.onPick(id);
    }
    return found;
  }

  override get interactive(): boolean {
    return true;
  }

  override cursor(): string {
    return 'default';
  }

  // the bar reconciles its open state at paint: if the overlay is no longer
  // our popover (click-away, Escape, a pick) then the highlight clears
  override paintSelf(dl: DisplayList): void {
    if (this.popover && this.ui?.overlay !== this.popover) {
      this.popover = null;
      this.openIdx = -1;
    }
    const b = this.bounds;
    dl.rect(b, { color: theme.titlebar });
    const hair = metrics.borderWidth;
    dl.rect(rect(b.x, b.y + b.h - hair, b.w, hair), { color: theme.edgeSoft });
    let x = b.x + TITLE_PAD;
    this.menus.forEach((m, i) => {
      const w = this.titleW(m.title);
      const active = i === this.openIdx;
      if (active || i === this.hoverIdx) {
        dl.rect(rect(x, b.y + 3, w, b.h - 7), {
          color: active ? withAlpha(theme.accent, 0.25) : theme.control,
          radius: metrics.radiusControl,
        });
      }
      const run = this.ui!.measure(BAR_FONT, m.title);
      dl.text(
        m.title,
        x + TITLE_PAD,
        b.y + (b.h - 1 - (run.ascent + run.descent)) / 2,
        BAR_FONT,
        active || i === this.hoverIdx ? theme.ink : theme.inkDim,
      );
      x += w;
    });
  }

  override onPointerDown(x: number, _y: number): void {
    const i = this.titleAt(x);
    if (i < 0) return;
    if (i === this.openIdx) {
      this.ui?.closeOverlay(); // toggling the open title closes it
      return;
    }
    this.open(i);
  }

  override onPointerHover(x: number, _y: number): void {
    const i = this.titleAt(x);
    if (i !== this.hoverIdx) {
      this.hoverIdx = i;
      this.invalidate();
    }
    // the menubar convention: while a menu is open, pointing at another
    // title switches to it without having to press
    if (this.openIdx >= 0 && i >= 0 && i !== this.openIdx) this.open(i);
  }

  override onHoverChange(hovered: boolean): void {
    if (!hovered && this.hoverIdx !== -1) {
      this.hoverIdx = -1;
      this.invalidate();
    }
  }

  private open(i: number): void {
    if (!this.ui) return;
    this.openIdx = i;
    this.popover = MenuPopover.open(this.ui, this.menus[i]!.items, this.onPick, this.titleX(i), this.bounds.y + this.bounds.h);
    this.sound('open');
    this.invalidate();
  }

  private titleW(title: string): number {
    return Math.ceil(this.ui!.measure(BAR_FONT, title).width) + TITLE_PAD * 2;
  }

  private titleX(idx: number): number {
    let x = this.bounds.x + TITLE_PAD;
    for (let i = 0; i < idx; i++) x += this.titleW(this.menus[i]!.title);
    return x;
  }

  private titleAt(absX: number): number {
    let x = this.bounds.x + TITLE_PAD;
    for (let i = 0; i < this.menus.length; i++) {
      const w = this.titleW(this.menus[i]!.title);
      if (absX >= x && absX < x + w) return i;
      x += w;
    }
    return -1;
  }
}

// one dropdown row
interface Row {
  item: MenuItemSpec;
  indent: number;
  section: boolean;
}

// the dropdown under an open item, pattern aligned with ContextMenu
export class MenuPopover extends Widget {
  private hoverRow = -1;

  constructor(
    private readonly rows: Row[],
    private readonly onPick: (id: number) => void,
  ) {
    super();
  }

  static open(ui: Ui, items: MenuItemSpec[], onPick: (id: number) => void, x: number, y: number): MenuPopover {
    const rows: Row[] = [];
    const flatten = (list: MenuItemSpec[], indent: number) => {
      for (const it of list) {
        if (it.items) {
          rows.push({ item: it, indent, section: true });
          flatten(it.items, indent + 1);
        } else {
          rows.push({ item: it, indent, section: false });
        }
      }
    };
    flatten(items, 0);
    const menu = new MenuPopover(rows, onPick);
    menu.ui = ui;
    let w = 160;
    for (const r of rows) {
      if (!r.item.title) continue;
      const label = Math.ceil(ui.measure(BAR_FONT, r.item.title).width) + r.indent * 12;
      const key = r.item.key ? Math.ceil(ui.measure(BAR_FONT, comboLabel(r.item.key)).width) + 24 : 0;
      w = Math.max(w, label + key + 44);
    }
    w = Math.min(w, 360);
    let h = PAD * 2;
    for (const r of rows) h += r.item.sep ? SEP_H : metrics.rowHeight;
    const vw = ui.canvas.clientWidth;
    const vh = ui.canvas.clientHeight;
    const px = Math.max(8, Math.min(x, vw - w - 8));
    const py = Math.min(y, vh - h - 8);
    menu.bounds = rect(px, Math.max(8, py), w, h);
    ui.openOverlay(menu);
    return menu;
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
    this.rows.forEach((r, i) => {
      const it = r.item;
      if (it.sep) {
        dl.rect(rect(b.x + 8, y + SEP_H / 2, b.w - 16, metrics.borderWidth), { color: theme.edgeSoft });
        y += SEP_H;
        return;
      }
      const x = b.x + 14 + r.indent * 12;
      if (r.section) {
        const m = this.ui!.measure(SECTION_FONT, it.title ?? '');
        dl.text(it.title ?? '', x, y + (metrics.rowHeight - (m.ascent + m.descent)) / 2, SECTION_FONT, theme.inkFaint);
        y += metrics.rowHeight;
        return;
      }
      if (i === this.hoverRow) {
        dl.rect(rect(b.x + 4, y, b.w - 8, metrics.rowHeight), { color: withAlpha(theme.accent, 0.25), radius: metrics.radiusControl });
      }
      const m = this.ui!.measure(BAR_FONT, it.title ?? '');
      dl.text(it.title ?? '', x, y + (metrics.rowHeight - (m.ascent + m.descent)) / 2, BAR_FONT, theme.ink);
      if (it.key) {
        const label = comboLabel(it.key);
        const km = this.ui!.measure(BAR_FONT, label);
        dl.text(label, b.x + b.w - 14 - km.width, y + (metrics.rowHeight - (km.ascent + km.descent)) / 2, BAR_FONT, theme.inkFaint);
      }
      y += metrics.rowHeight;
    });
  }

  override onPointerDown(_x: number, y: number): void {
    const i = this.rowAt(y);
    const r = i >= 0 ? this.rows[i] : undefined;
    if (r && !r.section && r.item.id != null) {
      this.ui?.dismissOverlay();
      this.sound('select');
      this.onPick(r.item.id);
    }
  }

  override onPointerHover(_x: number, y: number): void {
    const i = this.rowAt(y);
    const target = i >= 0 && !this.rows[i]!.section ? i : -1;
    if (target !== this.hoverRow) {
      this.hoverRow = target;
      this.invalidate();
    }
  }

  private rowAt(absY: number): number {
    let y = this.bounds.y + PAD;
    for (let i = 0; i < this.rows.length; i++) {
      const h = this.rows[i]!.item.sep ? SEP_H : metrics.rowHeight;
      if (absY >= y && absY < y + h) return this.rows[i]!.item.sep ? -1 : i;
      y += h;
    }
    return -1;
  }
}

/**
 * Canonicalize a menu key spec ("shift+cmd+k", bare "n" = ⌘N) into the
 * registered-combo form: cmd+ctrl+alt+shift+key. Mirrored natively.
 */
export function canonicalMenuCombo(spec: string): string {
  const parts = spec.toLowerCase().split('+');
  const key = parts[parts.length - 1] ?? '';
  const mods = new Set(parts.slice(0, -1));
  const has = (...names: string[]) => names.some((n) => mods.has(n));
  let out = '';
  if (mods.size === 0 || has('cmd', 'command', 'super')) out += 'cmd+';
  if (has('ctrl', 'control')) out += 'ctrl+';
  if (has('opt', 'option', 'alt')) out += 'alt+';
  if (has('shift')) out += 'shift+';
  return out + (key === ' ' ? 'space' : key);
}

// Display form of a menu key: "shift+cmd+k" -> "Shift+Cmd+K"
export function comboLabel(spec: string): string {
  const canonical = canonicalMenuCombo(spec);
  const parts = canonical.split('+');
  const key = parts[parts.length - 1] ?? '';
  let out = '';
  for (const [mod, label] of [['ctrl', 'Ctrl'], ['alt', 'Alt'], ['shift', 'Shift'], ['cmd', 'Cmd']] as const) {
    if (parts.includes(mod)) out += `${label}+`;
  }
  return out + key.charAt(0).toUpperCase() + key.slice(1);
}
