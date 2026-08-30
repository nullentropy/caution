import { rect } from '../gfx/geom';
import { layoutSubtree, Size, Widget } from './widget';

/**
 * Column track: fixed px, content (max natural width of its cells), or fill
 * (shares leftover space by weight). `align` places each cell's child within
 * the column ('start' default, 'stretch' gives it the full cell width).
 */
export interface GridTrack {
  kind: 'fixed' | 'content' | 'fill';
  px?: number;
  weight?: number;
  align?: 'start' | 'center' | 'end' | 'stretch';
}

interface Placed {
  child: Widget;
  row: number;
  col: number;
  span: number;
}

/**
 * NSGridView-style rows x columns for forms and inspectors. Children flow
 * row-major and `gridSpan` spans columns. Rows auto-size to their tallest cell
 * and children center vertically within the row. The classic form layout
 * (right-aligned caption column, control column) needs no manual offsets.
 */
export class Grid extends Widget {
  columns: GridTrack[] = [];
  colGap = 12;
  rowGap = 10;

  override intrinsicSize(): Size {
    this.adopt();
    if (!this.columns.length || !this.children.length) return { w: 0, h: 0 };
    const placed = this.place();
    const widths = this.contentWidths(placed);
    const rows = this.rowHeights(placed);
    return {
      w: widths.reduce((a, b) => a + b, 0) + this.colGap * (this.columns.length - 1),
      h: rows.reduce((a, b) => a + b, 0) + this.rowGap * Math.max(0, rows.length - 1),
    };
  }

  override layoutChildren(): void {
    this.adopt();
    const n = this.columns.length;
    if (!n || !this.children.length) return;
    const placed = this.place();
    const widths = this.contentWidths(placed);

    // fill columns share whatever the fixed/content columns leave behind
    let used = this.colGap * (n - 1);
    let weightSum = 0;
    this.columns.forEach((t, i) => {
      if (t.kind === 'fill') weightSum += t.weight ?? 1;
      else used += widths[i]!;
    });
    const free = Math.max(0, this.bounds.w - used);
    this.columns.forEach((t, i) => {
      if (t.kind === 'fill') widths[i] = free * ((t.weight ?? 1) / (weightSum || 1));
    });

    const xs: number[] = [0];
    for (let i = 0; i < n; i++) xs.push(xs[i]! + widths[i]! + this.colGap);
    const rows = this.rowHeights(placed);
    const ys: number[] = [0];
    for (let i = 0; i < rows.length; i++) ys.push(ys[i]! + rows[i]! + this.rowGap);

    for (const p of placed) {
      let cellW = -this.colGap;
      for (let i = 0; i < p.span; i++) cellW += (widths[p.col + i] ?? 0) + this.colGap;
      const cellX = this.bounds.x + xs[p.col]!;
      const rowY = this.bounds.y + ys[p.row]!;
      const rowH = rows[p.row]!;
      // spanning cells always stretch across their columns (merged-cell rule)
      const align = p.span > 1 ? 'stretch' : this.columns[p.col]!.align ?? 'start';
      const sz = this.sizeOf(p.child);
      const w = align === 'stretch' ? cellW : Math.min(sz.w, cellW);
      let x = cellX;
      if (align === 'center') x += (cellW - w) / 2;
      else if (align === 'end') x += cellW - w;
      p.child.bounds = rect(x, rowY + (rowH - sz.h) / 2, w, sz.h);
      layoutSubtree(p.child);
    }
  }

  // row-major flow with span-aware wrapping
  private place(): Placed[] {
    const n = this.columns.length;
    const out: Placed[] = [];
    let row = 0;
    let col = 0;
    for (const child of this.children) {
      const span = Math.max(1, Math.min(child.gridSpan, n));
      if (col + span > n) {
        row++;
        col = 0;
      }
      out.push({ child, row, col, span });
      col += span;
      if (col >= n) {
        row++;
        col = 0;
      }
    }
    return out;
  }

  private contentWidths(placed: Placed[]): number[] {
    return this.columns.map((t, i) => {
      if (t.kind === 'fixed') return t.px ?? 0;
      let w = 0;
      for (const p of placed) {
        if (p.col === i && p.span === 1) w = Math.max(w, this.sizeOf(p.child).w);
      }
      return w;
    });
  }

  private rowHeights(placed: Placed[]): number[] {
    const rows: number[] = [];
    for (const p of placed) rows[p.row] = Math.max(rows[p.row] ?? 0, this.sizeOf(p.child).h);
    return rows;
  }
}
