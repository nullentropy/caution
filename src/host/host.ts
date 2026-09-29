import { SoundStore } from '../audio';
import { Funnel } from './funnel';
import { attachInput } from './input';
import { SemanticsMirror } from './semantics';
import { FRAME_DAMAGE, FRAME_PARTIAL, HostCallbacks, WasmTerm } from './wasm';

const CURSORS: Record<string, string> = {
  '': 'default',
  pointer: 'pointer',
  text: 'text',
  'col-resize': 'col-resize',
  'row-resize': 'row-resize',
};

export class Host {
  /** caps animation-driven repaints, the browser mirror of the native -fps flag. 0 = display rate */
  maxFps = 0;

  private funnel: Funnel;
  private semantics: SemanticsMirror;
  private scheduled = false;
  private frames = 0;
  private tickTimer: ReturnType<typeof setTimeout> | null = null;
  private animTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private canvas: HTMLCanvasElement,
    private term: WasmTerm,
  ) {
    this.funnel = new Funnel(canvas, term);
    this.semantics = new SemanticsMirror(canvas, term, () => this.funnel.isActive);
    attachInput(canvas, term, {
      funnelActive: () => this.funnel.isActive,
      afterInput: () => this.funnel.sync(),
    });
    document.addEventListener('fullscreenchange', () => term.fullscreen(document.fullscreenElement != null));
    new ResizeObserver(() => this.invalidate()).observe(canvas);
    // ResizeObserver misses pure-DPR changes (browser zoom, monitor move)
    window.addEventListener('resize', () => this.invalidate());
    const fps = Number(new URLSearchParams(location.search).get('fps'));
    if (Number.isFinite(fps) && fps > 0) this.maxFps = fps;
    this.invalidate();
  }

  invalidate(): void {
    if (this.scheduled) return;
    this.scheduled = true;
    requestAnimationFrame(() => this.paint());
  }

  private paint(): void {
    this.scheduled = false;
    const canvas = this.canvas;
    const dpr = window.devicePixelRatio || 1;
    const lw = canvas.clientWidth;
    const lh = canvas.clientHeight;
    const dw = Math.max(1, Math.round(lw * dpr));
    const dh = Math.max(1, Math.round(lh * dpr));
    if (canvas.width !== dw || canvas.height !== dh) {
      canvas.width = dw;
      canvas.height = dh;
    }
    const t0 = performance.now();
    const [animating, kind] = this.term.paint(lw, lh, dw, dh, t0 / 1000);
    // the headless probes (cmd/goldens, cmd/loopprobe) read these
    const c = ((window as any).__caution ??= { frames: 0, mounted: false });
    c.frames = ++this.frames;
    if (kind === FRAME_DAMAGE) c.damaged = (c.damaged ?? 0) + 1;
    if (kind === FRAME_PARTIAL) c.partials = (c.partials ?? 0) + 1;
    this.scheduleTick();
    this.funnel.sync();
    this.semantics.schedule();
    if (!animating) return;
    // animated shaders keep painting while any are on screen. input and
    // patches still paint immediately, so only the continuation is paced
    if (this.maxFps > 0) {
      if (this.animTimer == null) {
        const wait = Math.max(0, 1000 / this.maxFps - (performance.now() - t0));
        this.animTimer = setTimeout(() => {
          this.animTimer = null;
          this.invalidate();
        }, wait);
      }
    } else {
      this.invalidate();
    }
  }

  // the terminal's time-driven repaints: caret blink, tooltip delay, theme tween
  private scheduleTick(): void {
    if (this.tickTimer != null) {
      clearTimeout(this.tickTimer);
      this.tickTimer = null;
    }
    const ms = this.term.tickIn();
    if (ms <= 0) return;
    this.tickTimer = setTimeout(() => {
      this.tickTimer = null;
      this.term.tick();
      this.invalidate();
    }, ms);
  }
}

/**
 * The callbacks the terminal needs from the moment it starts, before the Host
 * exists. Calls that arrive early are held until bind.
 */
export function hostCallbacks(canvas: HTMLCanvasElement): HostCallbacks & { bind(host: Host): void } {
  const gl = canvas.getContext('webgl2', {
    alpha: false,
    antialias: false, // the renderer does its own analytic AA
    depth: false,
    stencil: false,
    premultipliedAlpha: true,
  });
  if (!gl) throw new Error('WebGL2 unavailable');
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const sounds = new SoundStore();
  let host: Host | null = null;
  let pendingInvalidate = false;
  let lastCursor = '';
  return {
    gl,
    url: `${proto}://${location.host}/ws`,
    sid: sessionStorage.getItem('caution:sid'),
    bind(h) {
      host = h;
      if (pendingInvalidate) h.invalidate();
    },
    viewSize: () => [canvas.clientWidth, canvas.clientHeight],
    invalidate() {
      if (host) host.invalidate();
      else pendingInvalidate = true;
    },
    setCursor(name) {
      if (name === lastCursor) return;
      lastCursor = name;
      canvas.style.cursor = CURSORS[name] ?? 'default';
    },
    setTitle(title) {
      document.title = title;
    },
    writeClipboard(text) {
      void navigator.clipboard.writeText(text).catch(() => {});
    },
    preloadSound: (src) => sounds.preload(src),
    play: (src, loop) => sounds.play(src, loop),
    stop: (src) => sounds.stop(src || undefined),
    mounted(sid) {
      if (sid) sessionStorage.setItem('caution:sid', sid);
      // headless capture (go/cmd/goldens) waits for this before screenshotting
      ((window as any).__caution ??= { frames: 0, mounted: false }).mounted = true;
    },
  };
}
