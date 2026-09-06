// Wire protocol (JSON over WebSocket) - see README.md. Server-assigned integer
// node ids, patches stream down, semantic events stream up.

export type Props = Record<string, unknown>;

export interface NodeJson {
  id: number;
  type: string;
  p?: Props;
  kids?: NodeJson[];
}

export interface OpInsert {
  op: 'insert';
  parent: number;
  index: number;
  node: NodeJson;
}

export interface OpSet {
  op: 'set';
  id: number;
  p: Props;
}

export interface OpRemove {
  op: 'remove';
  id: number;
}

export interface OpMove {
  op: 'move';
  id: number;
  parent: number;
  index: number;
}

/** Virtualization fill: row data for a table window (not part of the node tree). */
export interface OpRows {
  op: 'rows';
  id: number;
  start: number;
  /** Clear the client's row cache first (sort order or dataset changed). */
  reset?: boolean;
  rows: string[][];
  /** Trees only: hierarchy meta parallel to rows. */
  meta?: RowMetaSpec[];
}

/** Server-driven theme tokens (color values or the tokens the client knows). */
/** Tree rows only: per-row hierarchy meta parallel to `rows`. */
export interface RowMetaSpec {
  key: string;
  d: number;
  k?: boolean;
  x?: boolean;
}

export interface OpTheme {
  op: 'theme';
  tokens: Record<string, string>;
}

/**
 * Server-driven metric tokens ("radius.control", "border.width", ...): the geometry
 * half of theming. The key is `metrics` rather than
 * `tokens` so the union stays unambiguous next to OpTheme's string map.
 */
export interface OpMetrics {
  op: 'metrics';
  metrics: Record<string, number>;
}

/** One entry in a server-driven menu (see OpMenu). */
export interface MenuItemSpec {
  id?: number;
  title?: string;
  key?: string;
  sep?: boolean;
  items?: MenuItemSpec[];
}

/**
 * Server-driven application menus. Realized natively by the desktop terminal
 * (real NSMenus on macOS; picks come back as session-level `menu` events).
 */
export interface OpMenu {
  op: 'menu';
  menu: { title: string; items: MenuItemSpec[] }[];
}

/** Server-driven window title (browser tab title / native titlebar). */
export interface OpTitle {
  op: 'title';
  title: string;
}

/** One registered session-wide shortcut ("cmd+j", canonical modifier order). */
export interface KeySpec {
  id: number;
  key: string;
}

/** Session-wide keyboard shortcuts; a match emits a `key` event with the id. */
export interface OpKeys {
  op: 'keys';
  keys: KeySpec[];
}

/**
 * Resource preload hint: warm caches before first use
 */
export interface OpResource {
  op: 'resource';
  images?: string[];
  sounds?: string[];
}

/** Play a sound once. */
export interface OpPlay {
  op: 'play';
  src: string;
  loop?: boolean;
}

export interface OpStop {
  op: 'stop';
  src?: string;
}

/** The gesture -> source table */
export interface OpSounds {
  op: 'sounds';
  tokens: Record<string, string> | null;
}

export type PatchOp =
  | OpInsert
  | OpSet
  | OpRemove
  | OpMove
  | OpRows
  | OpTheme
  | OpMetrics
  | OpMenu
  | OpTitle
  | OpKeys
  | OpResource
  | OpPlay
  | OpStop
  | OpSounds;

export type ServerMsg =
  | {
      t: 'mount';
      seq: number;
      root: NodeJson;
      sid?: string;
      theme?: Record<string, string>;
      metrics?: Record<string, number>;
      sounds?: Record<string, string> | null;
      loops?: string[];
      menu?: OpMenu['menu'];
      title?: string;
      keys?: KeySpec[];
      resources?: { images?: string[]; sounds?: string[] };
    }
  | { t: 'patch'; seq: number; ops: PatchOp[] }
  /** Resume ack: the client's tree is still current; nothing to rebuild. */
  | { t: 'resume'; seq: number };

export interface ClientEvent {
  t: 'ev';
  id: number;
  ev: string;
  value?: unknown;
  /** Last server seq the client had applied. Lets the server drop stale events. */
  seq: number;
}
