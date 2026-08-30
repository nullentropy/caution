// Damage-plan fixtures mirroring go/terminal/gfx/damage_test.go, the two
// terminals must agree on what partial invalidation repaints
import { cmdEqual, diffDamage, paintedBounds, planDamage } from '../gfx/damage';
import { BLACK, WHITE } from '../gfx/color';
import { font } from '../gfx/font';
import { NO_CLIP, overlaps, rect } from '../gfx/geom';
import { DisplayList, type Cmd } from '../gfx/painter';
import { eq, test } from './harness';

const view = rect(0, 0, 1000, 700);

test('plan: top-level animated quad is its own damage; static quads are not', () => {
  const dl = new DisplayList();
  dl.rect(rect(0, 0, 1000, 700), { color: WHITE });
  dl.shaderQuad(rect(100, 100, 200, 150), 'f', {}, true);
  dl.shaderQuad(rect(600, 50, 50, 50), 'f', {}, false);
  const p = planDamage(dl, view);
  eq(p.rects, [rect(100, 100, 200, 150)]);
  eq(p.layers.size, 0);
});

test('plan: no animation means no partial frames', () => {
  const dl = new DisplayList();
  dl.rect(rect(0, 0, 100, 100), { color: WHITE });
  dl.beginLayer(rect(10, 10, 50, 50));
  dl.text('hello', 12, 12, font(13), WHITE);
  dl.endLayer('f', {}, false);
  const p = planDamage(dl, view);
  eq(p.rects, []);
  eq(p.layers.get(1), { end: 3, cacheable: true });
});

test('plan: nested animation promotes to the outermost layer and poisons caches', () => {
  const dl = new DisplayList();
  dl.beginLayer(rect(0, 0, 400, 400)); // idx 0
  dl.beginLayer(rect(50, 50, 100, 100)); // idx 1
  dl.shaderQuad(rect(60, 60, 20, 20), 'f', {}, true); // idx 2
  dl.endLayer('f', {}, false); // idx 3
  dl.endLayer('f', {}, false); // idx 4
  const p = planDamage(dl, view);
  eq(p.rects, [rect(0, 0, 400, 400)]);
  eq(p.layers.get(0), { end: 4, cacheable: false });
  eq(p.layers.get(1), { end: 3, cacheable: false });
});

test('plan: animated composite over static content stays cacheable (the CRT case)', () => {
  const dl = new DisplayList();
  dl.beginLayer(rect(0, 0, 1000, 700));
  dl.text('static', 10, 10, font(13), WHITE);
  dl.endLayer('crt', {}, true);
  const p = planDamage(dl, view);
  eq(p.rects, [rect(0, 0, 1000, 700)]);
  eq(p.layers.get(0), { end: 2, cacheable: true });
});

test('plan: nested animated composite poisons enclosing but not itself', () => {
  const dl = new DisplayList();
  dl.beginLayer(rect(0, 0, 500, 500)); // outer, idx 0
  dl.beginLayer(rect(10, 10, 50, 50)); // inner, idx 1
  dl.text('x', 12, 12, font(13), WHITE);
  dl.endLayer('spin', {}, true);
  dl.endLayer('f', {}, false);
  const p = planDamage(dl, view);
  eq(p.rects, [rect(0, 0, 500, 500)]);
  eq(p.layers.get(0)?.cacheable, false);
  eq(p.layers.get(1)?.cacheable, true);
});

test('plan: merges overlapping damage, keeps disjoint, clamps to view', () => {
  const dl = new DisplayList();
  dl.shaderQuad(rect(0, 0, 100, 100), 'f', {}, true);
  dl.shaderQuad(rect(50, 50, 100, 100), 'f', {}, true);
  dl.shaderQuad(rect(950, 650, 200, 200), 'f', {}, true); // straddles the edge
  const p = planDamage(dl, view);
  eq(p.rects, [rect(0, 0, 150, 150), rect(950, 650, 50, 50)]);
});

test('plan: beyond the pass cap, damage collapses to one bounding box', () => {
  const dl = new DisplayList();
  for (let i = 0; i < 13; i++) {
    dl.shaderQuad(rect(i * 70, i * 50, 10, 10), 'f', {}, true);
  }
  const p = planDamage(dl, view);
  eq(p.rects.length, 1);
  eq(p.rects[0], rect(0, 0, 12 * 70 + 10, 12 * 50 + 10));
});

test('paintedBounds is conservative for shadows, AA margins, text, and clips', () => {
  const sh: Cmd = {
    kind: 'shadow', rect: rect(100, 100, 50, 50), radii: { tl: 0, tr: 0, br: 0, bl: 0 },
    color: WHITE, blur: 10, clip: NO_CLIP,
  };
  const b = paintedBounds(sh);
  eq(b.x <= 100 - 26 && b.y <= 100 - 26 && b.x + b.w >= 176 && b.y + b.h >= 176, true, 'shadow inflation');

  const tx: Cmd = { kind: 'text', x: 0, y: 0, text: 't', font: font(13), color: WHITE, clip: rect(50, 50, 100, 20), box: rect(0, 0, 0, 0) };
  eq(overlaps(paintedBounds(tx), rect(140, 60, 5, 5)), true, 'clipped text inside clip');
  eq(overlaps(paintedBounds(tx), rect(400, 400, 5, 5)), false, 'clipped text outside clip');

  const free: Cmd = { kind: 'text', x: 0, y: 0, text: 't', font: font(13), color: WHITE, clip: NO_CLIP, box: rect(0, 0, 0, 0) };
  eq(overlaps(paintedBounds(free), rect(400, 400, 5, 5)), true, 'unclipped text matches everything');

  const clipped: Cmd = {
    kind: 'rect', rect: rect(0, 0, 900, 900), radii: { tl: 0, tr: 0, br: 0, bl: 0 },
    color: WHITE, borderWidth: 0, borderColor: WHITE, clip: rect(200, 200, 50, 50),
  };
  eq(overlaps(paintedBounds(clipped), rect(600, 600, 10, 10)), false, 'clip bounds a huge rect');

  // Shader quads record no clip (their rect is pre-intersected); bounds must
  // be the rect itself - else the animated pane would be excluded from its
  // own replay.
  const sq: Cmd = { kind: 'shader-quad', rect: rect(100, 100, 50, 50), uv: [0, 0, 1, 1], frag: 'f', uniforms: {}, animated: true };
  eq(overlaps(paintedBounds(sq), rect(120, 120, 5, 5)), true, 'shader quad bounds are its rect');
});

// -- real damage -------------------------------------------------------------

/** a list with a fixed-advance measure, so text records a line box */
function measured(): DisplayList {
  const dl = new DisplayList();
  dl.measure = (f, s) => ({ w: s.length * 7, ascent: f.size * 0.8, descent: f.size * 0.2 });
  return dl;
}

test('cmdEqual sees every field of every command kind', () => {
  const samples: Cmd[] = [
    { kind: 'rect', rect: rect(1, 2, 3, 4), radii: { tl: 1, tr: 2, br: 3, bl: 4 }, color: WHITE, borderWidth: 1, borderColor: WHITE, clip: rect(0, 0, 9, 9) },
    { kind: 'shadow', rect: rect(1, 2, 3, 4), radii: { tl: 1, tr: 2, br: 3, bl: 4 }, color: WHITE, blur: 5, clip: rect(0, 0, 9, 9) },
    { kind: 'text', x: 1, y: 2, text: 'hi', font: font(13), color: WHITE, clip: rect(0, 0, 9, 9), box: rect(1, 2, 14, 16) },
    { kind: 'layer-begin', rect: rect(1, 2, 3, 4) },
    { kind: 'layer-end', rect: rect(1, 2, 3, 4), frag: 'f', uniforms: { u: 1 }, animated: true },
    { kind: 'shader-quad', rect: rect(1, 2, 3, 4), uv: [0, 0, 1, 1], frag: 'f', uniforms: { u: 1 }, animated: true },
    { kind: 'image', rect: rect(1, 2, 3, 4), src: 'a.png', fit: 'cover', radii: { tl: 1, tr: 2, br: 3, bl: 4 }, clip: rect(0, 0, 9, 9) },
  ];
  for (const s of samples) {
    eq(cmdEqual(s, { ...s }), true, `${s.kind} equals its own copy`);
    for (const key of Object.keys(s)) {
      if (key === 'kind') continue;
      const v = (s as any)[key];
      let mutated: unknown;
      if (typeof v === 'number') mutated = v + 1;
      else if (typeof v === 'string') mutated = `${v}x`;
      else if (typeof v === 'boolean') mutated = !v;
      else if (Array.isArray(v)) mutated = [v[0] + 1, ...v.slice(1)];
      else mutated = { ...v, ...Object.fromEntries(Object.keys(v).map((k) => [k, (v as any)[k] + 1])) };
      eq(cmdEqual(s, { ...s, [key]: mutated } as Cmd), false, `${s.kind}.${key} is compared`);
    }
  }
});

test('diff: identical lists are clean', () => {
  const build = (): DisplayList => {
    const dl = measured();
    dl.rect(rect(0, 0, 1000, 700), { color: WHITE });
    dl.text('hello', 10, 10, font(13), WHITE);
    return dl;
  };
  eq(diffDamage(build(), build(), view, []), []);
});

test('diff: one changed command damages where it was and where it went', () => {
  const build = (y: number): DisplayList => {
    const dl = measured();
    dl.rect(rect(0, 0, 1000, 700), { color: WHITE });
    dl.rect(rect(100, y, 40, 20), { color: WHITE });
    dl.rect(rect(500, 500, 40, 20), { color: WHITE });
    return dl;
  };
  eq(diffDamage(build(200), build(300), view, []), [rect(99, 199, 42, 22), rect(99, 299, 42, 22)]);
});

test('diff: a text change damages its line box, not the view', () => {
  const build = (s: string): DisplayList => {
    const dl = measured();
    dl.text(s, 100, 100, font(13), WHITE);
    return dl;
  };
  const got = diffDamage(build('aaaa'), build('bbbb'), view, []);
  eq(got?.length, 1);
  // the line box (4 × 7 wide, 13 tall) plus one em of ink slack, twice over
  eq(got![0]!.w < 100 && got![0]!.h < 100, true, `tight text damage, got ${JSON.stringify(got![0])}`);
});

test('diff: animated rects survive an otherwise identical frame', () => {
  const build = (): DisplayList => {
    const dl = measured();
    dl.shaderQuad(rect(10, 10, 30, 30), 'f', {}, true);
    return dl;
  };
  eq(diffDamage(build(), build(), view, [rect(10, 10, 30, 30)]), [rect(10, 10, 30, 30)]);
});

test('diff: damage inside a layer promotes to the outermost layer', () => {
  const build = (x: number): DisplayList => {
    const dl = measured();
    dl.beginLayer(rect(100, 100, 200, 200));
    dl.beginLayer(rect(120, 120, 60, 60));
    dl.rect(rect(x, 130, 10, 10), { color: WHITE });
    dl.endLayer('f', {}, false);
    dl.endLayer('g', {}, false);
    return dl;
  };
  eq(diffDamage(build(130), build(140), view, []), [rect(100, 100, 200, 200)]);
});

test('diff: a length change trims the common ends', () => {
  const prev = measured();
  prev.rect(rect(0, 0, 1000, 700), { color: WHITE });
  prev.rect(rect(600, 600, 10, 10), { color: WHITE });
  const next = measured();
  next.rect(rect(0, 0, 1000, 700), { color: WHITE });
  next.rect(rect(300, 300, 20, 20), { color: WHITE }); // a hover ring appears
  next.rect(rect(600, 600, 10, 10), { color: WHITE });
  eq(diffDamage(prev, next, view, []), [rect(299, 299, 22, 22)]);
});

test('diff: a layer appearing takes the full frame', () => {
  const prev = measured();
  prev.rect(rect(0, 0, 100, 100), { color: WHITE });
  const next = measured();
  next.beginLayer(rect(0, 0, 100, 100)); // a fade-in starts
  next.rect(rect(0, 0, 100, 100), { color: WHITE });
  next.endLayer('f', {}, false);
  eq(diffDamage(prev, next, view, []), null);
});

test('diff: a whole-view change takes the full frame', () => {
  const prev = measured();
  const next = measured();
  for (let i = 0; i < 20; i++) {
    prev.rect(rect(0, i * 35, 1000, 35), { color: BLACK });
    next.rect(rect(0, i * 35, 1000, 35), { color: WHITE });
  }
  eq(diffDamage(prev, next, view, []), null);
});

test('diff: nothing retained means no partial frame', () => {
  eq(diffDamage(null, measured(), view, []), null);
});

// -- the flag that decides whether partial frames happen --------------

test('wantsAnimation survives a copied command range', () => {
  const built = new DisplayList();
  built.shaderQuad(rect(0, 0, 10, 10), 'f', {}, true);
  eq(built.wantsAnimation, true, 'a freshly built animated quad asks for frames');

  // what a spliced subtree does: copy retained commands into the new list
  // without calling shaderQuad or endLayer. an accumulated flag is
  // lost here, and the surface stops asking for frames, which killed all
  // animation on the first frame that reused anything.
  const spliced = new DisplayList();
  for (const c of built.cmds) spliced.cmds.push(c);
  eq(spliced.wantsAnimation, true, 'copied commands still ask for frames');

  const layer = new DisplayList();
  layer.beginLayer(rect(0, 0, 10, 10));
  layer.endLayer('f', {}, true);
  const copied = new DisplayList();
  for (const c of layer.cmds) copied.cmds.push(c);
  eq(copied.wantsAnimation, true, 'a copied animated composite asks for frames');

  const still = new DisplayList();
  still.rect(rect(0, 0, 10, 10), { color: WHITE });
  eq(still.wantsAnimation, false, 'a still list asks for nothing');

  const geom = new DisplayList();
  geom.geomAnimation = true;
  eq(geom.wantsAnimation, true, 'a geometric tween asks for frames');
});
