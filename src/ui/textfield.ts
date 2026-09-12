import { withAlpha } from '../gfx/color';
import { font, MONO } from '../gfx/font';
import type { Font } from '../gfx/font';
import { inset, rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import type { TextRun } from '../gfx/text/shaper';
import { metrics } from './metrics';
import type { SemanticSpec } from './semantics';
import { funnel } from './textinput';
import { theme } from './theme';
import { paintFocusRing } from './widgets';
import { Size, Widget } from './widget';

const WORD_CH = /[\p{L}\p{N}_]/u;

/**
 * Single-line text input. Editing state (value, selection) is mirrored from
 * the hidden textarea funnel, which owns the keyboard. This widget owns the
 * mouse and all rendering. Selection indices are UTF-16 code units to match
 * the textarea. Glyph positions come from the shaper.
 */
export class TextField extends Widget {
  value = '';
  // per-node radius override (null = the radius.control metric)
  radius: number | null = null;
  placeholder = '';
  // text size in logical px (`size` prop) and family (`mono` prop)
  fontSize = 14;
  mono = false;

  sensitive = false;

  // fires on every edit (wire-level debouncing happens in the protocol layer)
  onInput: ((v: string) => void) | null = null;
  onCommit: ((v: string) => void) | null = null;

  protected selStart = 0;
  protected selEnd = 0;
  protected anchor = 0;
  private scrollX = 0;
  protected caretOn = true;
  private blinkTimer: ReturnType<typeof setInterval> | null = null;
  private lastCommitted = '';

  // last-seen values of the server's one-shot command props
  resetSeq = 0;
  overrideSeq = 0;

  get focused(): boolean {
    return this.ui?.isFocused(this) ?? false;
  }

  // multiline fields (TextArea) let <enter> insert a newline in the funnel
  get multiline(): boolean {
    return false;
  }

  // visual-line caret movement. only meaningful for multiline fields
  verticalMove(_dir: 1 | -1, _extend: boolean): void {}

  protected fieldFont(): Font {
    return font(this.fontSize, this.mono ? { family: MONO } : {});
  }

  override intrinsicSize(): Size {
    return { w: this.width ?? 220, h: this.height ?? Math.max(metrics.controlHeight, Math.round(this.fontSize * 1.7)) };
  }

  override get interactive(): boolean {
    return true;
  }

  override get focusable(): boolean {
    return true;
  }

  override cursor(): string {
    return 'text';
  }

  override onFocusChange(focused: boolean): void {
    if (focused) {
      this.lastCommitted = this.value;
      funnel.acquire(this);
      this.startBlink();
    } else {
      funnel.release(this);
      this.stopBlink();
      this.commit();
    }
    this.invalidate();
  }

  override semantics(): SemanticSpec {
    return { role: 'textbox', label: this.placeholder || 'text field', value: this.value };
  }

  // screenreader activation: take focus so the input funnel engages
  override activate(): void {
    this.ui?.setFocus(this, false);
  }

  // commit points (<enter>, blur) only fire if the value actually changed
  commit(): void {
    if (this.value === this.lastCommitted) return;
    this.lastCommitted = this.value;
    this.onCommit?.(this.value);
  }

  /**
   * Escape-to-revert: back to the value the field had at focus (or last
   * commit). Returns false on a clean field so Escape bubbles onward
   * (dialogs, selection clearing).
   */
  revert(): boolean {
    if (this.value === this.lastCommitted) return false;
    this.value = this.lastCommitted;
    funnel.refresh(this); // resync the hidden textarea, caret to the end
    this.onInput?.(this.value);
    this.invalidate();
    return true;
  }

  /**
   * Server-driven focus (`Focus()` in the SDK). Unlike a plain setFocus this
   * also (re)binds the input funnel: if focus was nominally on this field but
   * the funnel was released (a button click blurred the textarea, devtools
   * stole focus, ...), setFocus alone would no-op and keystrokes would vanish.
   */
  override grabFocus(): void {
    this.ui?.setFocus(this, false);
    if (this.focused) funnel.acquire(this);
  }

  /**
   * Server-forced clear (`ClearValue` in the SDK): unlike a plain value set,
   * this applies even while focused. The sanctioned exception to local echo
   * for submit-and-keep-typing flows. The funnel textarea is resynced so the
   * next keystroke starts from the empty string.
   */
  forceClear(): void {
    this.value = '';
    this.lastCommitted = '';
    this.selStart = this.selEnd = this.anchor = 0;
    this.scrollX = 0;
    funnel.refresh(this);
    this.invalidate();
  }

  /**
   * Server-forced value (`SetValueNow` in the SDK): applies even while
   * focused. The override half of the local-echo contract, for validation
   * and formatting. The pushed value becomes the new baseline (Escape
   * reverts to it, no commit fires unless the user edits again).
   */
  forceValue(v: string): void {
    this.value = v;
    this.lastCommitted = v;
    funnel.refresh(this);
    this.invalidate();
  }

  // called by the funnel after every edit
  syncFromTextarea(value: string, selStart: number, selEnd: number): void {
    const changed = this.value !== value;
    this.value = value;
    this.syncSelection(selStart, selEnd);
    if (changed) this.onInput?.(value);
    this.invalidate();
  }

  // called by the funnel on selection movement
  syncSelection(selStart: number, selEnd: number): void {
    if (this.selStart === selStart && this.selEnd === selEnd) return;
    this.selStart = selStart;
    this.selEnd = selEnd;
    this.caretOn = true; // moving the caret keeps it visible
    this.invalidate();
  }

  override paintSelf(dl: DisplayList): void {
    const b = this.bounds;
    if (this.ui?.showFocusRing(this)) paintFocusRing(dl, b, this.radius ?? metrics.radiusControl);
    dl.rect(b, {
      color: theme.panelInset,
      radius: this.radius ?? metrics.radiusControl,
      borderWidth: this.focused ? metrics.borderWidth * 1.5 : metrics.borderWidth,
      borderColor: this.focused ? theme.accent : theme.controlEdge,
    });

    const run = this.run();
    const innerW = b.w - metrics.spacePad * 2;
    const textY = b.y + (b.h - (run.ascent + run.descent)) / 2;
    const textH = run.ascent + run.descent;
     // keep the caret inside the viewport by sliding the text
    if (this.focused) {
      const caretX = this.xAtCU(this.selEnd);
      if (caretX - this.scrollX > innerW) this.scrollX = caretX - innerW;
      if (caretX - this.scrollX < 0) this.scrollX = caretX;
    }
    const textX = b.x + metrics.spacePad - this.scrollX;
     dl.pushClip(inset(b, 1.5));
    if (this.value === '' && this.placeholder) {
      dl.text(this.placeholder, b.x + metrics.spacePad, textY, this.fieldFont(), theme.inkFaint);
    }
    if (this.focused && this.selStart !== this.selEnd) {
      const x0 = this.xAtCU(Math.min(this.selStart, this.selEnd));
      const x1 = this.xAtCU(Math.max(this.selStart, this.selEnd));
      dl.rect(rect(textX + x0 - 1, textY - 1, x1 - x0 + 2, textH + 2), {
        color: withAlpha(theme.accent, 0.35),
        radius: 2,
      });
    }
    dl.text(this.sensitive ? '*'.repeat(this.value.length) : this.value, textX, textY, this.fieldFont(), theme.ink);
    if (this.focused && this.selStart === this.selEnd && this.caretOn) {
      dl.rect(rect(textX + this.xAtCU(this.selEnd), textY - 1, 1.5, textH + 2), {
        color: theme.accent,
      });
    }
    dl.popClip();
}
 override onPointerDown(x: number, _y: number, detail: number): void {
    const cu = this.cuAtX(x - (this.bounds.x + metrics.spacePad) + this.scrollX);
    if (detail >= 3) {
      funnel.setSelection(0, this.value.length);
      this.anchor = 0;
    } else if (detail === 2) {
      const [s, e] = this.wordRangeCU(cu);
      funnel.setSelection(s, e);
      this.anchor = s;
    } else {
      funnel.setSelection(cu, cu);
      this.anchor = cu;
    }
  }

  override onPointerDrag(x: number, _y: number): void {
    const cu = this.cuAtX(x - (this.bounds.x + metrics.spacePad) + this.scrollX);
    if (cu >= this.anchor) funnel.setSelection(this.anchor, cu);
    else funnel.setSelection(cu, this.anchor, 'backward');
  }

  // -- geometry helpers ---------------------------------------------------------

  private run(): TextRun {
      return this.ui!.measure(this.fieldFont(), this.sensitive? '*'.repeat(this.value.length) : this.value);
  }

  // X offset (from the text origin) of a UTF-16 boundary
  private xAtCU(cu: number): number {
    const run = this.run();
    let units = 0;
    for (const g of run.glyphs) {
      if (units >= cu) return g.x;
      units += g.ch.length;
    }
    return run.width;
  }

  // nearest UTF-16 boundary to an x offset from the text origin
  private cuAtX(x: number): number {
    const run = this.run();
    let best = 0;
    let bestD = Infinity;
    let units = 0;
    const consider = (bx: number, cu: number) => {
      const d = Math.abs(x - bx);
      if (d < bestD) {
        bestD = d;
        best = cu;
      }
    };
    for (const g of run.glyphs) {
      consider(g.x, units);
      units += g.ch.length;
    }
    consider(run.width, units);
    return best;
  }

  protected wordRangeCU(cu: number): [number, number] {
    const cps: { ch: string; off: number }[] = [];
    let off = 0;
    for (const ch of this.value) {
      cps.push({ ch, off });
      off += ch.length;
    }
    if (!cps.length) return [0, 0];
    let i = cps.findIndex((c) => c.off + c.ch.length > cu);
    if (i < 0) i = cps.length - 1;
    const isWord = (s: string) => WORD_CH.test(s);
    const target = isWord(cps[i]!.ch);
    let s = i;
    let e = i + 1;
    while (s > 0 && isWord(cps[s - 1]!.ch) === target) s--;
    while (e < cps.length && isWord(cps[e]!.ch) === target) e++;
    return [cps[s]!.off, e < cps.length ? cps[e]!.off : this.value.length];
  }

  private startBlink(): void {
    this.stopBlink();
    this.caretOn = true;
    this.blinkTimer = setInterval(() => {
      this.caretOn = !this.caretOn;
      this.invalidate();
    }, 530);
  }

  private stopBlink(): void {
    if (this.blinkTimer != null) {
      clearInterval(this.blinkTimer);
      this.blinkTimer = null;
    }
  }
}
