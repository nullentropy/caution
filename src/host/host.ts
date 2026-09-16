import { SoundStore } from '../audio';
import type { DisplayList } from '../gfx/painter';
import { FrameEnv, Surface } from '../gfx/surface';
import { WasmEngine } from '../gfx/text/wasmengine';
import { decodeFrame } from './frame';
import { Funnel } from './funnel';
import { attachInput } from './input';
import { SemanticsMirror } from './semantics';
import type { HostCallbacks, Terminal, WasmTerm } from './wasm';

const CURSORS: Record<string, string> = {
  '': 'default',
  pointer: 'pointer',
  text: 'text',
  'col-resize': 'col-resize',
  'row-resize': 'row-resize',
};

export class Host {
  readonly surface: Surface;
  private term: WasmTerm;
  private funnel: Funnel;
  private semantics: SemanticsMirror;
  private buf = new Uint8Array(1 << 16);
  private tickTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(canvas: HTMLCanvasElement, wasm: Terminal) {
    this.term = wasm.term;
    this.surface = new Surface(canvas, new WasmEngine(wasm.text));
    this.funnel = new Funnel(canvas, this.term);
    this.semantics = new SemanticsMirror(canvas, this.term, () => this.funnel.isActive);
    this.surface.onBuild = (dl, env) => this.build(dl, env);
    attachInput(canvas, this.term, {
      funnelActive: () => this.funnel.isActive,
      afterInput: () => this.funnel.sync(),
    });
    document.addEventListener('fullscreenchange', () => this.term.fullscreen(document.fullscreenElement != null));
    // ?fps=N caps animation repaints, the way -fps does for the native terminal
    const fps = Number(new URLSearchParams(location.search).get('fps'));
    if (Number.isFinite(fps) && fps > 0) this.surface.maxFps = fps;
    this.surface.invalidate();
  }

  private build(dl: DisplayList, env: FrameEnv): void {
    const n = this.term.frame(env.width, env.height, env.dpr);
    if (n > this.buf.length) this.buf = new Uint8Array(Math.max(n, this.buf.length * 2));
    this.term.take(this.buf);
    this.surface.background = decodeFrame(this.buf, n, dl);
    this.scheduleTick();
    this.funnel.sync();
    this.semantics.schedule();
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
      this.surface.invalidate();
    }, ms);
  }
}

export function hostCallbacks(canvas: HTMLCanvasElement): HostCallbacks & { bind(host: Host): void } {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const sounds = new SoundStore();
  let host: Host | null = null;
  let pendingInvalidate = false;
  let lastCursor = '';
  return {
    url: `${proto}://${location.host}/ws`,
    sid: sessionStorage.getItem('caution:sid'),
    bind(h) {
      host = h;
      if (pendingInvalidate) h.surface.invalidate();
    },
    viewSize: () => [canvas.clientWidth, canvas.clientHeight],
    invalidate() {
      if (host) host.surface.invalidate();
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
    preloadImage(src) {
      host?.surface.renderer.images.get(src);
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
