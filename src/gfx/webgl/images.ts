/**
 * Async image -> GL texture cache, keyed by src.
 * While an image loads, the renderer draws a placeholder.
 * onLoad fires so the surface can repaint.
 */
export interface StoredImage {
  tex: WebGLTexture | null;
  w: number;
  h: number;
  state: 'loading' | 'ready' | 'error';
}

export class ImageStore {
  onLoad: (() => void) | null = null;
  private map = new Map<string, StoredImage>();
  /**
   * Set whenever an entry changes state: same display list, different pixels,
   * so the next frame cannot trust a damage diff. The renderer reads and
   * clears it (see takeChanged).
   */
  private changed = false;

  /** whether any image changed state since the last call */
  takeChanged(): boolean {
    const c = this.changed;
    this.changed = false;
    return c;
  }

  constructor(private gl: WebGL2RenderingContext) {}

  get(src: string): StoredImage {
    const hit = this.map.get(src);
    if (hit) return hit;
    const entry: StoredImage = { tex: null, w: 0, h: 0, state: 'loading' };
    this.map.set(src, entry);

    const img = new Image();
    img.crossOrigin = 'anonymous'; // texture upload needs CORS-clean pixels
    img.onload = () => {
      const gl = this.gl;
      const tex = gl.createTexture();
      if (!tex) {
        entry.state = 'error';
        this.changed = true;
        return;
      }
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, true);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, img);
      gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false);
      entry.tex = tex;
      entry.w = img.naturalWidth;
      entry.h = img.naturalHeight;
      entry.state = 'ready';
      this.changed = true;
      this.onLoad?.();
    };
    img.onerror = () => {
      entry.state = 'error';
      this.changed = true;
      this.onLoad?.();
    };
    img.src = src;
    return entry;
  }
}
