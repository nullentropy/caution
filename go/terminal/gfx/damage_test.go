package gfx

import (
	"reflect"
	"testing"
)

var testView = R(0, 0, 1000, 700)

func rectsEqual(a, b []Rect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPlanDamageTopLevelQuad(t *testing.T) {
	dl := &DisplayList{}
	dl.Fill(R(0, 0, 1000, 700), White, Corners{})
	dl.ShaderQuad(R(100, 100, 200, 150), "f", nil, true)
	dl.ShaderQuad(R(600, 50, 50, 50), "f", nil, false) // static: no damage

	p := PlanDamage(dl, testView)
	want := []Rect{R(100, 100, 200, 150)}
	if !rectsEqual(p.Rects, want) {
		t.Fatalf("rects = %v, want %v", p.Rects, want)
	}
	if len(p.Layers) != 0 {
		t.Fatalf("layers = %v, want none", p.Layers)
	}
}

func TestPlanDamageNoAnimationMeansNoPartial(t *testing.T) {
	dl := &DisplayList{}
	dl.Fill(R(0, 0, 100, 100), White, Corners{})
	dl.BeginLayer(R(10, 10, 50, 50))
	dl.Text("hello", 12, 12, Font{}, White)
	dl.EndLayer("f", nil, false)

	p := PlanDamage(dl, testView)
	if len(p.Rects) != 0 {
		t.Fatalf("rects = %v, want none", p.Rects)
	}
	span := p.Layers[1]
	if span.End != 3 || !span.Cacheable {
		t.Fatalf("layer span = %+v, want End 3 Cacheable", span)
	}
}

func TestPlanDamagePromotesThroughLayers(t *testing.T) {
	// outer layer > inner layer > animated quad: damage is the OUTER rect
	// (a composite may map any texel anywhere), and neither layer's content
	// is frame-invariant.
	dl := &DisplayList{}
	dl.BeginLayer(R(0, 0, 400, 400))                 // idx 0
	dl.BeginLayer(R(50, 50, 100, 100))               // idx 1
	dl.ShaderQuad(R(60, 60, 20, 20), "f", nil, true) // idx 2
	dl.EndLayer("f", nil, false)                     // idx 3
	dl.EndLayer("f", nil, false)                     // idx 4

	p := PlanDamage(dl, testView)
	want := []Rect{R(0, 0, 400, 400)}
	if !rectsEqual(p.Rects, want) {
		t.Fatalf("rects = %v, want %v", p.Rects, want)
	}
	if p.Layers[0].Cacheable || p.Layers[1].Cacheable {
		t.Fatalf("layers = %v, want both non-cacheable", p.Layers)
	}
	if p.Layers[0].End != 4 || p.Layers[1].End != 3 {
		t.Fatalf("spans = %v, want 0->4, 1->3", p.Layers)
	}
}

func TestPlanDamageAnimatedCompositeOverStaticContentIsCacheable(t *testing.T) {
	// The CRT case: static content, u_time in the composite. The layer's
	// texture never changes - cacheable - while its rect is damage.
	dl := &DisplayList{}
	dl.BeginLayer(R(0, 0, 1000, 700))
	dl.Text("static", 10, 10, Font{}, White)
	dl.EndLayer("crt", nil, true)

	p := PlanDamage(dl, testView)
	want := []Rect{R(0, 0, 1000, 700)}
	if !rectsEqual(p.Rects, want) {
		t.Fatalf("rects = %v, want %v", p.Rects, want)
	}
	if !p.Layers[0].Cacheable {
		t.Fatal("static-content layer with animated composite must stay cacheable")
	}
}

func TestPlanDamageAnimatedNestedCompositePoisonsEnclosing(t *testing.T) {
	dl := &DisplayList{}
	dl.BeginLayer(R(0, 0, 500, 500)) // outer, idx 0
	dl.BeginLayer(R(10, 10, 50, 50)) // inner, idx 1
	dl.Text("x", 12, 12, Font{}, White)
	dl.EndLayer("spin", nil, true) // inner composite animates within outer
	dl.EndLayer("f", nil, false)

	p := PlanDamage(dl, testView)
	want := []Rect{R(0, 0, 500, 500)}
	if !rectsEqual(p.Rects, want) {
		t.Fatalf("rects = %v, want %v", p.Rects, want)
	}
	if p.Layers[0].Cacheable {
		t.Fatal("outer layer holds an animating composite; must not be cacheable")
	}
	if !p.Layers[1].Cacheable {
		t.Fatal("inner content is static; the animated composite alone must not poison it")
	}
}

func TestPlanDamageMergesOverlapsKeepsDisjoint(t *testing.T) {
	dl := &DisplayList{}
	dl.ShaderQuad(R(0, 0, 100, 100), "f", nil, true)
	dl.ShaderQuad(R(50, 50, 100, 100), "f", nil, true) // overlaps the first
	dl.ShaderQuad(R(500, 500, 40, 40), "f", nil, true) // far away

	p := PlanDamage(dl, testView)
	want := []Rect{R(0, 0, 150, 150), R(500, 500, 40, 40)}
	if !rectsEqual(p.Rects, want) {
		t.Fatalf("rects = %v, want %v", p.Rects, want)
	}
}

func TestPlanDamageClampsToViewAndCaps(t *testing.T) {
	dl := &DisplayList{}
	dl.ShaderQuad(R(950, 650, 200, 200), "f", nil, true) // straddles the edge
	p := PlanDamage(dl, testView)
	want := []Rect{R(950, 650, 50, 50)}
	if !rectsEqual(p.Rects, want) {
		t.Fatalf("rects = %v, want %v", p.Rects, want)
	}

	// Beyond the pass cap, damage collapses to one bounding box.
	dl = &DisplayList{}
	for i := 0; i < maxDamageRects+1; i++ {
		dl.ShaderQuad(R(float32(i*70), float32(i*50), 10, 10), "f", nil, true)
	}
	p = PlanDamage(dl, testView)
	if len(p.Rects) != 1 {
		t.Fatalf("capped rects = %v, want one bounding box", p.Rects)
	}
	box := p.Rects[0]
	if box.X != 0 || box.Y != 0 || box.W != float32(maxDamageRects*70+10) || box.H != float32(maxDamageRects*50+10) {
		t.Fatalf("bounding box = %v", box)
	}
}

func TestPaintedBoundsIsConservative(t *testing.T) {
	// Shadows paint outside their rect: the vertex shader inflates the quad
	// by blur*2.5 + 1 (see render/shaders.go).
	sh := &Cmd{Kind: CmdShadow, Rect: R(100, 100, 50, 50), Blur: 10, Clip: NoClip}
	b := PaintedBounds(sh)
	if b.X > 100-26 || b.Y > 100-26 || b.X+b.W < 176 || b.Y+b.H < 176 {
		t.Fatalf("shadow bounds %v don't cover the inflated quad", b)
	}

	// Rects get 1px of AA margin.
	rc := &Cmd{Kind: CmdRect, Rect: R(10, 10, 20, 20), Clip: NoClip}
	b = PaintedBounds(rc)
	if b.X > 9 || b.X+b.W < 31 {
		t.Fatalf("rect bounds %v miss the AA margin", b)
	}

	// Text has no recorded rect: its clip is the bound, NoClip matches all.
	tx := &Cmd{Kind: CmdText, Clip: R(50, 50, 100, 20)}
	if !Overlaps(PaintedBounds(tx), R(140, 60, 5, 5)) {
		t.Fatal("clipped text must intersect damage inside its clip")
	}
	if Overlaps(PaintedBounds(tx), R(400, 400, 5, 5)) {
		t.Fatal("clipped text must not intersect damage far outside its clip")
	}
	free := &Cmd{Kind: CmdText, Clip: NoClip}
	if !Overlaps(PaintedBounds(free), R(400, 400, 5, 5)) {
		t.Fatal("unclipped text must conservatively intersect everything")
	}

	// A clip tighter than the rect bounds the paint.
	clipped := &Cmd{Kind: CmdRect, Rect: R(0, 0, 900, 900), Clip: R(200, 200, 50, 50)}
	if Overlaps(PaintedBounds(clipped), R(600, 600, 10, 10)) {
		t.Fatal("clip must bound a huge rect's painted extent")
	}

	// Shader quads record no clip (their rect is pre-intersected); the
	// zero-value Clip must not empty their bounds - else the animated pane
	// would be excluded from its own replay.
	sq := &Cmd{Kind: CmdShaderQuad, Rect: R(100, 100, 50, 50)}
	if !Overlaps(PaintedBounds(sq), R(120, 120, 5, 5)) {
		t.Fatal("shader quad must intersect damage inside its rect despite zero Clip")
	}
}

// -- real damage -------------------------------------------------------------

// TestCmdEqualSeesEveryField is the guard on cmdEqual: a field it forgets to
// compare is a pixel that goes stale, so every field of Cmd gets mutated
// reflectively and must be noticed. A new field added to Cmd fails here until
// cmdEqual learns about it.
func TestCmdEqualSeesEveryField(t *testing.T) {
	base := Cmd{
		Kind: CmdRect, Rect: R(1, 2, 3, 4), Radii: Corners{1, 2, 3, 4}, Clip: R(0, 0, 9, 9),
		Animated: false, Color: RGBA(0.1, 0.2, 0.3, 1), Border: RGBA(0.4, 0.5, 0.6, 1),
		BorderWidth: 1, Blur: 2, X: 5, Y: 6, Text: "hi", Font: NewFont(12, FontOpts{}),
		Src: "a.png", Fit: "cover", Frag: "f", UV: [4]float32{0, 0, 1, 1},
		Uniforms: map[string]float32{"u": 1},
	}
	if !CmdEqual(&base, &base) {
		t.Fatal("a command must equal itself")
	}
	rv := reflect.ValueOf(base)
	for i := 0; i < rv.NumField(); i++ {
		name := rv.Type().Field(i).Name
		other := base
		if !perturb(reflect.ValueOf(&other).Elem().Field(i)) {
			t.Fatalf("field %s: the test cannot mutate this kind", name)
		}
		if CmdEqual(&base, &other) {
			t.Fatalf("cmdEqual ignores field %s", name)
		}
	}
}

// perturb changes a value observably, descending into structs and arrays
// (Rect, Corners, Font, UV) until it finds a scalar to move.
func perturb(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Float32:
		v.SetFloat(v.Float() + 1)
		return true
	case reflect.Uint8, reflect.Uint:
		v.SetUint(v.Uint() + 1)
		return true
	case reflect.Int:
		v.SetInt(v.Int() + 1)
		return true
	case reflect.String:
		v.SetString(v.String() + "x")
		return true
	case reflect.Bool:
		v.SetBool(!v.Bool())
		return true
	case reflect.Map:
		v.Set(reflect.ValueOf(map[string]float32{"u": 2}))
		return true
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if perturb(v.Field(i)) {
				return true
			}
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if perturb(v.Index(i)) {
				return true
			}
		}
	}
	return false
}

func TestDiffDamageIdenticalListsAreClean(t *testing.T) {
	build := func() *DisplayList {
		dl := &DisplayList{}
		dl.Fill(R(0, 0, 1000, 700), White, Corners{})
		dl.Text("hello", 10, 10, Font{}, White)
		return dl
	}
	got, ok := DiffDamage(build(), build(), testView, nil)
	if !ok {
		t.Fatal("identical lists must be diffable")
	}
	if len(got) != 0 {
		t.Fatalf("rects = %v, want none", got)
	}
}

func TestDiffDamageOneChangedCommand(t *testing.T) {
	build := func(y float32) *DisplayList {
		dl := &DisplayList{}
		dl.Fill(R(0, 0, 1000, 700), White, Corners{})
		dl.Fill(R(100, y, 40, 20), White, Corners{})
		dl.Fill(R(500, 500, 40, 20), White, Corners{})
		return dl
	}
	got, ok := DiffDamage(build(200), build(300), testView, nil)
	if !ok {
		t.Fatal("want a partial frame")
	}
	// Old and new position, each inflated 1px for the shader's AA margin.
	want := []Rect{R(99, 199, 42, 22), R(99, 299, 42, 22)}
	if !rectsEqual(got, want) {
		t.Fatalf("rects = %v, want %v", got, want)
	}
}

func TestDiffDamageSeedsAnimatedRects(t *testing.T) {
	build := func() *DisplayList {
		dl := &DisplayList{}
		dl.ShaderQuad(R(10, 10, 30, 30), "f", nil, true)
		return dl
	}
	// The command is identical frame to frame; its u_time is not, so the
	// animation damage has to survive the diff.
	got, ok := DiffDamage(build(), build(), testView, []Rect{R(10, 10, 30, 30)})
	if !ok || !rectsEqual(got, []Rect{R(10, 10, 30, 30)}) {
		t.Fatalf("rects = %v (ok=%v), want the animated rect", got, ok)
	}
}

func TestDiffDamagePromotesToOutermostLayer(t *testing.T) {
	build := func(x float32) *DisplayList {
		dl := &DisplayList{}
		dl.BeginLayer(R(100, 100, 200, 200))
		dl.BeginLayer(R(120, 120, 60, 60))
		dl.Fill(R(x, 130, 10, 10), White, Corners{})
		dl.EndLayer("f", nil, false)
		dl.EndLayer("g", nil, false)
		return dl
	}
	got, ok := DiffDamage(build(130), build(140), testView, nil)
	if !ok {
		t.Fatal("want a partial frame")
	}
	// A composite may map any texel anywhere: the outer layer's whole
	// footprint is the damage, not the changed fill.
	if !rectsEqual(got, []Rect{R(100, 100, 200, 200)}) {
		t.Fatalf("rects = %v, want the outer layer rect", got)
	}
}

func TestDiffDamageLengthChangeTrimsEnds(t *testing.T) {
	prev := &DisplayList{}
	prev.Fill(R(0, 0, 1000, 700), White, Corners{})
	prev.Fill(R(600, 600, 10, 10), White, Corners{})
	next := &DisplayList{}
	next.Fill(R(0, 0, 1000, 700), White, Corners{})
	next.Fill(R(300, 300, 20, 20), White, Corners{}) // a hover ring appears
	next.Fill(R(600, 600, 10, 10), White, Corners{})

	got, ok := DiffDamage(prev, next, testView, nil)
	if !ok {
		t.Fatal("want a partial frame")
	}
	if !rectsEqual(got, []Rect{R(299, 299, 22, 22)}) {
		t.Fatalf("rects = %v, want just the inserted rect", got)
	}
}

func TestDiffDamageLayerAppearingTakesTheFullFrame(t *testing.T) {
	prev := &DisplayList{}
	prev.Fill(R(0, 0, 100, 100), White, Corners{})
	next := &DisplayList{}
	next.BeginLayer(R(0, 0, 100, 100)) // a fade-in starts
	next.Fill(R(0, 0, 100, 100), White, Corners{})
	next.EndLayer("f", nil, false)

	if got, ok := DiffDamage(prev, next, testView, nil); ok {
		t.Fatalf("rects = %v, want the full frame: the target stack moved", got)
	}
}

func TestDiffDamageWholeScreenChangeTakesTheFullFrame(t *testing.T) {
	prev := &DisplayList{}
	next := &DisplayList{}
	for i := 0; i < 20; i++ {
		prev.Fill(R(0, float32(i)*35, 1000, 35), RGBA(0, 0, 0, 1), Corners{})
		next.Fill(R(0, float32(i)*35, 1000, 35), RGBA(1, 1, 1, 1), Corners{})
	}
	if got, ok := DiffDamage(prev, next, testView, nil); ok {
		t.Fatalf("rects = %v, want the full frame: a theme change is not damage", got)
	}
}

func TestDiffDamageNothingRetained(t *testing.T) {
	if _, ok := DiffDamage(nil, &DisplayList{}, testView, nil); ok {
		t.Fatal("no retained list means no partial frame")
	}
}

// bigList is a ~2600-command frame: what the diff has to scan on every real
// invalidation, and the tax it adds to the whole-frame path when it declines.
func bigList(shift float32) *DisplayList {
	dl := &DisplayList{Measure: func(_ Font, s string) (float32, float32, float32) {
		return float32(len(s)) * 7, 10, 3
	}}
	for i := 0; i < 1300; i++ {
		y := float32(20+i*5) + shift
		dl.Fill(R(20, y, 1100, 4), White, Corners{})
		dl.Text("row static", 26, y-4, NewFont(11, FontOpts{}), White)
	}
	return dl
}

func BenchmarkDiffDamageClean(b *testing.B) {
	prev, next := bigList(0), bigList(0)
	view := R(0, 0, 1180, 780)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DiffDamage(prev, next, view, nil)
	}
}

func BenchmarkDiffDamageEverythingMoved(b *testing.B) {
	prev, next := bigList(0), bigList(3)
	view := R(0, 0, 1180, 780)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DiffDamage(prev, next, view, nil)
	}
}
