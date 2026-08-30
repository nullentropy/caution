// The accessibility semantics layer (see README.md).
// A WebGL canvas is invisible to screen readers, so we mirror the widget
// tree's *meaning* into a hidden DOM overlay.
// Assistive tech reads and activates those. Activation routes back into the widget system,
// and widget focus drives DOM focus so screen readers announce changes.
// This is framework-internal DOM and apps don't see it.

import type { Rect } from '../gfx/geom';
import type { Ui } from './ui';
import type { Widget } from './widget';

export interface SemanticChild {
  role: 'row';
  label: string;
  posInSet: number;
  setSize: number;
  selected: boolean;
  bounds: Rect;
  onActivate: () => void;
}

export interface SemanticSpec {
  role: 'text' | 'button' | 'checkbox' | 'textbox' | 'grid' | 'image';
  label: string;
  checked?: boolean;
  value?: string;
  rowCount?: number;
  children?: SemanticChild[];
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

export class SemanticsLayer {
  private host: HTMLDivElement;
  private els = new WeakMap<Widget, HTMLElement>();
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(private ui: Ui) {
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

  // keep DOM focus on the focused widget's element so AT announces it
  focusWidget(w: Widget | null): void {
    if (!w) return;
    const el = this.els.get(w);
    if (el && el.isConnected && document.activeElement !== el) {
      el.focus({ preventScroll: true });
    }
  }

  private sync(): void {
    const root = this.ui.root;
    if (!root) {
      this.host.replaceChildren();
      return;
    }
    const origin = this.ui.canvas.getBoundingClientRect();
    this.host.style.left = `${origin.left}px`;
    this.host.style.top = `${origin.top}px`;

    const live = new Set<HTMLElement>();
    this.walk(root, live);
    // prune elements whose widgets left the tree. new elements were appended
    // in walk order so existing ones are left in place so DOM focus survives.
    for (const child of Array.from(this.host.children)) {
      if (!live.has(child as HTMLElement)) child.remove();
    }
  }

  private walk(w: Widget, live: Set<HTMLElement>): void {
    const spec = w.semantics();
    if (spec) live.add(this.ensure(w, spec));
    for (const c of w.children) this.walk(c, live);
  }

  private ensure(w: Widget, spec: SemanticSpec): HTMLElement {
    let el = this.els.get(w);
    if (el && el.dataset.role !== spec.role) {
      el.remove();
      el = undefined;
    }
    if (!el) {
      el = this.create(w, spec.role);
      el.dataset.role = spec.role;
      this.els.set(w, el);
    }
    this.apply(el, spec, w.bounds);
    if (!el.isConnected) this.host.appendChild(el);
    return el;
  }

  private create(w: Widget, role: SemanticSpec['role']): HTMLElement {
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
      // AT activation dispatches click directly at the element, bypassing
      // pointer-events
      el.addEventListener('click', () => {
        this.ui.setFocus(w, false);
        w.activate();
      });
    }
    return el;
  }

  private apply(el: HTMLElement, spec: SemanticSpec, b: Rect): void {
    el.style.left = `${b.x}px`;
    el.style.top = `${b.y}px`;
    el.style.width = `${Math.max(1, b.w)}px`;
    el.style.height = `${Math.max(1, b.h)}px`;

    switch (spec.role) {
      case 'text':
        if (el.textContent !== spec.label) el.textContent = spec.label;
        break;
      case 'button':
        if (el.textContent !== spec.label) el.textContent = spec.label;
        break;
      case 'checkbox':
        el.setAttribute('aria-label', spec.label);
        el.setAttribute('aria-checked', String(spec.checked ?? false));
        break;
      case 'textbox':
        el.setAttribute('aria-label', spec.label);
        if (el.textContent !== (spec.value ?? '')) el.textContent = spec.value ?? '';
        break;
      case 'image':
        el.setAttribute('aria-label', spec.label);
        break;
      case 'grid': {
        el.setAttribute('aria-label', spec.label);
        el.setAttribute('aria-rowcount', String(spec.rowCount ?? 0));
        const rows: HTMLElement[] = [];
        for (const child of spec.children ?? []) {
          const row = document.createElement('div');
          Object.assign(row.style, INVISIBLE);
          row.setAttribute('role', 'row');
          row.setAttribute('aria-rowindex', String(child.posInSet));
          row.setAttribute('aria-selected', String(child.selected));
          row.style.left = `${child.bounds.x - el.offsetLeft}px`;
          row.style.top = `${child.bounds.y}px`;
          row.style.width = `${Math.max(1, child.bounds.w)}px`;
          row.style.height = `${Math.max(1, child.bounds.h)}px`;
          row.textContent = child.label;
          row.addEventListener('click', child.onActivate);
          rows.push(row);
        }
        el.replaceChildren(...rows);
        break;
      }
    }
  }
}
