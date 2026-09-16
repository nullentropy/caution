import type { WasmTerm } from './wasm';

type Role = 'text' | 'button' | 'checkbox' | 'textbox' | 'grid' | 'image';

interface SemRow {
  index: number;
  label: string;
  selected: boolean;
  bounds: [number, number, number, number];
}

interface SemNode {
  id: number;
  role: Role;
  label: string;
  checked?: boolean;
  value?: string;
  rowCount?: number;
  focused?: boolean;
  bounds: [number, number, number, number];
  rows?: SemRow[];
}

const INVISIBLE: Partial<CSSStyleDeclaration> = {
  position: 'absolute',
  opacity: '0',
  margin: '0',
  padding: '0',
  border: '0',
  background: 'none',
  pointerEvents: 'none',
  overflow: 'hidden',
};

export class SemanticsMirror {
  private host: HTMLDivElement;
  private els = new Map<number, HTMLElement>();
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private canvas: HTMLCanvasElement,
    private term: WasmTerm,
    private funnelActive: () => boolean,
  ) {
    this.host = document.createElement('div');
    Object.assign(this.host.style, {
      position: 'fixed',
      left: '0px',
      top: '0px',
      width: '0px',
      height: '0px',
      overflow: 'visible',
      pointerEvents: 'none',
    });
    document.body.appendChild(this.host);
  }

  // coalesce updates because assistive tech doesn't need 60fps
  schedule(): void {
    if (this.timer != null) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.sync();
    }, 80);
  }

  private sync(): void {
    const origin = this.canvas.getBoundingClientRect();
    this.host.style.left = `${origin.left}px`;
    this.host.style.top = `${origin.top}px`;
    const json = this.term.semantics();
    if (!json) return;
    const nodes = JSON.parse(json) as SemNode[];
    const live = new Set<HTMLElement>();
    let focus: HTMLElement | null = null;
    for (const n of nodes) {
      const el = this.ensure(n);
      live.add(el);
      // the funnel textarea keeps DOM focus while a text editor is focused
      if (n.focused && n.role !== 'textbox' && !this.funnelActive()) focus = el;
    }
    // prune elements whose widgets left the tree. new elements were appended
    // in walk order so existing ones stay put and DOM focus survives
    for (const child of Array.from(this.host.children)) {
      if (!live.has(child as HTMLElement)) {
        child.remove();
        for (const [id, el] of this.els) if (el === child) this.els.delete(id);
      }
    }
    if (focus && document.activeElement !== focus) focus.focus({ preventScroll: true });
  }

  private ensure(n: SemNode): HTMLElement {
    let el = this.els.get(n.id);
    if (el && el.dataset.role !== n.role) {
      el.remove();
      el = undefined;
    }
    if (!el) {
      el = this.create(n.id, n.role);
      el.dataset.role = n.role;
      this.els.set(n.id, el);
    }
    this.apply(el, n);
    if (!el.isConnected) this.host.appendChild(el);
    return el;
  }

  private create(id: number, role: Role): HTMLElement {
    const el = role === 'button' ? document.createElement('button') : document.createElement('div');
    Object.assign(el.style, INVISIBLE);
    switch (role) {
      case 'checkbox':
        el.setAttribute('role', 'checkbox');
        el.tabIndex = 0;
        break;
      case 'textbox':
        el.setAttribute('role', 'textbox');
        el.tabIndex = 0;
        break;
      case 'grid':
        el.setAttribute('role', 'grid');
        break;
      case 'image':
        el.setAttribute('role', 'img');
        break;
      case 'button':
      case 'text':
        break;
    }
    if (role !== 'text' && role !== 'grid' && role !== 'image') {
      // AT activation dispatches click directly at the element, bypassing pointer-events
      el.addEventListener('click', () => this.term.activate(id, -1));
    }
    return el;
  }

  private apply(el: HTMLElement, n: SemNode): void {
    const [x, y, w, h] = n.bounds;
    el.style.left = `${x}px`;
    el.style.top = `${y}px`;
    el.style.width = `${Math.max(1, w)}px`;
    el.style.height = `${Math.max(1, h)}px`;

    switch (n.role) {
      case 'text':
      case 'button':
        if (el.textContent !== n.label) el.textContent = n.label;
        break;
      case 'checkbox':
        el.setAttribute('aria-label', n.label);
        el.setAttribute('aria-checked', String(n.checked ?? false));
        break;
      case 'textbox':
        el.setAttribute('aria-label', n.label);
        if (el.textContent !== (n.value ?? '')) el.textContent = n.value ?? '';
        break;
      case 'image':
        el.setAttribute('aria-label', n.label);
        break;
      case 'grid': {
        el.setAttribute('aria-label', n.label);
        el.setAttribute('aria-rowcount', String(n.rowCount ?? 0));
        const rows: HTMLElement[] = [];
        for (const r of n.rows ?? []) {
          const row = document.createElement('div');
          Object.assign(row.style, INVISIBLE);
          row.setAttribute('role', 'row');
          row.setAttribute('aria-rowindex', String(r.index + 1));
          row.setAttribute('aria-selected', String(r.selected));
          row.style.left = `${r.bounds[0] - x}px`;
          row.style.top = `${r.bounds[1]}px`;
          row.style.width = `${Math.max(1, r.bounds[2])}px`;
          row.style.height = `${Math.max(1, r.bounds[3])}px`;
          row.textContent = r.label;
          row.addEventListener('click', () => this.term.activate(n.id, r.index));
          rows.push(row);
        }
        el.replaceChildren(...rows);
        break;
      }
    }
  }
}
