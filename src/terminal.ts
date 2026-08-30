// The caution terminal: the fixed client runtime every caution app serves.
// It connects to the app server and renders whatever it is told - there is
// no application code on this side of the wire, ever. The server bundles
// this entrypoint (see caution.ServeClient) and generates the host page.

import { loadWasmEngine } from './gfx/text/wasmengine';
import { Session } from './proto/session';
import { Ui } from './ui/ui';

const canvas = document.getElementById('root') as HTMLCanvasElement;
const proto = location.protocol === 'https:' ? 'wss' : 'ws';

// The text engine loads before the session connects: metrics must not
// change under a laid-out tree, so the choice is one-shot. On failure or
// timeout the Canvas2D fallback ships the same UI with system fonts.
void (async () => {
  const engine = await loadWasmEngine('/shaper.wasm');
  const ui = new Ui(canvas, engine ?? undefined);
  // ?fps=N caps animation repaints, the way -fps does for the native
  // terminal; input still paints immediately.
  const fps = Number(new URLSearchParams(location.search).get('fps'));
  if (Number.isFinite(fps) && fps > 0) ui.surface.maxFps = fps;
  new Session(ui, `${proto}://${location.host}/ws`);
})();
