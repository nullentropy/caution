import '../vendor/wasm_exec.js';

export interface WasmTerm {
  /** service one frame: logical size, device size, clock in seconds. returns [animating, kind] */
  paint(w: number, h: number, devW: number, devH: number, time: number): [number, number];
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

/** the frame kinds paint reports */
export const FRAME_DAMAGE = 2;
export const FRAME_PARTIAL = 3;

export interface HostCallbacks {
  gl: WebGL2RenderingContext;
  url: string;
  sid: string | null;
  viewSize(): [number, number];
  invalidate(): void;
  setCursor(name: string): void;
  setTitle(title: string): void;
  writeClipboard(text: string): void;
  preloadSound(src: string): void;
  play(src: string, loop: boolean): void;
  stop(src: string): void;
  mounted(sid: string): void;
}

export async function loadTerminal(url: string, host: HostCallbacks): Promise<WasmTerm> {
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
  for (let i = 0; i < 200 && !g.__cautionTerm; i++) {
    await new Promise((r) => setTimeout(r, 5));
  }
  if (!g.__cautionTerm) throw new Error('terminal did not start');
  return g.__cautionTerm as WasmTerm;
}
