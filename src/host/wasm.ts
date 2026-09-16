import '../vendor/wasm_exec.js';

import type { WasmText } from '../gfx/text/wasmengine';

export interface WasmTerm {
  frame(w: number, h: number, dpr: number): number;
  take(dst: Uint8Array): void;
  tickIn(): number;
  tick(): void;
  pointerDown(x: number, y: number): void;
  pointerMove(x: number, y: number): void;
  pointerUp(x: number, y: number): void;
  rightDown(x: number, y: number): boolean;
  rightUp(x: number, y: number): void;
  contextClick(x: number, y: number): void;
  aux(button: number): void;
  wheel(x: number, y: number, dx: number, dy: number): void;
  keyDown(name: string, meta: boolean, ctrl: boolean, shift: boolean, alt: boolean): boolean;
  textSync(value: string, selStart: number, selEnd: number, typed: boolean): void;
  textState(): [string, number, number, boolean, string, number, number, number, number] | null;
  blur(): void;
  semantics(): string;
  activate(id: number, row: number): void;
  fullscreen(on: boolean): void;
}

export interface HostCallbacks {
  url: string;
  sid: string | null;
  viewSize(): [number, number];
  invalidate(): void;
  setCursor(name: string): void;
  setTitle(title: string): void;
  writeClipboard(text: string): void;
  preloadImage(src: string): void;
  preloadSound(src: string): void;
  play(src: string, loop: boolean): void;
  stop(src: string): void;
  mounted(sid: string): void;
}

export interface Terminal {
  text: WasmText;
  term: WasmTerm;
}

export async function loadTerminal(url: string, host: HostCallbacks): Promise<Terminal> {
  (globalThis as any).__cautionHost = host;
  const go = new Go();
  const resp = await fetch(url);
  if (!resp.ok) throw new Error(`${url}: ${resp.status}`);
  let inst: WebAssembly.Instance;
  try {
    ({ instance: inst } = await WebAssembly.instantiateStreaming(resp, go.importObject));
  } catch {
    const buf = await (await fetch(url)).arrayBuffer();
    ({ instance: inst } = await WebAssembly.instantiate(buf, go.importObject));
  }
  void go.run(inst);
  const g = globalThis as any;
  for (let i = 0; i < 200 && !(g.__cautionTerm && g.__cautionText); i++) {
    await new Promise((r) => setTimeout(r, 5));
  }
  if (!g.__cautionTerm || !g.__cautionText) throw new Error('terminal did not start');
  return { text: g.__cautionText as WasmText, term: g.__cautionTerm as WasmTerm };
}
