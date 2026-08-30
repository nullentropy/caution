/**
 * The metric half of the design-token system for widget-chrome geometry. 
 *
 * Widgets read fields at measure/paint time so a `metrics` op restyles live widgets.
 */
export const metrics = {
  /** "radius.control": buttons, fields, checkboxes, row highlights. */
  radiusControl: 6,
  /** "radius.popover": menus, dropdowns, floating lists. */
  radiusPopover: 8,
  /** "radius.panel": dialog cards. */
  radiusPanel: 12,
  /** "border.width": control hairlines and separators (emphasis = ×1.5). */
  borderWidth: 1,
  /** "focus.ring": the keyboard-focus ring's stroke width. */
  focusRing: 2,

  /** "control.height": buttons, selects, text fields (min); tabs +2 for the underline. */
  controlHeight: 32,
  /** "control.padX": a control's horizontal text padding (buttons, tabs). */
  controlPadX: 16,
  /** "row.height": list rows.  menus, select popups, radio rows, the menubar. the table's default. */
  rowHeight: 28,
  /** "table.headerHeight" */
  tableHeaderHeight: 32,
  /** "table.cellPadX" */
  tableCellPadX: 12,
  /** "checkbox.size": the checkbox box; radios are -2 (circles read bigger than squares). */
  checkboxSize: 18,
  /** "slider.thumb": thumb radius. */
  sliderThumb: 8,
  /** "slider.track": track height. */
  sliderTrack: 4,
  /** "dialog.titleHeight" */
  dialogTitleHeight: 48,
  /** "space.pad": field/select inner text inset. */
  spacePad: 10,
  /** "space.gap": mark<->label gap (checkbox, radio). */
  spaceGap: 10,
};

/**
 * The corner radius the checkbox mark actually paints: radius.control capped
 * at a third of checkbox.size. Shape is semantics for marks, a rounded
 * square is a checkbox, a circle is a radio, and the default pair (18, 6)
 * sits at the cap, so smaller boxes keep the default's proportion
 * instead of growing rounder until the distinction dissolves. The table's
 * 16px in-cell miniature caps for the same reason.
 */
export const checkboxRadius = (): number =>
  Math.min(metrics.radiusControl, metrics.checkboxSize / 3);

const slots: Record<string, keyof typeof metrics> = {
  'radius.control': 'radiusControl',
  'radius.popover': 'radiusPopover',
  'radius.panel': 'radiusPanel',
  'border.width': 'borderWidth',
  'focus.ring': 'focusRing',
  'control.height': 'controlHeight',
  'control.padX': 'controlPadX',
  'row.height': 'rowHeight',
  'table.headerHeight': 'tableHeaderHeight',
  'table.cellPadX': 'tableCellPadX',
  'checkbox.size': 'checkboxSize',
  'slider.thumb': 'sliderThumb',
  'slider.track': 'sliderTrack',
  'dialog.titleHeight': 'dialogTitleHeight',
  'space.pad': 'spacePad',
  'space.gap': 'spaceGap',
};

/**
 * Mutate the table in place. Unknown names are ignored (a newer server
 * against an older terminal degrades to the defaults it ships). Callers
 * outside the protocol layer want Ui.applyMetrics, which also drops size
 * memos and rebuilds the frame.
 */
export const applyMetricTokens = (tokens: Record<string, number>): void => {
  for (const [name, v] of Object.entries(tokens)) {
    const slot = slots[name];
    if (slot != null && typeof v === 'number' && Number.isFinite(v)) metrics[slot] = v;
  }
};
