import { Color, hex } from '../gfx/color';

/**
 * Token Colors are mutated in place (widgets resolve them once at inflate
 * time), which means a display list recorded before a theme change comes out
 * holding the *new* colors. The retained list is not the record of what is on
 * screen anymore. Anything comparing lists across frames has to notice, so a
 * theme change bumps this counter and the renderer stops trusting the diff.
 * (The native terminal needs no such thing: its Color is a value type, so its
 * commands carry copies.)
 */
let epoch = 0;

export const themeEpoch = (): number => epoch;

/** Call right after mutating any token Color. */
export const bumpThemeEpoch = (): void => {
  epoch++;
};

/**
 * Design tokens 
 */
export const theme: Record<string, Color> & {
  bg: Color;
  accent: Color;
  ink: Color;
  inkDim: Color;
  inkFaint: Color;
  panel: Color;
  panelAlt: Color;
  panelInset: Color;
  edge: Color;
  edgeSoft: Color;
  titlebar: Color;
  control: Color;
  controlEdge: Color;
} = {
  bg: hex('#16181d'),
  accent: hex('#4f8cff'),
  ink: hex('#e8eaf0'),
  inkDim: hex('#9aa3b2'),
  inkFaint: hex('#6b7280'),
  panel: hex('#262b33'),
  panelAlt: hex('#20252c'),
  panelInset: hex('#1b1f26'),
  edge: hex('#3a4150'),
  edgeSoft: hex('#333a46'),
  titlebar: hex('#2e343e'),
  control: hex('#2e343e'),
  controlEdge: hex('#4a5262'),
};
