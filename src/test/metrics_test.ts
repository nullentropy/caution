// metric-token fixtures mirroring go/terminal/ui/metrics_test.go, the two
// terminals must resolve the same wire names into the same chrome geometry
import { DisplayList, type RectCmd } from '../gfx/painter';
import type { TextRun } from '../gfx/text/shaper';
import { applyMetricTokens, metrics } from '../ui/metrics';
import type { Ui } from '../ui/ui';
import { Checkbox } from '../ui/widgets';
import { eq, test } from './harness';

/** fixed-advance fake measure, like the Go layout tests' fakeUi */
const fakeUi = {
  measure: (_f: unknown, s: string): TextRun => ({
    glyphs: [...s].map((ch, i) => ({ ch, x: i * 7, cluster: i, gid: 1 })),
    width: s.length * 7,
    ascent: 10,
    descent: 3,
  }),
  measureEpoch: () => 0,
  showFocusRing: () => false,
} as unknown as Ui;

const defaults = { ...metrics };

test('metrics: wire names map onto the table; unknown names are ignored', () => {
  try {
    applyMetricTokens({ 'radius.control': 3, 'border.width': 2, 'no.such.token': 99 });
    eq(metrics.radiusControl, 3);
    eq(metrics.borderWidth, 2);
    eq(metrics.radiusPopover, defaults.radiusPopover, 'untouched token changed');
  } finally {
    Object.assign(metrics, defaults);
  }
});

test('metrics: layout tokens change measurement, and memo-free reads follow live', () => {
  const cb = new Checkbox('ab', false); // 2 chars × 7px fake advance
  cb.ui = fakeUi;
  try {
    eq(cb.intrinsicSize(), { w: defaults.checkboxSize + defaults.spaceGap + 14, h: defaults.checkboxSize });
    applyMetricTokens({ 'checkbox.size': 14, 'space.gap': 6 });
    eq(cb.intrinsicSize(), { w: 14 + 6 + 14, h: 14 }, 'intrinsic size still on the old table');
  } finally {
    Object.assign(metrics, defaults);
  }
});

test('metrics: widgets resolve the table at paint time, not construct time', () => {
  const cb = new Checkbox('opt', false);
  cb.ui = fakeUi; // constructed BEFORE the metrics change
  cb.bounds = { x: 0, y: 0, w: 120, h: 24 };
  const boxOf = (dl: DisplayList) => dl.cmds.find((c): c is RectCmd => c.kind === 'rect' && c.borderWidth > 0)!;
  try {
    const before = new DisplayList();
    cb.paintSelf(before);
    eq(boxOf(before).radii.tl, defaults.radiusControl);

    applyMetricTokens({ 'radius.control': 0, 'border.width': 2 });
    const after = new DisplayList();
    cb.paintSelf(after);
    const box = boxOf(after);
    eq(box.radii.tl, 0, 'radius still resolved from the old table');
    eq(box.borderWidth, 3, 'emphasis border must be border.width × 1.5');
  } finally {
    Object.assign(metrics, defaults);
  }
});

test('metrics: the checkbox mark can never round into a radio', () => {
  const cb = new Checkbox('x', false);
  cb.ui = fakeUi;
  cb.bounds = { x: 0, y: 0, w: 120, h: 24 };
  const boxOf = (dl: DisplayList) => dl.cmds.find((c): c is RectCmd => c.kind === 'rect' && c.rect.w === metrics.checkboxSize)!;
  try {
    // 6px of radius on a 14px box is 43% round and reads as a radio
    // the painted radius caps at size/3 - the default pair's own proportion
    applyMetricTokens({ 'checkbox.size': 14 });
    for (const checked of [false, true]) {
      cb.checked = checked;
      const dl = new DisplayList();
      cb.paintSelf(dl);
      eq(boxOf(dl).radii.tl, 14 / 3, `checked=${checked} box rounder than size/3`);
    }
    // squarer themes pass through - the cap never rounds a corner up
    applyMetricTokens({ 'radius.control': 0 });
    const square = new DisplayList();
    cb.paintSelf(square);
    eq(boxOf(square).radii.tl, 0);
  } finally {
    Object.assign(metrics, defaults);
  }
});
