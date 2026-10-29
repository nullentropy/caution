// the terminal's entry point
//
// widgets, layout, protocol, and text are all from the Go terminal and
// compiled to wasm. here we have the canvas, socket, input, audio,
// and the "shadow" DOM for assistive tech.
import { Host, hostCallbacks } from './host/host';
import { loadTerminal } from './host/wasm';

const canvas = document.getElementById('root') as HTMLCanvasElement;

void (async () => {
  const callbacks = hostCallbacks(canvas);
  const wasm = await loadTerminal('/term.wasm', callbacks);
  callbacks.bind(new Host(canvas, wasm));
})().catch((e: unknown) => {
  console.error('caution: terminal failed to start:', e);
  const msg = document.createElement('pre');
  msg.style.cssText = 'color:#ddd;font:13px ui-monospace,monospace;padding:24px';
  msg.textContent = `caution: terminal failed to start\n${String(e)}`;
  document.body.replaceChildren(msg);
});
