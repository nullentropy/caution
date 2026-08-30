import { Color, hex } from './color';
import { DisplayList } from './painter';
import type { TextEngine } from './text/engine';
import { GlRenderer } from './webgl/renderer';

export interface FrameEnv {
  width: number;
  height: number;
  dpr: number;
  frame: number;
}

/**
 * Damage-driven paint loop: nothing renders until invalidate() is called, and
 * multiple invalidations within a frame coalesce into one paint. There is no
 * continuous rAF loop so an idle UI costs nothing.
 */
export class Surface {
  readonly renderer: GlRenderer;
  background: Color = hex('#16181d');
  onBuild: ((dl: DisplayList, env: FrameEnv) => void) | null = null;
  /**
   * Caps animation-driven repaints (the browser mirror of the native
   * terminal's Options.MaxFPS. Set from ?fps=N). 0 = display rate. Damage
   * and input invalidations always paint immediately so only the
   * animation-continuation repaint is paced.
   */
  maxFps = 0;

  private dirty = false;
  /**
   * Whether the scheduled paint must rebuild the tree. Real invalidations
   * (input, ops, image loads, resize, timers) set it. The animation
   * continuation doesn't so those frames replay the renderer's retained
   * display list, repainting only the animated damage rects.
   */
  private rebuild = true;
  private frame = 0;
  /** single-flight pacer: damage paints during the wait must not stack chains */
  private animTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    readonly canvas: HTMLCanvasElement,
    engine?: TextEngine,
  ) {
    this.renderer = new GlRenderer(canvas, engine);
    this.renderer.images.onLoad = () => this.invalidate();
    new ResizeObserver(() => this.invalidate()).observe(canvas);
    // ResizeObserver misses pure-DPR changes (browser zoom, monitor move)
    window.addEventListener('resize', () => this.invalidate());
  }

  get frameCount(): number {
    return this.frame;
  }

  invalidate(): void {
    this.rebuild = true;
    this.schedule();
  }

  /** the animation continuation: schedules a paint without forcing a rebuild */
  private invalidateAnim(): void {
    this.schedule();
  }

  private schedule(): void {
    if (this.dirty) return;
    this.dirty = true;
    requestAnimationFrame(() => this.paint());
  }

  private paint(): void {
    this.dirty = false;
    const rebuild = this.rebuild;
    this.rebuild = false;
    this.frame++;

    ((window as any).__caution ??= { frames: 0, mounted: false }).frames = this.frame;
    const t0 = performance.now();
    let wantsAnimation: boolean;
    if (!rebuild && this.renderer.renderPartial(this.background, t0 / 1000)) {
      // pure animation continuation: the renderer replayed only its retained
      // list's damage rects, no tree walk, no display-list build. The
      // partials counter is diagnostic (headless probes assert on it).
      const c = (window as any).__caution;
      c.partials = (c.partials ?? 0) + 1;
      wantsAnimation = this.renderer.retainedWantsAnimation();
    } else {
      const dl = new DisplayList();
      this.onBuild?.(dl, {
        width: this.canvas.clientWidth,
        height: this.canvas.clientHeight,
        dpr: window.devicePixelRatio || 1,
        frame: this.frame,
      });
      const before = this.renderer.frames.damage;
      this.renderer.render(dl, this.background, performance.now() / 1000);
      // a real invalidation that only repainted its own damage: same
      // diagnostic role as partials, and the probes assert on it
      if (this.renderer.frames.damage !== before) {
        const c = (window as any).__caution;
        c.damaged = (c.damaged ?? 0) + 1;
      }
      wantsAnimation = dl.wantsAnimation;
    }
    // animated shaders opt out of damage-driven idle: keep painting while any are on screen
    // the moment none are, the surface goes quiet again
    if (wantsAnimation) {
      if (this.maxFps > 0) {
        if (this.animTimer == null) {
          const wait = Math.max(0, 1000 / this.maxFps - (performance.now() - t0));
          this.animTimer = setTimeout(() => {
            this.animTimer = null;
            this.invalidateAnim();
          }, wait);
        }
      } else {
        this.invalidateAnim();
      }
    }
  }
}
