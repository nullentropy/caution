import type { WasmTerm } from './wasm';

const KEY_NAMES: Record<string, string> = {
  ArrowLeft: 'Left',
  ArrowRight: 'Right',
  ArrowUp: 'Up',
  ArrowDown: 'Down',
};

const MODIFIERS = new Set(['Shift', 'Meta', 'Control', 'Alt', 'CapsLock', 'AltGraph', 'Fn', 'Dead']);

function keyName(e: KeyboardEvent): string | null {
  if (MODIFIERS.has(e.key)) return null;
  const mapped = KEY_NAMES[e.key];
  if (mapped) return mapped;
  return e.key.length === 1 ? e.key.toLowerCase() : e.key;
}

// keys the funnel textarea must keep while a text editor is focused: anything
// else it edits natively and reports back through textSync
const FUNNEL_PASSES = new Set(['Tab', 'Escape', 'Enter', 'Up', 'Down']);

export interface InputHooks {
  funnelActive(): boolean;
  afterInput(): void;
}

export function attachInput(canvas: HTMLCanvasElement, term: WasmTerm, hooks: InputHooks): void {
  const pos = (e: { clientX: number; clientY: number }): [number, number] => {
    const r = canvas.getBoundingClientRect();
    return [e.clientX - r.left, e.clientY - r.top];
  };
  let rightCaptured = false;

  canvas.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    if (rightCaptured) {
      rightCaptured = false;
      return;
    }
    term.contextClick(...pos(e));
    hooks.afterInput();
  });

  const AUX = new Set([3, 4]);
  for (const name of ['pointerup', 'auxclick', 'mouseup'] as const) {
    canvas.addEventListener(name, (e) => {
      if (AUX.has((e as MouseEvent).button)) e.preventDefault();
    });
  }

  canvas.addEventListener('pointerdown', (e) => {
    if (AUX.has(e.button)) {
      e.preventDefault();
      term.aux(e.button + 1); // the DOM counts from 0, the SDK from 1
      hooks.afterInput();
      return;
    }
    if (e.button === 2) {
      if (term.rightDown(...pos(e))) {
        rightCaptured = true;
        canvas.setPointerCapture(e.pointerId);
      }
      e.preventDefault();
      return;
    }
    if (e.button !== 0) return;
    canvas.setPointerCapture(e.pointerId);
    term.pointerDown(...pos(e));
    e.preventDefault(); // keeps focus where the terminal put it (the funnel textarea)
    hooks.afterInput();
  });

  canvas.addEventListener('pointermove', (e) => {
    term.pointerMove(...pos(e));
  });

  canvas.addEventListener('pointerup', (e) => {
    if (e.button === 2) {
      term.rightUp(...pos(e));
      return;
    }
    if (e.button !== 0) return;
    term.pointerUp(...pos(e));
    hooks.afterInput();
  });

  canvas.addEventListener(
    'wheel',
    (e) => {
      const scale = e.deltaMode === 1 ? 16 : 1;
      let dx = e.deltaX * scale;
      let dy = e.deltaY * scale;
      if (e.shiftKey && dx === 0) {
        // mouse-wheel convention: shift turns vertical into horizontal
        dx = dy;
        dy = 0;
      }
      term.wheel(...pos(e), dx, dy);
      e.preventDefault();
    },
    { passive: false },
  );

  window.addEventListener('keydown', (e) => {
    const name = keyName(e);
    if (!name) return;
    if (hooks.funnelActive() && !FUNNEL_PASSES.has(name) && !e.metaKey && !e.ctrlKey && !e.altKey) return;
    if (term.keyDown(name, e.metaKey, e.ctrlKey, e.shiftKey, e.altKey)) e.preventDefault();
    hooks.afterInput();
  });
}
