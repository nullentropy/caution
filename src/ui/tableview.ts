import { withAlpha } from '../gfx/color';
import { font } from '../gfx/font';
import { rect, Rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { Scroller } from './containers';
import { metrics } from './metrics';
import type { SemanticChild, SemanticSpec } from './semantics';
import { theme } from './theme';
import { contains, Widget } from './widget';
import { paintFocusRing } from './widgets';

export interface TableColumn {
  key: string;
  title: string;
  weight?: number;
  width?: number;
  // in-cell widget column: '' text (default), 'button', 'checkbox', 'progress'
  kind?: string;
}

// per-row hierarchy meta shipped alongside tree rows: stable key, depth, has-kids, expanded
export interface RowMeta {
  key: string;
  d: number;
  k?: boolean;
  x?: boolean;
}

const HEADER_FONT = font(12, { weight: 600 });
const CELL_FONT = font(13);
/**
 * The table.headerHeight / table.cellPadX tokens, read live so a metrics
 * patch re-lays the header and cells out.
 */
const headerH = (): number => metrics.tableHeaderHeight;
const cellPad = (): number => metrics.tableCellPadX;
// tree mode: px of indent per depth level, and the disclosure glyph's slot
const TREE_INDENT = 14;
const TREE_GLYPH_W = 18;
// rows requested beyond the visible edge, so scrolling has runway
const OVERSCAN = 12;
// trailing throttle for visible-range reports
const REPORT_MS = 100;

/**
 * Virtualized table. Rows are data and the client holds a sparse
 * cache of row cells and paints only the visible window. Scrolling reports a
 * throttled `visible-range` upstream and `rows` ops fill the cache. Sort
 * order and the full dataset live on the server. Selection, hover, and
 * scrolling are client-local (with silent server sync for selection/sort).
 */
export class TableView extends Scroller {
  columns: TableColumn[] = [];
  rowCount = 0;
  /**
   * The per-node `rowHeight` prop; 0 (the default) defers to the row.height
   * metric token.
   */
  rowHeight = 0;
  sortKey: string | null = null;
  sortDir: 'asc' | 'desc' = 'asc';
  selected = -1;
  /**
   * Column index whose cells are stable row keys; -1 = keyless. With keys,
   * selection is the *key* (selectedKey), so it survives server-side sorts
   * and data changes.
   */
  keyCol = -1;
  selectedKey: string | null = null;
  /**
   * Tree mode (node type "tree"): rows carry hierarchy meta, column 0
   * indents by depth and draws a disclosure triangle, selection identity is
   * the meta key, and toggles round-trip to the server (which owns
   * expansion and reflattens).
   */
  tree = false;

  onVisibleRange: ((start: number, end: number) => void) | null = null;
  onSort: ((key: string, asc: boolean) => void) | null = null;
  onRowSelect: ((row: number, key: string | null) => void) | null = null;
  onRowActivate: ((row: number, key: string | null) => void) | null = null;
  onToggle: ((row: number, key: string) => void) | null = null;
  onCellActivate: ((row: number, key: string, col: string, value: string) => void) | null = null;
  onColResize: ((key: string, width: number) => void) | null = null;

  // row the last press hit. double-click must hit the same row twice
  private lastDownRow = -1;

  /**
   * Client-local column width overrides (keyed by column key) from header
   * divider drags. UX state like scroll position: it survives prop
   * re-application but not a remount.
   */
  private colOverrides = new Map<string, number>();
  private colDrag: { index: number; startX: number; startW: number } | null = null;
  private hoverDivider = -1;

  private cache = new Map<number, string[]>();
  private meta = new Map<number, RowMeta>();
  private hoverRow = -1;
  // row index of the last selection/keyboard move
  private cursorRow = -1;
  private lastSent: [number, number] | null = null;
  private pendingRange: [number, number] | null = null;
  private rangeTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    super();
    this.clips = true;
  }

  /**
   * The effective row height: the rowHeight prop when the app set one, otherwise
   * the row.height token.
   */
  private rowH(): number {
    return this.rowHeight > 0 ? this.rowHeight : metrics.rowHeight;
  }

  override get interactive(): boolean {
    return true;
  }

  override get focusable(): boolean {
    return true;
  }

  // Arrows/Page/Home/End move the cursor row, select it, and reveal it
  override onKey(e: KeyboardEvent): boolean {
    const n = this.rowCount;
    if (n <= 0) return false;
    const page = Math.max(1, Math.floor(this.viewport().h / this.rowH()) - 1);
    let to: number;
    switch (e.key) {
      case 'ArrowDown':
        to = Math.min(n - 1, this.cursorRow + 1);
        break;
      case 'ArrowUp':
        to = Math.max(0, this.cursorRow - 1);
        break;
      case 'PageDown':
        to = Math.min(n - 1, this.cursorRow + page);
        break;
      case 'PageUp':
        to = Math.max(0, this.cursorRow - page);
        break;
      case 'Home':
        to = 0;
        break;
      case 'End':
        to = n - 1;
        break;
      case 'Enter':
        // enter selects AND activates
        if (this.cursorRow < 0) return false;
        this.revealRow(this.cursorRow);
        this.selectRow(this.cursorRow);
        this.activateRow(this.cursorRow);
        return true;
      case 'ArrowRight': {
        // tree: expand the cursor row, or step into it once open
        if (!this.tree || this.cursorRow < 0) return false;
        const m = this.meta.get(this.cursorRow);
        if (m?.k && !m.x) {
          this.onToggle?.(this.cursorRow, m.key);
          return true;
        }
        to = Math.min(n - 1, this.cursorRow + 1);
        break;
      }
      case 'ArrowLeft': {
        // tree: collapse the cursor row, or jump to its parent
        if (!this.tree || this.cursorRow < 0) return false;
        const m = this.meta.get(this.cursorRow);
        if (m?.k && m.x) {
          this.onToggle?.(this.cursorRow, m.key);
          return true;
        }
        const d = m?.d ?? 0;
        let p = this.cursorRow - 1;
        while (p >= 0 && (this.meta.get(p)?.d ?? 0) >= d) p--;
        if (p < 0) return true;
        to = p;
        break;
      }
      default:
        return false;
    }
    this.cursorRow = Math.max(0, to);
    this.revealRow(this.cursorRow);
    this.selectRow(this.cursorRow);
    return true;
  }

  private revealRow(i: number): void {
    const v = this.viewport();
    const top = i * this.rowH();
    const bot = top + this.rowH();
    if (top < this.scrollY) this.scrollY = top;
    else if (bot > this.scrollY + v.h) this.scrollY = bot - v.h;
    this.clampScroll();
    this.invalidate();
  }

  protected override viewport(): Rect {
    const b = this.bounds;
    return rect(b.x, b.y + headerH(), b.w, Math.max(0, b.h - headerH()));
  }

  override layoutChildren(): void {
    this.contentH = this.rowCount * this.rowH();
    this.contentW = this.colWidths().reduce((a, b) => a + b, 0);
    this.clampScroll();
  }

  // fill (or, with reset, replace) the row cache from a server `rows` op
  applyRows(start: number, rows: string[][], meta: RowMeta[] | undefined, reset: boolean): void {
    if (reset) {
      this.cache.clear();
      this.meta.clear();
    }
    rows.forEach((r, i) => this.cache.set(start + i, r));
    meta?.forEach((m, i) => this.meta.set(start + i, m));
    this.evictFarRows();
    this.invalidate();
  }

  /**
   * Bound the cache: long scrolls through a huge table would otherwise pin
   * every row ever seen. Rows far from the viewport re-fetch on return.
   */
  private evictFarRows(): void {
    const MAX = 2000;
    const KEEP = 600; // rows kept on each side of the viewport
    if (this.cache.size <= MAX) return;
    const first = Math.floor(this.scrollY / this.rowH());
    const last = Math.ceil((this.scrollY + this.viewport().h) / this.rowH());
    for (const k of this.cache.keys()) {
      if (k < first - KEEP || k > last + KEEP) {
        this.cache.delete(k);
        this.meta.delete(k);
      }
    }
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    const v = this.viewport();
    const widths = this.colWidths();
    if (this.ui?.showFocusRing(this)) paintFocusRing(dl, b, metrics.radiusControl);

    // header
    const hr = metrics.radiusControl;
    dl.rect(rect(b.x, b.y, b.w, headerH()), { color: theme.titlebar, radius: { tl: hr, tr: hr, br: 0, bl: 0 } });
    dl.pushClip(rect(b.x, b.y, b.w, headerH()));
    let hx = b.x - this.scrollX;
    for (let i = 0; i < this.columns.length; i++) {
      const c = this.columns[i]!;
      const w = widths[i]!;
      let title = c.title;
      if (c.key === this.sortKey) title += this.sortDir === 'asc' ? '  ▲' : '  ▼';
      const m = this.ui!.measure(HEADER_FONT, title);
      dl.pushClip(rect(hx, b.y, w, headerH()));
      dl.text(title, hx + cellPad(), b.y + (headerH() - (m.ascent + m.descent)) / 2, HEADER_FONT, theme.inkDim);
      dl.popClip();
      hx += w;
    }
    dl.popClip();
    const hair = metrics.borderWidth;
    dl.rect(rect(b.x, b.y + headerH() - hair, b.w, hair), { color: theme.edge });

    if (this.rowCount <= 0 || v.h <= 0) return;

    const first = Math.max(0, Math.floor(this.scrollY / this.rowH()));
    const last = Math.min(this.rowCount - 1, Math.ceil((this.scrollY + v.h) / this.rowH()) - 1);
    const fm = this.ui!.measure(CELL_FONT, 'M');
    const cellTextH = fm.ascent + fm.descent;

    dl.pushClip(v);
    for (let i = first; i <= last; i++) {
      const y = v.y + i * this.rowH() - this.scrollY;
      const rowRect = rect(b.x, y, b.w, this.rowH());
      const data = this.cache.get(i);
      if (this.isSelected(i, data)) dl.rect(rowRect, { color: withAlpha(theme.accent, 0.22) });
      else if (i === this.hoverRow) dl.rect(rowRect, { color: withAlpha(theme.ink, 0.05) });
      else if (i % 2 === 1) dl.rect(rowRect, { color: withAlpha(theme.ink, 0.02) });
      const textY = y + (this.rowH() - cellTextH) / 2;
      let x = b.x - this.scrollX;
      for (let ci = 0; ci < this.columns.length; ci++) {
        const w = widths[ci]!;
        if (data) {
          // tree mode: column 0 indents by depth and carries the disclosure
          let tx = x + cellPad();
          if (this.tree && ci === 0) {
            const m = this.meta.get(i);
            tx += (m?.d ?? 0) * TREE_INDENT;
            if (m?.k) paintDisclosure(dl, Math.floor(tx), Math.floor(y + this.rowH() / 2), m.x ?? false);
            tx += TREE_GLYPH_W;
          }
          dl.pushClip(rect(x, y, w - 4, this.rowH()));
          const cell = data[ci] ?? '';
          const kind = this.columns[ci]?.kind;
          if (kind === 'progress') {
            const bw = Math.max(24, w - 2 * cellPad() - (tx - x - cellPad()));
            const bar = rect(tx, Math.floor(y + this.rowH() / 2 - 3), bw, 6);
            dl.rect(bar, { color: theme.panelInset, radius: 3, borderWidth: metrics.borderWidth, borderColor: theme.edgeSoft });
            const v = Math.max(0, Math.min(1, parseFloat(cell) || 0));
            if (v > 0) dl.rect(rect(bar.x, bar.y, Math.max(6, bar.w * v), 6), { color: theme.accent, radius: 3 });
          } else if (kind === 'checkbox') {
            // in-cell marks are miniatures: they follow radius.control but cap
            // at their own scale, so a rounded theme can't turn a 16px box
            // into a pill.
            const cb = rect(tx, Math.floor(y + this.rowH() / 2 - 8), 16, 16);
            dl.rect(cb, {
              color: theme.control,
              radius: Math.min(metrics.radiusControl, 4),
              borderWidth: metrics.borderWidth,
              borderColor: theme.controlEdge,
            });
            // the mark is geometry (an inset fill), so both terminals agree to the pixel
            if (cellTruthy(cell)) {
              dl.rect(rect(cb.x + 4, cb.y + 4, 8, 8), { color: theme.accent, radius: Math.min(metrics.radiusControl, 2) });
            }
          } else if (kind === 'button') {
            const m = this.ui!.measure(CELL_FONT, cell);
            const bw = Math.min(w - 2 * cellPad(), Math.ceil(m.width) + 24);
            const bt = rect(tx, y + 5, Math.max(28, bw), this.rowH() - 10);
            dl.rect(bt, { color: theme.control, radius: metrics.radiusControl, borderWidth: metrics.borderWidth, borderColor: theme.controlEdge });
            dl.text(cell, bt.x + (bt.w - m.width) / 2, y + (this.rowH() - cellTextH) / 2, CELL_FONT, theme.ink);
          } else {
            dl.text(cell, tx, textY, CELL_FONT, theme.ink);
          }
          dl.popClip();
        } else {
          // skeleton placeholder while the window streams in
          dl.rect(rect(x + cellPad(), y + this.rowH() / 2 - 4, Math.max(24, w * 0.55), 8), {
            color: withAlpha(theme.ink, 0.06),
            radius: 4,
          });
        }
        x += w;
      }
    }
    dl.popClip();

    this.reportRange(
      Math.max(0, first - OVERSCAN),
      Math.min(this.rowCount - 1, last + OVERSCAN),
    );
  }

  // -- interaction ---------------------------------------------------------------

  override hitTest(x: number, y: number): Widget | null {
    return contains(this.bounds, x, y) ? this : null;
  }

  override cursor(): string | null {
    return this.hoverDivider >= 0 || this.colDrag ? 'col-resize' : null;
  }

  // divider index under x in the header (boundary right of column i), or -1
  private dividerAt(x: number, y: number): number {
    if (y >= this.bounds.y + headerH()) return -1;
    const widths = this.colWidths();
    let cx = this.bounds.x - this.scrollX;
    for (let i = 0; i < this.columns.length - 1; i++) {
      cx += widths[i]!;
      if (Math.abs(x - cx) <= 4) return i;
    }
    return -1;
  }

  override onPointerDown(x: number, y: number, detail: number): void {
    if (this.thumbDown(x, y)) return;
    const b = this.bounds;
    if (y < b.y + headerH()) {
      const div = this.dividerAt(x, y);
      if (div >= 0) {
        this.colDrag = { index: div, startX: x, startW: this.colWidths()[div]! };
        return;
      }
      const widths = this.colWidths();
      let cx = b.x - this.scrollX;
      for (let i = 0; i < this.columns.length; i++) {
        cx += widths[i]!;
        if (x < cx) {
          const c = this.columns[i]!;
          const asc = this.sortKey === c.key ? this.sortDir !== 'asc' : true;
          this.sortKey = c.key; // local echo; server syncs silently
          this.sortDir = asc ? 'asc' : 'desc';
          this.invalidate();
          this.onSort?.(c.key, asc);
          return;
        }
      }
      return;
    }
    const row = this.rowAt(y);
    if (row < 0) return;
    // a click on the disclosure toggles without selecting
    if (this.tree) {
      const m = this.meta.get(row);
      if (m?.k) {
        const gx = this.bounds.x - this.scrollX + cellPad() + m.d * TREE_INDENT;
        if (x >= gx - 4 && x < gx + TREE_GLYPH_W) {
          this.lastDownRow = -1; // a toggle is not half of a double-click
          this.onToggle?.(row, m.key);
          return;
        }
      }
    }
    // in-cell widget columns act instead of selecting
    const ci = this.colIndexAt(x);
    const kind = ci >= 0 ? this.columns[ci]?.kind : undefined;
    if ((kind === 'button' || kind === 'checkbox') && this.cache.has(row)) {
      this.lastDownRow = -1; // widget cells never count toward a double-click
      const colKey = this.columns[ci]!.key;
      const rowKey = this.rowKeyOf(row) ?? '';
      if (kind === 'button') {
        this.onCellActivate?.(row, rowKey, colKey, '');
      } else {
        // local echo: flip the cached cell now. the server keeps it by
        // updating its data, or vetoes with a refresh.
        const cells = this.cache.get(row)!.slice();
        const next = cellTruthy(cells[ci] ?? '') ? 'false' : 'true';
        cells[ci] = next;
        this.cache.set(row, cells);
        this.invalidate();
        this.onCellActivate?.(row, rowKey, colKey, next);
      }
      return;
    }
    // the UI router counts same-widget multi-clicks (timing + radius). the
    // table adds the same-row constraint. precisely 2, a triple starts over.
    const dbl = detail === 2 && row === this.lastDownRow;
    this.lastDownRow = row;
    this.selectRow(row);
    if (dbl) this.activateRow(row);
  }

  // emits row-activate with the row's selection identity (double-click/Enter)
  private activateRow(row: number): void {
    if (this.tree || this.keyCol >= 0) {
      const key = this.rowKeyOf(row);
      if (key == null) return; // skeleton row - no identity yet
      this.onRowActivate?.(row, key);
      return;
    }
    this.onRowActivate?.(row, null);
  }

  // column index under x, or -1
  private colIndexAt(x: number): number {
    const widths = this.colWidths();
    let cx = this.bounds.x - this.scrollX;
    for (let i = 0; i < this.columns.length; i++) {
      cx += widths[i]!;
      if (x < cx) return i;
    }
    return -1;
  }

  private isSelected(i: number, data: string[] | undefined): boolean {
    if (this.tree) {
      return this.selectedKey != null && this.meta.get(i)?.key === this.selectedKey;
    }
    if (this.keyCol >= 0) {
      return this.selectedKey != null && data != null && data[this.keyCol] === this.selectedKey;
    }
    return i === this.selected;
  }

  private rowKeyOf(row: number): string | null {
    if (this.tree) return this.meta.get(row)?.key ?? null;
    if (this.keyCol >= 0) return this.cache.get(row)?.[this.keyCol] ?? null;
    return null;
  }

  private selectRow(row: number): void {
    this.cursorRow = row; // clicks and keyboard share the cursor
    if (this.tree || this.keyCol >= 0) {
      const key = this.rowKeyOf(row);
      if (key == null) return; // skeleton row - no identity to select yet
      this.selectedKey = key; // local echo
      this.invalidate();
      this.onRowSelect?.(row, key);
    } else {
      this.selected = row; // local echo
      this.invalidate();
      this.onRowSelect?.(row, null);
    }
  }

  override onPointerDrag(x: number, y: number): void {
    if (this.colDrag) {
      const d = this.colDrag;
      const col = this.columns[d.index]!;
      this.colOverrides.set(col.key, Math.max(40, d.startW + (x - d.startX)));
      this.invalidate();
      return;
    }
    this.thumbDrag(x, y);
  }

  override onPointerUp(): void {
    if (this.colDrag) {
      const col = this.columns[this.colDrag.index]!;
      const w = this.colOverrides.get(col.key);
      this.colDrag = null;
      if (w != null) this.onColResize?.(col.key, Math.round(w));
      return;
    }
    this.thumbUp();
  }

  override onPointerHover(x: number, y: number): void {
    const div = this.dividerAt(x, y);
    if (div !== this.hoverDivider) {
      this.hoverDivider = div;
      this.invalidate();
    }
    const row = y >= this.bounds.y + headerH() ? this.rowAt(y) : -1;
    if (row !== this.hoverRow) {
      this.hoverRow = row;
      this.invalidate();
    }
  }

  override onHoverChange(hovered: boolean): void {
    if (!hovered && this.hoverRow !== -1) {
      this.hoverRow = -1;
      this.invalidate();
    }
  }

  override semantics(): SemanticSpec {
    const v = this.viewport();
    const children: SemanticChild[] = [];
    if (this.rowCount > 0 && v.h > 0) {
      const first = Math.max(0, Math.floor(this.scrollY / this.rowH()));
      const last = Math.min(this.rowCount - 1, Math.ceil((this.scrollY + v.h) / this.rowH()) - 1);
      for (let i = first; i <= last; i++) {
        const data = this.cache.get(i);
        if (!data) continue;
        const row = i;
        children.push({
          role: 'row',
          label: data.join(' · '),
          posInSet: row + 1,
          setSize: this.rowCount,
          selected: this.isSelected(row, data),
          bounds: rect(this.bounds.x, v.y + row * this.rowH() - this.scrollY, this.bounds.w, this.rowH()),
          onActivate: () => this.selectRow(row),
        });
      }
    }
    const cols = this.columns.map((c) => c.title).join(', ');
    return { role: 'grid', label: `table (${cols}), ${this.rowCount} rows`, rowCount: this.rowCount, children };
  }

  // -- internals -------------------------------------------------------------------

  private rowAt(y: number): number {
    const v = this.viewport();
    const i = Math.floor((y - v.y + this.scrollY) / this.rowH());
    return i >= 0 && i < this.rowCount ? i : -1;
  }

  private colWidths(): number[] {
    const px = (c: TableColumn): number | undefined => this.colOverrides.get(c.key) ?? c.width;
    let fixed = 0;
    let weightSum = 0;
    for (const c of this.columns) {
      const w = px(c);
      if (w) fixed += w;
      else weightSum += c.weight ?? 1;
    }
    const free = Math.max(0, this.bounds.w - fixed);
    // weighted columns never collapse below a readable floor: when fixed
    // columns (resizes included) eat the width, the table scrolls
    // horizontally instead
    const MIN_W = 80;
    return this.columns.map((c) => px(c) ?? Math.max(MIN_W, free * ((c.weight ?? 1) / (weightSum || 1))));
  }

  // leading + trailing throttle so fast scrolls report at most every REPORT_MS
  private reportRange(start: number, end: number): void {
    if (!this.onVisibleRange || end < start) return;
    this.pendingRange = [start, end];
    if (this.rangeTimer != null) return;
    this.flushRange();
    this.rangeTimer = setTimeout(() => {
      this.rangeTimer = null;
      this.flushRange();
    }, REPORT_MS);
  }

  private flushRange(): void {
    const p = this.pendingRange;
    if (!p) return;
    if (this.lastSent && p[0] === this.lastSent[0] && p[1] === this.lastSent[1]) {
      this.pendingRange = null;
      return;
    }
    this.lastSent = p;
    this.pendingRange = null;
    this.onVisibleRange!(p[0], p[1]);
  }
}

// checkbox-cell truthiness: the wire ships plain strings
function cellTruthy(v: string): boolean {
  return v === 'true' || v === '1' || v === 'yes';
}

/**
 * The disclosure triangle, drawn as geometry (like the select chevron)
 */
function paintDisclosure(dl: DisplayList, x: number, cy: number, open: boolean): void {
  for (let i = 0; i < 5; i++) {
    const s = 9 - 2 * i;
    if (open) dl.rect(rect(x + (9 - s) / 2, cy - 3 + i, s, 1), { color: theme.inkDim });
    else dl.rect(rect(x + i, cy - s / 2, 1, s), { color: theme.inkDim });
  }
}
