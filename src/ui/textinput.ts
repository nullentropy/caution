// The hidden-input funnel (see README.md). A single invisible
// <textarea> is positioned over whichever TextField holds focus. It receives
// real keyboard focus, so IME composition, dead keys, emoji pickers,
// clipboard, undo, and word-jump keys all come from the browser/OS for free.
// The GL layer renders the text, caret, and selection. This element is only
// the input funnel.

import type { TextField } from './textfield';

class InputFunnel {
  private ta: HTMLTextAreaElement | null = null;
  private current: TextField | null = null;
  private releasing = false;

  acquire(field: TextField): void {
    const ta = this.ensure();
    this.current = field;
    ta.value = field.value;
    // screen readers announce the funnel while editing
    ta.setAttribute('aria-label', field.placeholder || 'text field');
    // position over the field so IME candidate windows appear near the text
    const canvas = field.ui?.canvas.getBoundingClientRect();
    ta.style.left = `${(canvas?.left ?? 0) + field.bounds.x}px`;
    ta.style.top = `${(canvas?.top ?? 0) + field.bounds.y}px`;
    ta.style.width = `${Math.max(20, field.bounds.w)}px`;
    ta.style.height = `${Math.max(16, field.bounds.h)}px`;
    ta.focus({ preventScroll: true });
    const end = ta.value.length;
    ta.setSelectionRange(end, end);
    field.syncSelection(end, end);
  }

  // resync the textarea after a server-forced value change on the field
  refresh(field: TextField): void {
    if (this.current !== field || !this.ta) return;
    this.ta.value = field.value;
    const end = field.value.length;
    this.ta.setSelectionRange(end, end);
  }

  release(field: TextField): void {
    if (this.current !== field) return;
    this.current = null;
    if (this.ta && document.activeElement === this.ta) {
      this.releasing = true;
      this.ta.blur();
      this.releasing = false;
    }
  }

  // drive the textarea's selection from canvas mouse gestures (UTF-16 units)
  setSelection(start: number, end: number, dir: 'forward' | 'backward' = 'forward'): void {
    if (!this.ta || !this.current) return;
    this.ta.setSelectionRange(start, end, dir);
    this.current.syncSelection(this.ta.selectionStart, this.ta.selectionEnd);
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
      if ((e as InputEvent).inputType === 'insertText') this.current?.sound('type');
      this.current?.syncFromTextarea(ta.value, ta.selectionStart, ta.selectionEnd);
    });
    document.addEventListener('selectionchange', () => {
      if (this.current && document.activeElement === ta) {
        this.current.syncSelection(ta.selectionStart, ta.selectionEnd);
      }
    });
    ta.addEventListener('keydown', (e) => {
      if (!this.current) return;
      if (e.key === 'Enter' && !this.current.multiline) {
        e.preventDefault(); // single-line field: no newline, commit instead
        this.current.commit();
        return;
      }
      // multiline: Up/Down must move by *visual* (wrapped) lines, which only
      // the widget knows, the funnel textarea wraps nothing (wrap=off).
      if (
        this.current.multiline &&
        (e.key === 'ArrowUp' || e.key === 'ArrowDown') &&
        !e.metaKey &&
        !e.altKey &&
        !e.ctrlKey
      ) {
        e.preventDefault();
        this.current.verticalMove(e.key === 'ArrowUp' ? -1 : 1, e.shiftKey);
        return;
      }
      // escape-to-revert; a clean field lets <esc> bubble to the shell
      // (dialog dismissal, selection clearing)
      if (e.key === 'Escape' && this.current.revert()) {
        e.preventDefault();
        e.stopPropagation();
      }
    });
    ta.addEventListener('blur', () => {
      // focus left through a non-canvas path (e.g. browser chrome)
      if (!this.releasing && this.current) this.current.ui?.setFocus(null, false);
    });

    this.ta = ta;
    return ta;
  }
}

export const funnel = new InputFunnel();
