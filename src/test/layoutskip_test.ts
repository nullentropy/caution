// layout-skip parity, mirroring go/terminal/ui/layoutskip_test.go. The layout
// pass skips subtrees whose geometry cannot have moved (see layoutSubtree), and
// the failure mode is stale bounds behind pixels that still look right, so the
// scene runs twice, once with the skip and once laid out in full, and every
// widget's bounds are compared after each step.
import { rect, type Rect } from '../gfx/geom';
import type { TextRun } from '../gfx/text/shaper';
import { Dock, ScrollView, Stack } from '../ui/containers';
import { Dialog } from '../ui/dialog';
import { Grid } from '../ui/grid';
import { SplitView } from '../ui/splitview';
import { TableView } from '../ui/tableview';
import type { Ui } from '../ui/ui';
import { layoutSubtree, type Widget } from '../ui/widget';
import { Button, Checkbox, Label, Panel } from '../ui/widgets';
import { test } from './harness';

/** fixed-advance fake measure, like the Go layout tests' fakeUi */
function fakeUi(): Ui {
  return {
    measure: (_f: unknown, s: string): TextRun => ({
      glyphs: [...s].map((ch, i) => ({ ch, x: i * 7, cluster: i, gid: 1 })),
      width: s.length * 7,
      ascent: 10,
      descent: 3,
    }),
    measureEpoch: () => 0,
    showFocusRing: () => false,
    layoutAll: false,
    wake() {},
  } as unknown as Ui;
}

interface Scene {
  root: Panel;
  split: SplitView;
  scroll: ScrollView;
  vstack: Stack;
  grid: Grid;
  table: TableView;
  dialog: Dialog;
  wrapped: Label;
  cell: Label;
  pinned: Panel;
}

/** one of every container the layout pass walks, plus a wrapped label */
function newScene(): Scene {
  const fill = () => ({ left: 0, right: 0, top: 0, bottom: 0 });
  const root = new Panel();

  const dock = new Dock();
  dock.padding = 6;
  dock.gap = 4;
  dock.anchors = fill();
  root.add(dock);

  const header = new Stack();
  header.axis = 'h';
  header.dock = 'top';
  header.height = 30;
  dock.add(header);
  const pinned = header.add(new Panel());
  pinned.width = 80;
  pinned.height = 24;
  header.add(new Label('header'));
  const stretchy = header.add(new Label('stretchy'));
  stretchy.stackSize = { kind: 'fill', weight: 1, px: 0 };

  const split = new SplitView();
  split.axis = 'h';
  split.pos = 220;
  split.minA = 80;
  split.minB = 80;
  split.dock = 'fill';
  dock.add(split);

  const grid = new Grid();
  grid.columns = [
    { kind: 'content', align: 'end' },
    { kind: 'fill', weight: 1, align: 'stretch' },
  ];
  split.add(grid);
  for (let i = 0; i < 4; i++) {
    grid.add(new Label(`field ${i}`));
    const box = grid.add(new Panel());
    box.height = 24;
  }
  const wide = grid.add(new Panel());
  wide.gridSpan = 2;
  wide.height = 18;

  const right = split.add(new Panel());

  const scroll = new ScrollView();
  scroll.anchors = { left: 0, right: 0, top: 0, bottom: 120 };
  right.add(scroll);
  const vstack = new Stack();
  vstack.axis = 'v';
  vstack.anchors = { left: 0, right: 0, top: 0 };
  vstack.padding = 8;
  scroll.add(vstack);
  let cell!: Label;
  for (let i = 0; i < 12; i++) {
    const row = new Stack();
    row.axis = 'h';
    row.stackSize = { kind: 'fixed', px: 28, weight: 0 };
    vstack.add(row);
    const l = row.add(new Label(`row ${i}`));
    row.add(new Button(`go ${i}`));
    if (i === 5) cell = l;
  }
  const wrapped = right.add(new Label('a wrapped label whose height follows the width it is given'));
  wrapped.wrap = true;
  wrapped.anchors = { left: 8, right: 8, bottom: 8 };

  const table = new TableView();
  table.columns = [
    { key: 'a', title: 'A', weight: 1, width: 0, kind: '' },
    { key: 'b', title: 'B', weight: 0, width: 90, kind: '' },
  ];
  table.rowCount = 400;
  table.anchors = { left: 8, right: 8, bottom: 8 };
  table.height = 100;
  right.add(table);

  const dialog = new Dialog();
  dialog.title = 'parity';
  dialog.cardW = 300;
  dialog.cardH = 180;
  dialog.anchors = fill();
  root.add(dialog);
  const body = dialog.add(new Label('dialog body'));
  body.anchors = { left: 16, top: 16, right: 16 };
  dialog.add(new Checkbox('agree'));

  return { root, split, scroll, vstack, grid, table, dialog, wrapped, cell, pinned };
}

interface Step {
  name: string;
  w: number;
  h: number;
  mutate?: (s: Scene) => void;
}

// every mutation invalidates the way the protocol layer or a widget's own
// interaction would
const steps: Step[] = [
  { name: 'initial', w: 900, h: 600 },
  {
    name: 'label text',
    w: 900,
    h: 600,
    mutate: (s) => {
      s.cell.text = 'a considerably longer cell label';
      s.cell.invalidate();
    },
  },
  { name: 'narrower viewport', w: 700, h: 600 },
  { name: 'scroll', w: 700, h: 600, mutate: (s) => s.scroll.scrollBy(120) },
  {
    name: 'divider drag',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.split.pos = 320;
      s.split.invalidate();
    },
  },
  {
    name: 'explicit width',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.pinned.width = 160;
      s.pinned.invalidate();
    },
  },
  {
    name: 'append a row',
    w: 700,
    h: 600,
    mutate: (s) => {
      const row = new Stack();
      row.axis = 'h';
      row.stackSize = { kind: 'fixed', px: 28, weight: 0 };
      s.vstack.add(row);
      row.add(new Label('appended'));
    },
  },
  {
    name: 'drop a row',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.vstack.children.splice(2, 1);
      s.vstack.invalidateLayout();
    },
  },
  {
    name: 'wrapped text',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.wrapped.text =
        'a much longer wrapped label, long enough that the line count changes ' +
        'with the width it is handed and the height follows';
      s.wrapped.invalidate();
    },
  },
  {
    name: 'row count',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.table.rowCount = 5000;
      s.table.invalidate();
    },
  },
  {
    name: 'grid column',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.grid.columns[0] = { kind: 'fixed', px: 140, align: 'end' };
      s.grid.invalidate();
    },
  },
  {
    name: 'stack spacing',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.vstack.spacing = 20;
      s.vstack.invalidate();
    },
  },
  {
    name: 'anchors',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.pinned.anchors = { left: 12, top: 3 };
      s.pinned.invalidate();
    },
  },
  {
    name: 'card size',
    w: 700,
    h: 600,
    mutate: (s) => {
      s.dialog.cardW = 420;
      s.dialog.cardH = 240;
      s.dialog.invalidate();
    },
  },
  { name: 'wider viewport', w: 1100, h: 640 },
  { name: 'quiet frame', w: 1100, h: 640 },
];

/** what buildFrame does to the root, minus the paint half */
function layoutRoot(u: Ui, root: Widget, w: number, h: number, all: boolean): void {
  (u as unknown as { layoutAll: boolean }).layoutAll = all;
  root.ui = u;
  root.bounds = rect(0, 0, w, h);
  layoutSubtree(root);
}

function sameRect(a: Rect, b: Rect): boolean {
  return a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h;
}

function compareBounds(step: string, path: string, got: Widget, want: Widget): void {
  const show = (r: Rect) => `(${r.x},${r.y} ${r.w}x${r.h})`;
  if (!sameRect(got.bounds, want.bounds)) {
    throw new Error(`${step}: ${path} bounds ${show(got.bounds)}, full layout gives ${show(want.bounds)}`);
  }
  if (got.children.length !== want.children.length) {
    throw new Error(
      `${step}: ${path} has ${got.children.length} children, full layout has ${want.children.length}`,
    );
  }
  for (let i = 0; i < got.children.length; i++) {
    const c = got.children[i]!;
    compareBounds(step, `${path}/${i}:${c.constructor.name}`, c, want.children[i]!);
  }
}

test('layout: skipping clean subtrees matches a full layout', () => {
  const skipUI = fakeUi();
  const skipScene = newScene();
  const fullUI = fakeUi();
  const fullScene = newScene();

  steps.forEach((step, i) => {
    step.mutate?.(skipScene);
    step.mutate?.(fullScene);
    layoutRoot(skipUI, skipScene.root, step.w, step.h, false);
    layoutRoot(fullUI, fullScene.root, step.w, step.h, true);
    compareBounds(`step ${i} (${step.name})`, 'root', skipScene.root, fullScene.root);
  });
});

// the parity test would pass just as well if nothing were ever skipped.
// corrupting a child's bounds and finding them still corrupt after a quiet
// frame proves the parent never recursed.
test('layout: a quiet frame skips clean subtrees', () => {
  const u = fakeUi();
  const s = newScene();
  layoutRoot(u, s.root, 900, 600, false);
  layoutRoot(u, s.root, 900, 600, false);

  const junk = rect(-1, -2, -3, -4);
  s.cell.bounds = junk;
  layoutRoot(u, s.root, 900, 600, false);
  if (!sameRect(s.cell.bounds, junk)) {
    throw new Error('a quiet frame re-laid-out a clean subtree');
  }

  s.cell.invalidate();
  layoutRoot(u, s.root, 900, 600, false);
  if (sameRect(s.cell.bounds, junk)) {
    throw new Error('an invalidated widget kept its stale bounds');
  }
});
