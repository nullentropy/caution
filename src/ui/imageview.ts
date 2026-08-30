import type { Radius } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';
import type { SemanticSpec } from './semantics';
import { Size, Widget } from './widget';

/**
 * A bitmap image. `src` is a URL or data: URI. The texture loads
 * asynchronously (placeholder until ready). `fit` follows the native image
 * view modes. `radius` rounds the corners in the shader. `alt` is what
 * screen readers hear.
 */
export class ImageView extends Widget {
  src = '';
  fit: 'contain' | 'cover' | 'fill' = 'contain';
  radius: Radius = 0;
  alt = '';

  override intrinsicSize(): Size {
    return { w: this.width ?? 0, h: this.height ?? 0 };
  }

  override paintSelf(dl: DisplayList): void {
    if (this.src) dl.image(this.bounds, this.src, this.fit, this.radius);
  }

  override semantics(): SemanticSpec | null {
    return this.alt ? { role: 'image', label: this.alt } : null;
  }
}
