import { Color } from '../gfx/color';
import { Font, font, MONO } from '../gfx/font';
import { Corners, NO_CLIP, Rect } from '../gfx/geom';
import type { DisplayList } from '../gfx/painter';

const utf8 = new TextDecoder();
const fonts = new Map<string, Font>();

class Reader {
  private view: DataView;
  private pos = 0;
  strings: string[] = [];

  constructor(private bytes: Uint8Array, len: number) {
    this.view = new DataView(bytes.buffer, bytes.byteOffset, len);
  }

  u8(): number {
    return this.view.getUint8(this.pos++);
  }

  u16(): number {
    const v = this.view.getUint16(this.pos, true);
    this.pos += 2;
    return v;
  }

  u32(): number {
    const v = this.view.getUint32(this.pos, true);
    this.pos += 4;
    return v;
  }

  f32(): number {
    const v = this.view.getFloat32(this.pos, true);
    this.pos += 4;
    return v;
  }

  bool(): boolean {
    return this.u8() !== 0;
  }

  str(): string {
    return this.strings[this.u32()] ?? '';
  }

  rawString(): string {
    const n = this.u32();
    const s = utf8.decode(this.bytes.subarray(this.pos, this.pos + n));
    this.pos += n;
    return s;
  }

  rect(): Rect {
    const x = this.f32();
    const y = this.f32();
    const w = this.f32();
    const h = this.f32();
    if (x === NO_CLIP.x && y === NO_CLIP.y && w === NO_CLIP.w && h === NO_CLIP.h) return NO_CLIP;
    return { x, y, w, h };
  }

  corners(): Corners {
    return { tl: this.f32(), tr: this.f32(), br: this.f32(), bl: this.f32() };
  }

  color(): Color {
    return { r: this.f32(), g: this.f32(), b: this.f32(), a: this.f32() };
  }

  font(): Font {
    const size = this.f32();
    const weight = this.u16();
    const flags = this.u8();
    const key = `${size}|${weight}|${flags}`;
    let f = fonts.get(key);
    if (!f) {
      f = font(size, { weight, italic: (flags & 1) !== 0, family: flags & 2 ? MONO : undefined });
      fonts.set(key, f);
    }
    return f;
  }

  uniforms(): Record<string, number> {
    const n = this.u16();
    const out: Record<string, number> = {};
    for (let i = 0; i < n; i++) out[this.str()] = this.f32();
    return out;
  }
}

const FITS = ['contain', 'cover', 'fill'] as const;

/** fill dl from an encoded frame; returns the background color it carried */
export function decodeFrame(bytes: Uint8Array, len: number, dl: DisplayList): Color {
  const r = new Reader(bytes, len);
  const flags = r.u8();
  dl.geomAnimation = (flags & 1) !== 0;
  const bg = r.color();
  const nStrings = r.u32();
  for (let i = 0; i < nStrings; i++) r.strings.push(r.rawString());
  const count = r.u32();
  const cmds = dl.cmds;
  for (let i = 0; i < count; i++) {
    switch (r.u8()) {
      case 0:
        cmds.push({
          kind: 'rect',
          rect: r.rect(),
          radii: r.corners(),
          color: r.color(),
          borderColor: r.color(),
          borderWidth: r.f32(),
          clip: r.rect(),
        });
        break;
      case 1:
        cmds.push({ kind: 'shadow', rect: r.rect(), radii: r.corners(), color: r.color(), blur: r.f32(), clip: r.rect() });
        break;
      case 2:
        cmds.push({
          kind: 'text',
          x: r.f32(),
          y: r.f32(),
          box: r.rect(),
          text: r.str(),
          font: r.font(),
          color: r.color(),
          clip: r.rect(),
        });
        break;
      case 3:
        cmds.push({ kind: 'layer-begin', rect: r.rect() });
        break;
      case 4:
        cmds.push({ kind: 'layer-end', rect: r.rect(), frag: r.str(), animated: r.bool(), uniforms: r.uniforms() });
        break;
      case 5:
        cmds.push({
          kind: 'shader-quad',
          rect: r.rect(),
          uv: [r.f32(), r.f32(), r.f32(), r.f32()],
          frag: r.str(),
          animated: r.bool(),
          uniforms: r.uniforms(),
        });
        break;
      case 6:
        cmds.push({
          kind: 'image',
          rect: r.rect(),
          src: r.str(),
          fit: FITS[r.u8()] ?? 'contain',
          radii: r.corners(),
          clip: r.rect(),
        });
        break;
      default:
        throw new Error('caution: bad frame');
    }
  }
  return bg;
}
