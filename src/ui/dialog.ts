import { BLACK, withAlpha } from '../gfx/color';
import { font } from '../gfx/font';
import { rect, Rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import { metrics } from './metrics';
import type { SemanticSpec } from './semantics';
import { theme } from './theme';
import { contains, Widget } from './widget';

const TITLE_FONT = font(15, { weight: 600 });
/** the card's title band, the dialog.titleHeight token */
const titleH = (): number => metrics.dialogTitleHeight;

/**
 * Modal dialog: a full-viewport scrim that swallows input, with a centered
 * card whose children lay out (frame/anchors) against the content area below
 * the title. Z-order comes from tree order and the server appends dialogs last.
 * Clicking the scrim or pressing Escape emits `dismiss`. The server decides
 * whether to remove the node.
 */
export class Dialog extends Widget {
  title = '';
  cardW = 420;
  cardH = 200;
  onDismiss: (() => void) | null = null;

  override get interactive(): boolean {
    return true; // the scrim swallows every click that misses the card
  }

  cardRect(): Rect {
    const b = this.bounds;
    return rect(b.x + (b.w - this.cardW) / 2, b.y + (b.h - this.cardH) / 2, this.cardW, this.cardH);
  }

  override layoutChildren(): void {
    this.adopt();
    const card = this.cardRect();
    this.layoutChildrenInto(rect(card.x, card.y + titleH(), card.w, card.h - titleH()));
  }

  override paintSelf(dl: DisplayList): void {
    dl.rect(this.bounds, { color: withAlpha(BLACK, 0.45) });
    const card = this.cardRect();
    dl.shadow(card, { radius: metrics.radiusPanel, blur: 40, offset: [0, 18], color: withAlpha(BLACK, 0.5) });
    dl.rect(card, { color: theme.panel, radius: metrics.radiusPanel, borderWidth: metrics.borderWidth, borderColor: theme.edge });
    const m = this.ui!.measure(TITLE_FONT, this.title);
    dl.text(this.title, card.x + 20, card.y + (titleH() - (m.ascent + m.descent)) / 2, TITLE_FONT, theme.ink);
    const hair = metrics.borderWidth;
    dl.rect(rect(card.x, card.y + titleH() - hair, card.w, hair), { color: theme.edge });
  }

  override onPointerDown(x: number, y: number): void {
    if (!contains(this.cardRect(), x, y) && this.onDismiss) {
      this.sound('close');
      this.onDismiss();
    }
  }

  override semantics(): SemanticSpec {
    return { role: 'text', label: `dialog: ${this.title}` };
  }
}
