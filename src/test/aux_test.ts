import { rect } from '../gfx/geom';
import type { Ui } from '../ui/ui';
import { Ui as RealUi } from '../ui/ui';
import { Panel } from '../ui/widgets';
import { eq, test } from './harness';

// the real methods run, with only the surface's invalidate stubbed out
function auxUi(): Ui {
  const u = Object.create(RealUi.prototype) as Ui;
  u.root = new Panel();
  u.root.bounds = rect(0, 0, 200, 100);
  u.overlay = null;
  u.onAux = null;
  Object.assign(u, { surface: { invalidate() {} }, tipTimer: null, tipState: null });
  return u;
}

test('aux: buttons 4 and 5 go upstream', () => {
  const u = auxUi();
  const got: number[] = [];
  u.onAux = (b) => got.push(b);
  u.auxDown(4);
  u.auxDown(5);
  eq(got, [4, 5]);
});

test('aux: a press closes an open popover instead of navigating', () => {
  const u = auxUi();
  let fired = 0;
  u.onAux = () => fired++;
  u.overlay = new Panel();
  u.auxDown(4);
  eq(u.overlay, null, 'the popover stayed open');
  eq(fired, 0, 'the press that closed the popover also navigated');
  u.auxDown(4);
  eq(fired, 1);
});
