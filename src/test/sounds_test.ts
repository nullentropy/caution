import { rect } from '../gfx/geom';
import type { Ui } from '../ui/ui';
import { Ui as RealUi } from '../ui/ui';
import { Button, Checkbox, Panel } from '../ui/widgets';
import { eq, test } from './harness';

function soundUi(table: Record<string, string>): { u: Ui; played: string[] } {
  const u = Object.create(RealUi.prototype) as Ui;
  const played: string[] = [];
  u.root = new Panel();
  u.overlay = null;
  u.playSound = (src) => played.push(src);
  u.setSounds(table);
  Object.assign(u, { surface: { invalidate() {} }, tipTimer: null, tipState: null, focused: null });
  return { u, played };
}

const TABLE = { press: '/press.wav', toggle: '/toggle.wav', whoosh: '/whoosh.wav' };

test('sounds: widgets fire their gesture token', () => {
  const { u, played } = soundUi(TABLE);
  const btn = new Button('go');
  btn.ui = u;
  btn.bounds = rect(10, 10, 100, 30);
  btn.activate();
  eq(played, ['/press.wav']);
  btn.onPointerDown(20, 20, 1);
  btn.onPointerUp(20, 20);
  eq(played, ['/press.wav', '/press.wav']);
  btn.onPointerDown(20, 20, 1);
  btn.onPointerUp(500, 500); // released outside: no click, no sound
  eq(played.length, 2);

  const cb = new Checkbox('opt', false);
  cb.ui = u;
  cb.activate();
  eq(played[2], '/toggle.wav');
});

test('sounds: a per-widget token replaces the gesture, and none silences it', () => {
  const { u, played } = soundUi(TABLE);
  const btn = new Button('send');
  btn.ui = u;
  btn.soundToken = 'whoosh';
  btn.activate();
  eq(played, ['/whoosh.wav']);
  btn.soundToken = 'none';
  btn.activate();
  eq(played.length, 1);
  btn.soundToken = 'not-in-the-table';
  btn.activate();
  eq(played.length, 1);
});

test('sounds: an empty table or a missing hook is silent', () => {
  const { u, played } = soundUi({});
  const btn = new Button('go');
  btn.ui = u;
  btn.activate();
  eq(played, []);
  u.setSounds(TABLE);
  u.playSound = null;
  btn.activate(); // must not throw
  eq(played, []);
});
