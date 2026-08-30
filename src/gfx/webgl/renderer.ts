import { Color, rgba, WHITE } from '../color';
import { diffDamage, paintedBounds, planDamage, type DamagePlan } from '../damage';
import type { Font } from '../font';
import { overlaps, rect, type Rect, type Corners } from '../geom';
import type { Cmd, DisplayList, ImageCmd, LayerEndCmd, RectCmd, ShadowCmd, ShaderQuadCmd, TextCmd } from '../painter';
import { ATLAS_SIZE, PHASES } from '../text/atlas';
import { CanvasEngine, TextEngine } from '../text/engine';
import type { TextRun } from '../text/shaper';
import { ImageStore } from './images';
import { FRAG, PASSTHROUGH_FRAG, QUAD_VERT, VERT, wrapUserFrag } from './shaders';

const FLOATS_PER_INSTANCE = 28; // 7 × vec4
const NO_CORNERS: Corners = { tl: 0, tr: 0, br: 0, bl: 0 };

const MODE_RECT = 0;
const MODE_SHADOW = 1;
const MODE_TEXTURED = 2;

/** aspect-fit an image's quad and uv into bounds */
function fitImage(
  b: Rect,
  iw: number,
  ih: number,
  fit: 'contain' | 'cover' | 'fill',
): { quad: Rect; u0: number; v0: number; u1: number; v1: number } {
  if (fit === 'contain' && iw > 0 && ih > 0) {
    const s = Math.min(b.w / iw, b.h / ih);
    const w = iw * s;
    const h = ih * s;
    return { quad: rect(b.x + (b.w - w) / 2, b.y + (b.h - h) / 2, w, h), u0: 0, v0: 0, u1: 1, v1: 1 };
  }
  if (fit === 'cover' && iw > 0 && ih > 0) {
    const s = Math.max(b.w / iw, b.h / ih);
    const uw = b.w / (iw * s);
    const vh = b.h / (ih * s);
    const u0 = (1 - uw) / 2;
    const v0 = (1 - vh) / 2;
    return { quad: b, u0, v0, u1: u0 + uw, v1: v0 + vh };
  }
  return { quad: b, u0: 0, v0: 0, u1: 1, v1: 1 }; // fill / degenerate
}

function compile(gl: WebGL2RenderingContext, type: number, src: string): WebGLShader {
  const sh = gl.createShader(type);
  if (!sh) throw new Error('createShader failed');
  gl.shaderSource(sh, src);
  gl.compileShader(sh);
  if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
    throw new Error(`shader compile failed: ${gl.getShaderInfoLog(sh)}`);
  }
  return sh;
}

/** a render destination: the canvas, or a pooled offscreen layer */
interface Target {
  fbo: WebGLFramebuffer | null;
  originX: number;
  originY: number;
  viewW: number;
  viewH: number;
  devW: number;
  devH: number;
  flipY: number;
  pooled: PooledLayer | null;
}

interface PooledLayer {
  fbo: WebGLFramebuffer;
  tex: WebGLTexture;
  key: string;
}

interface UserProgram {
  prog: WebGLProgram;
  locs: Map<string, WebGLUniformLocation | null>;
  used: number; // last-access tick, for LRU eviction (see userProgram)
}

/**
 * Bounds the compiled-shader cache. Apps that swap a shader's frag through
 * many distinct sources (e.g. per-state data baked into const arrays,
 * recompiled as the state changes) would otherwise grow it without limit.
 */
const MAX_USER_PROGS = 48;

/**
 * What one render did, mirroring the native terminal's render.FrameResult.
 * `painted` false means the damage diff came out empty: no scissored pass ran
 * and the scene framebuffer still holds the previous frame's image. Unlike the
 * native shell, this one cannot skip presentation on it. The canvas is created
 * without preserveDrawingBuffer, so its drawing buffer is undefined once the
 * browser has composited, and the scene has to be blitted onto it every frame
 * or the canvas shows garbage. The blit is unconditional here and conditional
 * natively.
 * asymmetry between the two terminals rather than an oversight.
 */
export interface FrameResult {
  painted: boolean;
  rects: number;
  full: boolean;
}

/**
 * WebGL2 display-list backend. Every command becomes one instance of a unit
 * quad; because clips are per-instance and the only texture is the glyph
 * atlas, an entire frame is a single instanced draw call.
 */
const IMG_PLACEHOLDER = rgba(0.5, 0.55, 0.6, 0.08);
const IMG_ERROR = rgba(0.9, 0.3, 0.3, 0.12);

export class GlRenderer {
  /** the text engine: Canvas2D fallback, or the shared wasm engine (see terminal.ts) */
  readonly engine: TextEngine;
  readonly images: ImageStore;

  private gl: WebGL2RenderingContext;
  private prog: WebGLProgram;
  private vao: WebGLVertexArrayObject;
  private quadVao: WebGLVertexArrayObject;
  private instBuf: WebGLBuffer;
  private tex: WebGLTexture;
  private uView: WebGLUniformLocation;
  private uOrigin: WebGLUniformLocation;
  private uFlipY: WebGLUniformLocation;
  private data = new Float32Array(4096 * FLOATS_PER_INSTANCE);
  private count = 0;
  private lastDpr = 0;
  private time = 0;
  private layerPool = new Map<string, PooledLayer[]>();
  private userProgs = new Map<string, UserProgram | null>();
  private progTick = 0; // monotonic access clock for userProgs LRU
  // logical size the pooled layers were sized for; a resize orphans them
  private lastViewW = 0;
  private lastViewH = 0;

  // partial invalidation (see renderPartial)
  // Every frame renders into the scene (a persistent offscreen framebuffer)
  // and blits to the canvas. The scene keeping last frame's pixels is what
  // makes scissored partial repaints possible because the canvas's own
  // drawing buffer is not preserved across frames
  private scene: { fbo: WebGLFramebuffer; tex: WebGLTexture; devW: number; devH: number; viewW: number; viewH: number } | null = null;
  // The last fully rendered display list, its damage plan, and the
  // frame-invariant layer textures retained from that render (keyed by
  // layer-begin index; valid as long as `retained` is).
  private retained: DisplayList | null = null;
  private plan: DamagePlan = { rects: [], layers: new Map() };
  private layerCache = new Map<number, Target>();
  private sceneScissor: [number, number, number, number] | null = null;
  /**
   * The color the scene was last cleared with. A background change repaints
   * everything, and nothing in the display list records it.
   */
  private lastBg: { r: number; g: number; b: number } | null = null;
  /**
   * How each kind of frame got serviced. The only way to tell from outside
   * which path a frame took. The pixel-identity probes assert on it. `damage`
   * repainted only what differs from the retained list. `partial` replayed the
   * retained list's animation damage without any tree walk.
   */
  readonly frames = { full: 0, damage: 0, partial: 0 };

  constructor(
    private canvas: HTMLCanvasElement,
    engine?: TextEngine,
  ) {
    this.engine = engine ?? new CanvasEngine();
    const gl = canvas.getContext('webgl2', {
      alpha: false,
      antialias: false, // we do our own analytic AA
      depth: false,
      stencil: false,
      premultipliedAlpha: true,
    });
    if (!gl) throw new Error('WebGL2 unavailable');
    this.gl = gl;
    this.images = new ImageStore(gl);

    const prog = gl.createProgram();
    if (!prog) throw new Error('createProgram failed');
    gl.attachShader(prog, compile(gl, gl.VERTEX_SHADER, VERT));
    gl.attachShader(prog, compile(gl, gl.FRAGMENT_SHADER, FRAG));
    gl.linkProgram(prog);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      throw new Error(`program link failed: ${gl.getProgramInfoLog(prog)}`);
    }
    this.prog = prog;
    const uView = gl.getUniformLocation(prog, 'u_view');
    const uOrigin = gl.getUniformLocation(prog, 'u_origin');
    const uFlipY = gl.getUniformLocation(prog, 'u_flipY');
    if (!uView || !uOrigin || !uFlipY) throw new Error('batch uniforms missing');
    this.uView = uView;
    this.uOrigin = uOrigin;
    this.uFlipY = uFlipY;

    const vao = gl.createVertexArray();
    if (!vao) throw new Error('createVertexArray failed');
    this.vao = vao;
    gl.bindVertexArray(vao);

    const quad = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, quad);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([0, 0, 1, 0, 0, 1, 1, 1]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);

    const instBuf = gl.createBuffer();
    if (!instBuf) throw new Error('createBuffer failed');
    this.instBuf = instBuf;
    gl.bindBuffer(gl.ARRAY_BUFFER, instBuf);
    for (let i = 0; i < 7; i++) {
      gl.enableVertexAttribArray(1 + i);
      gl.vertexAttribPointer(1 + i, 4, gl.FLOAT, false, FLOATS_PER_INSTANCE * 4, i * 16);
      gl.vertexAttribDivisor(1 + i, 1);
    }
    gl.bindVertexArray(null);

    // plain unit-quad VAO for custom-shader passes (no instancing)
    const quadVao = gl.createVertexArray();
    if (!quadVao) throw new Error('createVertexArray failed');
    this.quadVao = quadVao;
    gl.bindVertexArray(quadVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, quad);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
    gl.bindVertexArray(null);

    const tex = gl.createTexture();
    if (!tex) throw new Error('createTexture failed');
    this.tex = tex;
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, ATLAS_SIZE, ATLAS_SIZE, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);

    gl.disable(gl.DEPTH_TEST);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA); // premultiplied

    gl.useProgram(prog);
    gl.uniform1i(gl.getUniformLocation(prog, 'u_tex'), 0);
  }

  measure(f: Font, text: string): TextRun {
    return this.engine.shape(f, window.devicePixelRatio || 1, text);
  }

  /**
   * Paint a freshly built display list into the persistent scene framebuffer
   * and blit it to the canvas. Where the new list provably differs from the
   * retained one in only a few places (a server op touching one label, a
   * hover moving, a caret flipping) only those regions are repainted
   * (diffDamage), and the rest of the scene keeps the pixels it already has,
   * otherwise every pixel is rebuilt. Either way the list is retained with its
   * damage plan and its frame-invariant layer textures, so pure animation
   * continuations can go through renderPartial next.
   */
  render(dl: DisplayList, background: Color, time = 0): FrameResult {
    const { gl, canvas } = this;
    const dpr = window.devicePixelRatio || 1;
    this.time = time;
    const dprChanged = dpr !== this.lastDpr;
    if (dprChanged) {
      // glyphs are rasterized at device-pixel size
      // a DPR change invalidates them all and the pooled layer textures too
      if (this.lastDpr !== 0) {
        this.engine.reset();
        this.flushLayerCache();
        this.clearLayerPool();
      }
      this.lastDpr = dpr;
    }

    const lw = canvas.clientWidth;
    const lh = canvas.clientHeight;
    const dw = Math.max(1, Math.round(lw * dpr));
    const dh = Math.max(1, Math.round(lh * dpr));
    const sizeChanged = canvas.width !== dw || canvas.height !== dh;
    if (sizeChanged) {
      canvas.width = dw;
      canvas.height = dh;
    }
    // a resize (same DPR, new logical size) orphans every pooled layer
    // texture at the old size
    if (lw !== this.lastViewW || lh !== this.lastViewH) {
      if (this.lastViewW !== 0) {
        this.flushLayerCache();
        this.clearLayerPool();
      }
      this.lastViewW = lw;
      this.lastViewH = lh;
    }

    const view = rect(0, 0, lw, lh);
    const plan = planDamage(dl, view);
    // everything the display list cannot tell us has to be ruled out before
    // trusting a diff: the scene must still be the same surface, the glyphs
    // must still live where they lived, no image can have finished loading
    // (same command, different texture), and the clear color must be the one
    // the untouched pixels were cleared with
    const s = this.scene;
    const imagesChanged = this.images.takeChanged();
    const canDiff =
      !dprChanged && !sizeChanged && !imagesChanged &&
      !!s && s.devW === dw && s.devH === dh && s.viewW === lw && s.viewH === lh &&
      !!this.lastBg && this.lastBg.r === background.r && this.lastBg.g === background.g &&
      this.lastBg.b === background.b &&
      this.retained?.palette === dl.palette;
    const damage = canDiff ? diffDamage(this.retained, dl, view, plan.rects) : null;

    this.flushLayerCache(); // cached textures belong to the outgoing retained list
    this.ensureScene(dw, dh, lw, lh);
    this.retained = dl;
    this.plan = plan;
    this.lastBg = { r: background.r, g: background.g, b: background.b };
    this.sceneScissor = null;
    this.count = 0;

    if (damage) {
      this.frames.damage++;
      for (const d of damage) this.partialPass(dl.cmds, d, background, dpr);
      this.sceneScissor = null;
      this.blit(); // unconditional: see FrameResult
      return { painted: damage.length > 0, rects: damage.length, full: false };
    }

    this.frames.full++;
    const scene = this.sceneTarget();
    const targets: Target[] = [scene];
    this.bindTarget(scene);
    gl.clearColor(background.r, background.g, background.b, 1);
    gl.clear(gl.COLOR_BUFFER_BIT);
    this.execRange(dl.cmds, 0, dl.cmds.length, targets, dpr);
    this.flushBatch(targets[targets.length - 1]!);
    this.blit();
    return { painted: true, rects: 0, full: true };
  }

  /**
   * Run cmds[start, end) sequentially against the target stack (the
   * full-render body, also reused by partial passes for layer ranges that
   * must re-render live). A finished cacheable layer keeps its texture for
   * later partial replay instead of returning to the pool. Layers render
   * unscissored whichever path got here, so the texture is as good either way.
   */
  private execRange(cmds: readonly Cmd[], start: number, end: number, targets: Target[], dpr: number): void {
    const gl = this.gl;
    const beginIdxs: number[] = [];
    for (let i = start; i < end; i++) {
      const cmd = cmds[i]!;
      switch (cmd.kind) {
        case 'rect':
          this.emitRect(cmd);
          break;
        case 'shadow':
          this.emitShadow(cmd);
          break;
        case 'text':
          this.emitText(cmd, dpr);
          break;
        case 'layer-begin': {
          this.flushBatch(targets[targets.length - 1]!);
          const t = this.acquireLayer(cmd.rect, dpr);
          targets.push(t);
          beginIdxs.push(i);
          this.bindTarget(t);
          gl.clearColor(0, 0, 0, 0);
          gl.clear(gl.COLOR_BUFFER_BIT);
          break;
        }
        case 'layer-end': {
          const layer = targets[targets.length - 1]!;
          this.flushBatch(layer);
          targets.pop();
          const begin = beginIdxs.pop() ?? -1;
          const parent = targets[targets.length - 1]!;
          this.bindTarget(parent);
          this.drawComposite(cmd, layer, parent);
          if (layer.pooled) {
            if (begin >= 0 && this.plan.layers.get(begin)?.cacheable) {
              this.layerCache.set(begin, layer);
            } else {
              this.releaseLayer(layer.pooled);
            }
          }
          break;
        }
        case 'shader-quad': {
          const t = targets[targets.length - 1]!;
          this.flushBatch(t);
          this.drawShaderQuad(cmd, t);
          break;
        }
        case 'image':
          this.drawImage(cmd, targets[targets.length - 1]!);
          break;
      }
    }
  }

  /**
   * Repaint only the retained list's animated damage rects into the scene
   * framebuffer and blit, the pure animation-continuation frame: no tree
   * walk, no display-list build, no touching of clean pixels. Returns false
   * when a full render is required instead: nothing retained, no animated
   * damage, geometric animations mid-flight (they move bounds every frame),
   * or the canvas's size/DPR changed under the retained list.
   */
  renderPartial(background: Color, time: number): boolean {
    const { canvas, retained: dl, scene: s } = this;
    if (!dl || !s || !this.plan.rects.length || dl.geomAnimation) return false;
    const dpr = window.devicePixelRatio || 1;
    if (dpr !== this.lastDpr) return false;
    if (s.viewW !== canvas.clientWidth || s.viewH !== canvas.clientHeight) return false;
    if (s.devW !== canvas.width || s.devH !== canvas.height) return false;
    this.frames.partial++;
    this.time = time;
    this.count = 0;
    for (const d of this.plan.rects) {
      this.partialPass(dl.cmds, d, background, dpr);
    }
    this.sceneScissor = null;
    this.blit();
    return true;
  }

  /** whether the retained list keeps asking for animation frames */
  retainedWantsAnimation(): boolean {
    return this.retained?.wantsAnimation ?? false;
  }

  /**
   * Redraws a single damage rect.
   *
   * The scissor is set to the damage rect (snapped outward to whole pixels),
   * and only commands whose painted bounds intersect that rect are replayed.
   * We clear to the background color inside the scissor, then draw those
   * commands in the same painter's-algorithm order a full frame would use, so
   * the pixels inside the rect end up identical to a full redraw. Pixels
   * outside the rect are left as-is in the scene framebuffer.
   *
   * The command list comes from one of two places: the retained list when
   * we're continuing an animation, or a newly built list for a real damage
   * frame.
   *
   * Layers are an exception to the scissoring: the scissor doesn't apply
   * inside a layer's render target, so any layer reached here is rendered in
   * full. That means a frame-invariant layer is cached as it would be
   * during a full frame, and the next pass can composite the cached texture
   * instead of re-rendering the subtree.
   */
  private partialPass(cmds: readonly Cmd[], d: Rect, background: Color, dpr: number): void {
    const gl = this.gl;
    const scene = this.sceneTarget();
    const x0 = Math.max(0, Math.floor(d.x * dpr));
    const y0 = Math.max(0, Math.floor(d.y * dpr));
    const x1 = Math.min(scene.devW, Math.ceil((d.x + d.w) * dpr));
    const y1 = Math.min(scene.devH, Math.ceil((d.y + d.h) * dpr));
    if (x1 <= x0 || y1 <= y0) return;
    // GL scissor is bottom-origin; the scene renders canvas-oriented
    this.sceneScissor = [x0, scene.devH - y1, x1 - x0, y1 - y0];
    const targets: Target[] = [scene];
    this.bindTarget(scene);
    gl.clearColor(background.r, background.g, background.b, 1);
    gl.clear(gl.COLOR_BUFFER_BIT);
    for (let i = 0; i < cmds.length; i++) {
      const cmd = cmds[i]!;
      if (cmd.kind === 'layer-begin') {
        const span = this.plan.layers.get(i);
        if (!span) continue; // unbalanced list; paintTree can't produce this
        if (!overlaps(cmd.rect, d)) {
          i = span.end; // the whole subtree misses this pass
          continue;
        }
        const cached = span.cacheable ? this.layerCache.get(i) : undefined;
        if (cached) {
          // frame-invariant content: skip straight to compositing the
          // retained texture (fresh u_time still applies)
          //
          // flush first: the batch holds commands from *below* the composite.
          this.flushBatch(scene);
          this.drawComposite(cmds[span.end] as LayerEndCmd, cached, scene);
        } else {
          // contains animation: re-render the range live. bindTarget drops
          // the scissor inside layer targets and re-applies it when the
          // composite lands back on the scene.
          this.execRange(cmds, i, span.end + 1, targets, dpr);
        }
        i = span.end;
        continue;
      }
      if (cmd.kind === 'layer-end') continue; // consumed by the ranges above
      if (!overlaps(paintedBounds(cmd), d)) continue;
      switch (cmd.kind) {
        case 'rect':
          this.emitRect(cmd);
          break;
        case 'shadow':
          this.emitShadow(cmd);
          break;
        case 'text':
          this.emitText(cmd, dpr);
          break;
        case 'shader-quad':
          this.flushBatch(scene);
          this.drawShaderQuad(cmd, scene);
          break;
        case 'image':
          this.drawImage(cmd, scene);
          break;
      }
    }
    this.flushBatch(scene);
  }

  /** (re)allocate the persistent scene framebuffer at the canvas device size */
  private ensureScene(devW: number, devH: number, viewW: number, viewH: number): void {
    const gl = this.gl;
    const s = this.scene;
    if (s && s.devW === devW && s.devH === devH) {
      s.viewW = viewW;
      s.viewH = viewH;
      return;
    }
    if (s) {
      gl.deleteFramebuffer(s.fbo);
      gl.deleteTexture(s.tex);
    }
    const tex = gl.createTexture();
    const fbo = gl.createFramebuffer();
    if (!tex || !fbo) throw new Error('scene allocation failed');
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, devW, devH, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
    this.scene = { fbo, tex, devW, devH, viewW, viewH };
  }

  /**
   * the scene renders canvas-oriented (flipY -1), so the blit needs no flip
   */
  private sceneTarget(): Target {
    const s = this.scene!;
    return {
      fbo: s.fbo,
      originX: 0,
      originY: 0,
      viewW: s.viewW,
      viewH: s.viewH,
      devW: s.devW,
      devH: s.devH,
      flipY: -1,
      pooled: null,
    };
  }

  /** copy the scene to the canvas. a whole-frame GPU copy, scissor-free */
  private blit(): void {
    const gl = this.gl;
    const s = this.scene!;
    gl.disable(gl.SCISSOR_TEST);
    gl.bindFramebuffer(gl.READ_FRAMEBUFFER, s.fbo);
    gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, null);
    gl.blitFramebuffer(0, 0, s.devW, s.devH, 0, 0, s.devW, s.devH, gl.COLOR_BUFFER_BIT, gl.NEAREST);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  }

  /**
   * return every retained layer texture to the pool (the retained list they
   * were rendered for is going away)
   */
  private flushLayerCache(): void {
    for (const t of this.layerCache.values()) {
      if (t.pooled) this.releaseLayer(t.pooled);
    }
    this.layerCache.clear();
  }

  private drawImage(cmd: ImageCmd, t: Target): void {
    if (cmd.rect.w <= 0 || cmd.rect.h <= 0 || cmd.clip.w <= 0 || cmd.clip.h <= 0) return;
    const img = this.images.get(cmd.src);
    if (img.state !== 'ready' || !img.tex) {
      const tint = img.state === 'error' ? IMG_ERROR : IMG_PLACEHOLDER;
      this.inst(cmd.rect, cmd.radii, tint, tint, 0, MODE_RECT, 0, 0, 0, 0, 0, cmd.clip);
      return;
    }
    // the image is its own texture, so it gets its own single-instance draw
    this.flushBatch(t);
    const f = fitImage(cmd.rect, img.w, img.h, cmd.fit);
    this.inst(f.quad, cmd.radii, WHITE, WHITE, 0, MODE_TEXTURED, 0, f.u0, f.v0, f.u1, f.v1, cmd.clip);
    this.flushBatch(t, img.tex);
  }

  /** draw the accumulated instance batch onto the given target */
  private flushBatch(t: Target, tex?: WebGLTexture): void {
    if (this.count === 0) return;
    const gl = this.gl;
    gl.bindTexture(gl.TEXTURE_2D, this.tex);
    this.engine.upload(gl);
    gl.useProgram(this.prog);
    gl.uniform2f(this.uView, t.viewW, t.viewH);
    gl.uniform2f(this.uOrigin, t.originX, t.originY);
    gl.uniform1f(this.uFlipY, t.flipY);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, tex ?? this.tex);
    gl.bindVertexArray(this.vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.instBuf);
    gl.bufferData(gl.ARRAY_BUFFER, this.data.subarray(0, this.count * FLOATS_PER_INSTANCE), gl.DYNAMIC_DRAW);
    gl.drawArraysInstanced(gl.TRIANGLE_STRIP, 0, 4, this.count);
    gl.bindVertexArray(null);
    this.count = 0;
  }

  /**
   * bindTarget also owns the scissor: a partial pass's scissor applies only
   * while drawing the scene itself. Offscreen layers render in full, then
   * their composite is clipped on the way back onto the scene.
   */
  private bindTarget(t: Target): void {
    const gl = this.gl;
    gl.bindFramebuffer(gl.FRAMEBUFFER, t.fbo);
    gl.viewport(0, 0, t.devW, t.devH);
    if (this.sceneScissor && this.scene && t.fbo === this.scene.fbo) {
      const [x, y, w, h] = this.sceneScissor;
      gl.enable(gl.SCISSOR_TEST);
      gl.scissor(x, y, w, h);
    } else {
      gl.disable(gl.SCISSOR_TEST);
    }
  }

  // -- offscreen layers -----------------------------------------------------------

  private acquireLayer(r: Rect, dpr: number): Target {
    const gl = this.gl;
    const devW = Math.max(1, Math.round(r.w * dpr));
    const devH = Math.max(1, Math.round(r.h * dpr));
    const key = `${devW}x${devH}`;
    let pooled = this.layerPool.get(key)?.pop() ?? null;
    if (!pooled) {
      const tex = gl.createTexture();
      const fbo = gl.createFramebuffer();
      if (!tex || !fbo) throw new Error('layer allocation failed');
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, devW, devH, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
      gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
      pooled = { fbo, tex, key };
    }
    return {
      fbo: pooled.fbo,
      originX: r.x,
      originY: r.y,
      viewW: r.w,
      viewH: r.h,
      devW,
      devH,
      flipY: 1, // texture row 0 = layer top, so effects sample uv directly
      pooled,
    };
  }

  private releaseLayer(p: PooledLayer): void {
    const list = this.layerPool.get(p.key) ?? [];
    list.push(p);
    this.layerPool.set(p.key, list);
  }

  private clearLayerPool(): void {
    const gl = this.gl;
    for (const list of this.layerPool.values()) {
      for (const p of list) {
        gl.deleteFramebuffer(p.fbo);
        gl.deleteTexture(p.tex);
      }
    }
    this.layerPool.clear();
  }

  // -- custom shader passes ----------------------------------------------------------

  private drawComposite(cmd: LayerEndCmd, layer: Target, parent: Target): void {
    const gl = this.gl;
    const up =
      this.userProgram(cmd.frag, Object.keys(cmd.uniforms), true) ??
      this.userProgram(PASSTHROUGH_FRAG, [], true);
    if (!up) return;
    gl.useProgram(up.prog);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, layer.pooled!.tex);
    this.setQuadUniforms(up, cmd.rect, [0, 0, 1, 1], parent);
    gl.uniform1i(up.locs.get('u_tex') ?? null, 0);
    gl.uniform2f(up.locs.get('u_res') ?? null, cmd.rect.w, cmd.rect.h);
    this.setCustomUniforms(up, cmd.uniforms);
    gl.bindVertexArray(this.quadVao);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    gl.bindVertexArray(null);
  }

  private drawShaderQuad(cmd: ShaderQuadCmd, t: Target): void {
    const gl = this.gl;
    const up = this.userProgram(cmd.frag, Object.keys(cmd.uniforms), false);
    if (!up) return; // compile error already logged; draw nothing
    gl.useProgram(up.prog);
    this.setQuadUniforms(up, cmd.rect, cmd.uv, t);
    gl.uniform2f(up.locs.get('u_res') ?? null, cmd.rect.w, cmd.rect.h);
    this.setCustomUniforms(up, cmd.uniforms);
    gl.bindVertexArray(this.quadVao);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    gl.bindVertexArray(null);
  }

  private setQuadUniforms(up: UserProgram, r: Rect, uv: [number, number, number, number], t: Target): void {
    const gl = this.gl;
    gl.uniform4f(up.locs.get('u_rect') ?? null, r.x, r.y, r.w, r.h);
    gl.uniform4f(up.locs.get('u_uvrect') ?? null, uv[0], uv[1], uv[2], uv[3]);
    gl.uniform2f(up.locs.get('u_view') ?? null, t.viewW, t.viewH);
    gl.uniform2f(up.locs.get('u_origin') ?? null, t.originX, t.originY);
    gl.uniform1f(up.locs.get('u_flipY') ?? null, t.flipY);
    gl.uniform1f(up.locs.get('u_time') ?? null, this.time);
  }

  private setCustomUniforms(up: UserProgram, uniforms: Record<string, number>): void {
    for (const [k, v] of Object.entries(uniforms)) {
      const loc = up.locs.get(k);
      if (loc) this.gl.uniform1f(loc, v);
    }
  }

  /** compile-and-cache an app-supplied fragment shader. null = compile error */
  private userProgram(frag: string, uniformNames: string[], withContent: boolean): UserProgram | null {
    const key = `${withContent ? 'fx' : 'px'}\0${uniformNames.slice().sort().join(',')}\0${frag}`;
    this.progTick++;
    const hit = this.userProgs.get(key);
    if (hit !== undefined) {
      if (hit) hit.used = this.progTick;
      return hit;
    }
    const gl = this.gl;
    // bound the cache: evict the least-recently-used entry (and free its GL
    // program) before adding a new one. null entries are compile-failure
    // markers. they count against the cap too. unique broken sources are
    // the same unbounded-growth class as unique good ones, and evict first
    // (treated as tick 0): losing one costs a single failed recompile. the
    // just-requested key isn't in the map yet, so it can never be evicted.
    if (this.userProgs.size >= MAX_USER_PROGS) {
      let oldestKey: string | null = null;
      let oldest = Infinity;
      for (const [k, p] of this.userProgs) {
        const t = p ? p.used : 0;
        if (t < oldest) {
          oldest = t;
          oldestKey = k;
        }
      }
      if (oldestKey !== null) {
        const old = this.userProgs.get(oldestKey);
        if (old) gl.deleteProgram(old.prog);
        this.userProgs.delete(oldestKey);
      }
    }
    let up: UserProgram | null = null;
    try {
      const prog = gl.createProgram();
      if (!prog) throw new Error('createProgram failed');
      gl.attachShader(prog, compile(gl, gl.VERTEX_SHADER, QUAD_VERT));
      gl.attachShader(prog, compile(gl, gl.FRAGMENT_SHADER, wrapUserFrag(frag, uniformNames, withContent)));
      gl.linkProgram(prog);
      if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
        throw new Error(`link failed: ${gl.getProgramInfoLog(prog)}`);
      }
      const locs = new Map<string, WebGLUniformLocation | null>();
      for (const n of ['u_rect', 'u_uvrect', 'u_view', 'u_origin', 'u_flipY', 'u_res', 'u_time', 'u_tex', ...uniformNames]) {
        locs.set(n, gl.getUniformLocation(prog, n));
      }
      up = { prog, locs, used: this.progTick };
    } catch (err) {
      console.warn('caution: custom shader failed to compile:', err);
      up = null;
    }
    this.userProgs.set(key, up);
    return up;
  }

  private emitRect(cmd: RectCmd): void {
    if (cmd.rect.w <= 0 || cmd.rect.h <= 0 || cmd.clip.w <= 0 || cmd.clip.h <= 0) return;
    this.inst(
      cmd.rect, cmd.radii, cmd.color, cmd.borderColor,
      cmd.borderWidth, MODE_RECT, 0,
      0, 0, 0, 0, cmd.clip,
    );
  }

  private emitShadow(cmd: ShadowCmd): void {
    if (cmd.rect.w <= 0 || cmd.rect.h <= 0 || cmd.clip.w <= 0 || cmd.clip.h <= 0) return;
    this.inst(
      cmd.rect, cmd.radii, cmd.color, cmd.color,
      0, MODE_SHADOW, cmd.blur,
      0, 0, 0, 0, cmd.clip,
    );
  }

  private emitText(cmd: TextCmd, dpr: number): void {
    if (cmd.clip.w <= 0 || cmd.clip.h <= 0) return;
    const run = this.engine.shape(cmd.font, dpr, cmd.text);
    const baseline = cmd.y + run.ascent;
    for (const g of run.glyphs) {
      // keep the glyph's true fractional position: snap the pen to a whole
      // device pixel plus one of PHASES subpixel steps, and use the atlas
      // variant rasterized at that step. Rounding each pen to a whole pixel
      // instead would leave every gap between letters off by up to half a
      // pixel (spacing jitter that reads as bad kerning)
      const xDev = (cmd.x + g.x) * dpr;
      let penX = Math.floor(xDev);
      let phase = Math.round((xDev - penX) * PHASES);
      if (phase >= PHASES) {
        penX += 1;
        phase = 0;
      }
      const glyph = this.engine.glyph(cmd.font, dpr, g, phase);
      if (!glyph) continue;
      const penY = Math.round(baseline * dpr);
      const quad: Rect = {
        x: (penX + glyph.bx) / dpr,
        y: (penY + glyph.by) / dpr,
        w: glyph.w / dpr,
        h: glyph.h / dpr,
      };
      this.inst(
        quad, NO_CORNERS, glyph.colored ? WHITE : cmd.color, cmd.color,
        0, MODE_TEXTURED, 0,
        glyph.x0 / ATLAS_SIZE, glyph.y0 / ATLAS_SIZE,
        glyph.x1 / ATLAS_SIZE, glyph.y1 / ATLAS_SIZE,
        cmd.clip,
      );
    }
  }

  private inst(
    r: Rect, radii: Corners, color: Color, border: Color,
    borderWidth: number, mode: number, blur: number,
    u0: number, v0: number, u1: number, v1: number,
    clip: Rect,
  ): void {
    if ((this.count + 1) * FLOATS_PER_INSTANCE > this.data.length) {
      const grown = new Float32Array(this.data.length * 2);
      grown.set(this.data);
      this.data = grown;
    }
    const d = this.data;
    let o = this.count * FLOATS_PER_INSTANCE;
    d[o++] = r.x; d[o++] = r.y; d[o++] = r.w; d[o++] = r.h;
    d[o++] = radii.tl; d[o++] = radii.tr; d[o++] = radii.br; d[o++] = radii.bl;
    d[o++] = color.r; d[o++] = color.g; d[o++] = color.b; d[o++] = color.a;
    d[o++] = border.r; d[o++] = border.g; d[o++] = border.b; d[o++] = border.a;
    d[o++] = borderWidth; d[o++] = mode; d[o++] = blur; d[o++] = 0;
    d[o++] = u0; d[o++] = v0; d[o++] = u1; d[o++] = v1;
    d[o++] = clip.x; d[o++] = clip.y; d[o++] = clip.w; d[o++] = clip.h;
    this.count++;
  }
}
