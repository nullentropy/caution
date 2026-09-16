import type { WasmTerm } from './wasm';

// UTF-16 unit index -> rune index, and back. the textarea counts in units,
// the terminal in runes
function runeAt(s: string, cu: number): number {
  let runes = 0;
  for (let i = 0; i < cu && i < s.length; ) {
    i += (s.codePointAt(i) ?? 0) > 0xffff ? 2 : 1;
    runes++;
  }
  return runes;
}

function unitAt(s: string, rune: number): number {
  let i = 0;
  for (let r = 0; r < rune && i < s.length; r++) i += (s.codePointAt(i) ?? 0) > 0xffff ? 2 : 1;
  return i;
}

export class Funnel {
  private ta: HTMLTextAreaElement | null = null;
  private active = false;
  private releasing = false;

  constructor(
    private canvas: HTMLCanvasElement,
    private term: WasmTerm,
  ) {}

  get isActive(): boolean {
    return this.active && this.ta != null && document.activeElement === this.ta;
  }

  // bring the textarea in line with the terminal's focused editor, or let it
  // go when none is focused. runs after every input event and every frame
  sync(): void {
    const st = this.term.textState();
    if (!st) {
      if (this.active) this.release();
      return;
    }
    const [value, selStart, selEnd, , placeholder, x, y, w, h] = st;
    const ta = this.ensure();
    if (!this.active) {
      this.active = true;
      ta.focus({ preventScroll: true });
    }
    ta.setAttribute('aria-label', placeholder || 'text field');
    // over the field so IME candidate windows appear near the text
    const origin = this.canvas.getBoundingClientRect();
    ta.style.left = `${origin.left + x}px`;
    ta.style.top = `${origin.top + y}px`;
    ta.style.width = `${Math.max(20, w)}px`;
    ta.style.height = `${Math.max(16, h)}px`;
    if (ta.value !== value) ta.value = value;
    const s = unitAt(value, Math.min(selStart, selEnd));
    const e = unitAt(value, Math.max(selStart, selEnd));
    const dir = selEnd < selStart ? 'backward' : 'forward';
    if (ta.selectionStart !== s || ta.selectionEnd !== e || ta.selectionDirection !== dir) {
      ta.setSelectionRange(s, e, dir);
    }
  }

  private release(): void {
    this.active = false;
    if (this.ta && document.activeElement === this.ta) {
      this.releasing = true;
      this.ta.blur();
      this.releasing = false;
    }
  }

  private report(typed: boolean): void {
    const ta = this.ta!;
    const v = ta.value;
    let s = runeAt(v, ta.selectionStart);
    let e = runeAt(v, ta.selectionEnd);
    if (ta.selectionDirection === 'backward') [s, e] = [e, s];
    this.term.textSync(v, s, e, typed);
  }

  private ensure(): HTMLTextAreaElement {
    if (this.ta) return this.ta;
    const ta = document.createElement('textarea');
    Object.assign(ta.style, {
      position: 'fixed',
      opacity: '0',
      pointerEvents: 'none',
      left: '0px',
      top: '0px',
      border: '0',
      padding: '0',
      resize: 'none',
      overflow: 'hidden',
      zIndex: '-1',
    });
    ta.autocapitalize = 'off';
    ta.autocomplete = 'off';
    ta.spellcheck = false;
    ta.wrap = 'off';
    document.body.appendChild(ta);

    ta.addEventListener('input', (e) => {
      if (this.active) this.report((e as InputEvent).inputType === 'insertText');
    });
    document.addEventListener('selectionchange', () => {
      if (this.active && document.activeElement === ta) this.report(false);
    });
    ta.addEventListener('blur', () => {
      // focus left through a non-canvas path (e.g. browser chrome)
      if (!this.releasing && this.active) {
        this.active = false;
        this.term.blur();
      }
    });
    this.ta = ta;
    return ta;
  }
}
