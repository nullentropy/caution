import type { DisplayList } from '../gfx/painter';
import { Widget } from './widget';

/**
 * A pane painted entirely by an app-supplied fragment shader (ShaderToy
 * style): the GLSL defines `vec4 effect(vec2 uv)` and may use u_time, u_res,
 * and custom float uniforms. `animate` keeps the surface repainting while
 * the pane is visible.
 */
export class ShaderPane extends Widget {
  frag = '';
  uniforms: Record<string, number> = {};
  animate = false;

  override paintSelf(dl: DisplayList): void {
    if (this.frag) dl.shaderQuad(this.bounds, this.frag, this.uniforms, this.animate);
  }
}
