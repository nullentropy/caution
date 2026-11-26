package render

import (
	"cmp"
	"fmt"
	"log"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/text"
)

const floatsPerInstance = gpu.InstanceFloats

const (
	modeRect     = 0
	modeShadow   = 1
	modeTextured = 2
	modeSolid    = 3
)

var (
	imgPlaceholder = gfx.RGBA(0.5, 0.55, 0.6, 0.08)
	imgErrorTint   = gfx.RGBA(0.9, 0.3, 0.3, 0.12)
)

type target struct {
	tex              gpu.Texture
	originX, originY float32
	viewW, viewH     float32
	devW, devH       int32
	flipY            float32
	pooled           *pooledLayer
}

type pooledLayer struct {
	tex      gpu.Texture
	key      string
	bytes    int
	released uint64
}

const (
	layerIdleFrames = 60
	maxPoolBytes    = 64 << 20
)

type userProgram struct {
	pipe gpu.Pipeline
	used uint64
}

const maxUserProgs = 48

type Frame struct {
	ViewW, ViewH  float32
	DevW, DevH    int32
	Target        gpu.Texture
	Time          float32
	SkipUnpainted bool
}

type Renderer struct {
	Fonts  *text.Registry
	Shaper *text.Shaper
	Atlas  *text.Atlas
	Images *ImageStore

	dev       gpu.Device
	prog      gpu.Pipeline
	tex       gpu.Texture
	cur       target
	oversized bool
	failed    bool

	data                 []float32
	count                int
	lastDPR              float32
	time                 float32
	layerPool            map[string][]*pooledLayer
	poolBytes            int
	frameNo              uint64
	userProgs            map[string]*userProgram
	progTick             uint64
	lastViewW, lastViewH float32

	scene         *sceneFBO
	retained      *gfx.DisplayList
	plan          gfx.DamagePlan
	layerCache    map[int]target
	sceneScissor  *[4]int32
	lastBg        gfx.Color
	lastBgSet     bool
	lastTarget    gpu.Texture
	lastTargetSet bool
	Frames        FrameCounts
}

type FrameResult struct {
	Painted   bool
	Rects     int
	Full      bool
	Oversized bool
	Stopped   bool
	Empty     bool
}

type FrameCounts struct {
	Full, Damage, Partial int
}

type sceneFBO struct {
	tex          gpu.Texture
	devW, devH   int32
	viewW, viewH float32
}

func New(dev gpu.Device) (*Renderer, error) {
	fonts := text.NewRegistry()
	r := &Renderer{
		Fonts:      fonts,
		Shaper:     text.NewShaper(fonts),
		Atlas:      text.NewAtlas(fonts),
		Images:     newImageStore(dev),
		dev:        dev,
		data:       make([]float32, 4096*floatsPerInstance),
		layerPool:  make(map[string][]*pooledLayer),
		userProgs:  make(map[string]*userProgram),
		layerCache: make(map[int]target),
	}
	prog, err := dev.NewPipeline(vertSrc, fragSrc, true)
	if err != nil {
		return nil, err
	}
	r.prog = prog
	if r.tex, err = dev.NewTexture(text.AtlasSize, text.AtlasSize, gpu.Linear); err != nil {
		dev.DeletePipeline(prog)
		return nil, err
	}
	r.Atlas.OnReset = func() { r.flushBatch(r.cur, nil) }
	return r, nil
}

func (r *Renderer) fail(err error) {
	if !r.failed {
		log.Printf("caution: %v, drawing stopped", err)
	}
	r.failed = true
}

func (r *Renderer) Render(dl *gfx.DisplayList, background gfx.Color, fr Frame) FrameResult {
	res := r.render(dl, background, fr)
	res.Stopped = res.Stopped || r.failed
	return res
}

func (r *Renderer) render(dl *gfx.DisplayList, background gfx.Color, fr Frame) FrameResult {
	if r.dev.Lost() || r.failed {
		r.Images.discardUploads()
		return FrameResult{Stopped: true}
	}
	if fr.DevW <= 0 || fr.DevH <= 0 || fr.ViewW <= 0 || fr.ViewH <= 0 {
		return FrameResult{Empty: true}
	}
	if !r.fits(fr) {
		return FrameResult{Oversized: true}
	}
	r.time = fr.Time
	imagesChanged := r.Images.drainUploads()
	dpr := float32(fr.DevW) / fr.ViewW
	dprChanged := dpr != r.lastDPR
	if dprChanged {
		if r.lastDPR != 0 {
			r.Atlas.Reset()
			r.flushLayerCache()
			r.clearLayerPool()
		}
		r.lastDPR = dpr
	}
	if fr.ViewW != r.lastViewW || fr.ViewH != r.lastViewH {
		if r.lastViewW != 0 {
			r.flushLayerCache()
			r.clearLayerPool()
		}
		r.lastViewW, r.lastViewH = fr.ViewW, fr.ViewH
	}
	view := gfx.R(0, 0, fr.ViewW, fr.ViewH)
	plan := gfx.PlanDamage(dl, view)
	var damage []gfx.Rect
	partial := false
	if !dprChanged && !imagesChanged && r.sceneMatches(fr) &&
		r.lastBgSet && background == r.lastBg {
		damage, partial = gfx.DiffDamage(r.retained, dl, view, plan.Rects)
	}

	r.flushLayerCache()
	r.trimLayerPool()
	if !r.ensureScene(fr) {
		return FrameResult{Stopped: true}
	}
	r.retained = dl
	r.plan = plan
	r.lastBg, r.lastBgSet = background, true
	r.sceneScissor = nil
	r.count = 0

	if partial {
		r.Frames.Damage++
		if len(damage) == 0 && fr.SkipUnpainted && r.lastTargetSet && r.lastTarget == fr.Target {
			return FrameResult{}
		}
		for _, d := range damage {
			r.partialPass(dl.Cmds, d, background, dpr)
		}
		r.sceneScissor = nil
		r.blit(fr)
		return FrameResult{Painted: len(damage) > 0, Rects: len(damage)}
	}

	r.Frames.Full++
	scene := r.sceneTarget()
	targets := []target{scene}
	r.bindTarget(scene)
	r.dev.Clear(background.R, background.G, background.B, 1)
	r.execRange(dl.Cmds, 0, len(dl.Cmds), &targets, dpr)
	r.flushBatch(targets[len(targets)-1], nil)
	r.blit(fr)
	return FrameResult{Painted: true, Full: true}
}

func (r *Renderer) fits(fr Frame) bool {
	max := int32(r.dev.MaxTextureSize())
	if fr.DevW <= max && fr.DevH <= max {
		r.oversized = false
		return true
	}
	if !r.oversized {
		log.Printf("caution: %dx%d frame is past the GPU's %d px texture limit, not drawing", fr.DevW, fr.DevH, max)
	}
	r.oversized = true
	return false
}

func (r *Renderer) sceneMatches(fr Frame) bool {
	s := r.scene
	return s != nil && s.devW == fr.DevW && s.devH == fr.DevH &&
		s.viewW == fr.ViewW && s.viewH == fr.ViewH
}

func (r *Renderer) execRange(cmds []gfx.Cmd, start, end int, targets *[]target, dpr float32) {
	var beginIdxs []int
	for i := start; i < end; i++ {
		cmd := &cmds[i]
		switch cmd.Kind {
		case gfx.CmdLayerBegin:
			if span, ok := r.plan.Layers[i]; ok && span.Cacheable {
				if cached, hit := r.layerCache[i]; hit {
					r.compositeCached(&cmds[span.End], cached, (*targets)[len(*targets)-1])
					i = span.End
					continue
				}
			}
			r.flushBatch((*targets)[len(*targets)-1], nil)
			t, ok := r.acquireLayer(cmd.Rect, dpr)
			if !ok {
				if span, found := r.plan.Layers[i]; found {
					i = span.End
				}
				continue
			}
			*targets = append(*targets, t)
			beginIdxs = append(beginIdxs, i)
			r.bindTarget(t)
			r.dev.Clear(0, 0, 0, 0)
		case gfx.CmdLayerEnd:
			layer := (*targets)[len(*targets)-1]
			r.flushBatch(layer, nil)
			*targets = (*targets)[:len(*targets)-1]
			begin := -1
			if n := len(beginIdxs); n > 0 {
				begin = beginIdxs[n-1]
				beginIdxs = beginIdxs[:n-1]
			}
			parent := (*targets)[len(*targets)-1]
			r.bindTarget(parent)
			r.drawComposite(cmd, layer, parent)
			if layer.pooled != nil {
				if begin >= 0 && r.plan.Layers[begin].Cacheable {
					r.cacheLayer(begin, layer)
				} else {
					r.releaseLayer(layer.pooled)
				}
			}
		default:
			r.emitCmd(cmd, (*targets)[len(*targets)-1], dpr)
		}
	}
}

func (r *Renderer) emitCmd(cmd *gfx.Cmd, t target, dpr float32) {
	switch cmd.Kind {
	case gfx.CmdRect:
		r.emitRect(cmd)
	case gfx.CmdShadow:
		r.emitShadow(cmd)
	case gfx.CmdText:
		r.emitText(cmd, dpr)
	case gfx.CmdShaderQuad:
		r.flushBatch(t, nil)
		r.drawShaderQuad(cmd, t)
	case gfx.CmdImage:
		r.drawImage(cmd, t)
	}
}

func (r *Renderer) compositeCached(end *gfx.Cmd, cached, parent target) {
	r.flushBatch(parent, nil)
	r.drawComposite(end, cached, parent)
}

func (r *Renderer) AnimationIsInvisible() bool {
	return r.retained != nil && r.retained.WantsAnimation() &&
		!r.retained.GeomAnimation && len(r.plan.Rects) == 0
}

func (r *Renderer) RenderPartial(background gfx.Color, fr Frame) bool {
	if r.dev.Lost() || r.failed {
		return false
	}
	dl := r.retained
	if dl == nil || r.scene == nil || len(r.plan.Rects) == 0 || dl.GeomAnimation {
		return false
	}
	if !r.sceneMatches(fr) {
		return false
	}
	if float32(fr.DevW)/fr.ViewW != r.lastDPR {
		return false
	}
	r.Frames.Partial++
	r.trimLayerPool()
	r.time = fr.Time
	dpr := r.lastDPR
	r.count = 0
	for _, d := range r.plan.Rects {
		r.partialPass(dl.Cmds, d, background, dpr)
	}
	r.sceneScissor = nil
	r.blit(fr)
	return true
}

func (r *Renderer) partialPass(cmds []gfx.Cmd, d gfx.Rect, background gfx.Color, dpr float32) {
	scene := r.sceneTarget()
	x0 := max(0, int32(math.Floor(float64(d.X*dpr))))
	y0 := max(0, int32(math.Floor(float64(d.Y*dpr))))
	x1 := min(scene.devW, int32(math.Ceil(float64((d.X+d.W)*dpr))))
	y1 := min(scene.devH, int32(math.Ceil(float64((d.Y+d.H)*dpr))))
	if x1 <= x0 || y1 <= y0 {
		return
	}
	sc := [4]int32{x0, y0, x1 - x0, y1 - y0}
	r.sceneScissor = &sc
	targets := []target{scene}
	r.bindTarget(scene)
	// this is a draw because clears in explicit APIs ignore the scissor
	fill := gfx.R(-1, -1, scene.viewW+2, scene.viewH+2)
	opaque := background
	opaque.A = 1
	r.inst(instance{rect: fill, color: opaque, mode: modeSolid, clip: fill})
	for i := 0; i < len(cmds); i++ {
		cmd := &cmds[i]
		if cmd.Kind == gfx.CmdLayerBegin {
			span, ok := r.plan.Layers[i]
			if !ok {
				continue
			}
			if !gfx.Overlaps(cmd.Rect, d) {
				i = span.End
				continue
			}
			if span.Cacheable {
				if cached, hit := r.layerCache[i]; hit {
					r.compositeCached(&cmds[span.End], cached, scene)
					i = span.End
					continue
				}
			}
			r.execRange(cmds, i, span.End+1, &targets, dpr)
			i = span.End
			continue
		}
		if cmd.Kind == gfx.CmdLayerEnd {
			continue
		}
		if gfx.Overlaps(gfx.PaintedBounds(cmd), d) {
			r.emitCmd(cmd, scene, dpr)
		}
	}
	r.flushBatch(scene, nil)
}

func (r *Renderer) ensureScene(fr Frame) bool {
	s := r.scene
	if s != nil && s.devW == fr.DevW && s.devH == fr.DevH {
		s.viewW, s.viewH = fr.ViewW, fr.ViewH
		return true
	}
	if s != nil {
		r.dev.DeleteTexture(s.tex)
		r.scene = nil
	}
	tex, err := r.dev.NewTexture(int(fr.DevW), int(fr.DevH), gpu.Nearest)
	if err != nil {
		r.fail(err)
		return false
	}
	r.scene = &sceneFBO{tex: tex, devW: fr.DevW, devH: fr.DevH, viewW: fr.ViewW, viewH: fr.ViewH}
	return true
}

func (r *Renderer) sceneTarget() target {
	s := r.scene
	return target{tex: s.tex, viewW: s.viewW, viewH: s.viewH, devW: s.devW, devH: s.devH, flipY: r.dev.FlipY(false)}
}

// the whole scene is copied because the window's backbuffer is undefined after a swap
func (r *Renderer) blit(fr Frame) {
	r.lastTarget, r.lastTargetSet = fr.Target, true
	r.dev.SetScissor(nil)
	r.dev.SetTarget(fr.Target, int(fr.DevW), int(fr.DevH))
	r.dev.Copy(r.scene.tex)
}

func (r *Renderer) cacheLayer(begin int, t target) {
	if old, ok := r.layerCache[begin]; ok && old.pooled != nil && old.pooled != t.pooled {
		r.releaseLayer(old.pooled)
	}
	r.layerCache[begin] = t
}

func (r *Renderer) flushLayerCache() {
	for _, t := range r.layerCache {
		if t.pooled != nil {
			r.releaseLayer(t.pooled)
		}
	}
	clear(r.layerCache)
}

func (r *Renderer) drawImage(cmd *gfx.Cmd, t target) {
	if cmd.Rect.W <= 0 || cmd.Rect.H <= 0 || cmd.Clip.W <= 0 || cmd.Clip.H <= 0 {
		return
	}
	img := r.Images.get(cmd.Src)
	if img.state != imgReady {
		tint := imgPlaceholder
		if img.state == imgError {
			tint = imgErrorTint
		}
		r.inst(instance{rect: cmd.Rect, radii: cmd.Radii, color: tint, mode: modeRect, clip: cmd.Clip})
		return
	}
	r.flushBatch(t, nil)
	quad, u0, v0, u1, v1 := fitImage(cmd.Rect, img.w, img.h, cmd.Fit)
	r.inst(instance{rect: quad, radii: cmd.Radii, color: gfx.White, mode: modeTextured, uv: [4]float32{u0, v0, u1, v1}, clip: cmd.Clip})
	r.flushBatch(t, img.tex)
}

func (r *Renderer) flushBatch(t target, tex gpu.Texture) {
	if r.count == 0 {
		return
	}
	if y0, y1 := r.Atlas.TakeDirty(); y1 > y0 {
		const stride = text.AtlasSize * 4
		if err := r.dev.WriteTexture(r.tex, y0, r.Atlas.Pix[y0*stride:y1*stride]); err != nil {
			r.fail(err)
		}
	}
	r.prog.Set("u_view", t.viewW, t.viewH)
	r.prog.Set("u_origin", t.originX, t.originY)
	r.prog.Set("u_flipY", t.flipY)
	if tex == nil {
		tex = r.tex
	}
	r.dev.Draw(r.prog, tex, r.data[:r.count*floatsPerInstance], r.count)
	r.count = 0
}

func (r *Renderer) bindTarget(t target) {
	r.cur = t
	r.dev.SetTarget(t.tex, int(t.devW), int(t.devH))
	if r.sceneScissor != nil && t.tex == r.scene.tex {
		r.dev.SetScissor(r.sceneScissor)
	} else {
		r.dev.SetScissor(nil)
	}
}

func (r *Renderer) acquireLayer(rect gfx.Rect, dpr float32) (target, bool) {
	limit := float64(r.dev.MaxTextureSize())
	devW := int32(math.Min(limit, math.Max(1, math.Round(float64(rect.W*dpr)))))
	devH := int32(math.Min(limit, math.Max(1, math.Round(float64(rect.H*dpr)))))
	key := fmt.Sprintf("%dx%d", devW, devH)
	var pooled *pooledLayer
	if list := r.layerPool[key]; len(list) > 0 {
		pooled = list[len(list)-1]
		r.layerPool[key] = list[:len(list)-1]
		r.poolBytes -= pooled.bytes
	} else {
		tex, err := r.dev.NewTexture(int(devW), int(devH), gpu.Linear)
		if err != nil {
			r.fail(err)
			return target{}, false
		}
		pooled = &pooledLayer{key: key, bytes: int(devW) * int(devH) * 4, tex: tex}
	}
	return target{
		tex: pooled.tex, originX: rect.X, originY: rect.Y,
		viewW: rect.W, viewH: rect.H, devW: devW, devH: devH,
		flipY:  r.dev.FlipY(true),
		pooled: pooled,
	}, true
}

func (r *Renderer) releaseLayer(p *pooledLayer) {
	p.released = r.frameNo
	r.layerPool[p.key] = append(r.layerPool[p.key], p)
	r.poolBytes += p.bytes
}

func (r *Renderer) clearLayerPool() {
	for _, list := range r.layerPool {
		for _, p := range list {
			r.dev.DeleteTexture(p.tex)
		}
	}
	clear(r.layerPool)
	r.poolBytes = 0
}

func (r *Renderer) trimLayerPool() {
	r.frameNo++
	if r.poolBytes == 0 {
		return
	}
	var kept []*pooledLayer
	for key, list := range r.layerPool {
		live := list[:0]
		for _, p := range list {
			if r.frameNo-p.released > layerIdleFrames {
				r.dev.DeleteTexture(p.tex)
				r.poolBytes -= p.bytes
			} else {
				live = append(live, p)
			}
		}
		if len(live) == 0 {
			delete(r.layerPool, key)
			continue
		}
		r.layerPool[key] = live
		kept = append(kept, live...)
	}
	if r.poolBytes <= maxPoolBytes {
		return
	}
	slices.SortFunc(kept, func(a, b *pooledLayer) int { return cmp.Compare(a.released, b.released) })
	for _, p := range kept {
		if r.poolBytes <= maxPoolBytes {
			break
		}
		list := r.layerPool[p.key]
		r.layerPool[p.key] = slices.DeleteFunc(list, func(q *pooledLayer) bool { return q == p })
		r.dev.DeleteTexture(p.tex)
		r.poolBytes -= p.bytes
	}
}

func (r *Renderer) drawComposite(cmd *gfx.Cmd, layer, parent target) {
	up := r.userProgram(cmd.Frag, uniformNames(cmd.Uniforms), true)
	if up == nil {
		up = r.userProgram(passthroughFrag, nil, true)
	}
	if up == nil {
		return
	}
	r.setQuadUniforms(up, cmd.Rect, [4]float32{0, 0, 1, 1}, parent)
	up.pipe.Set("u_res", cmd.Rect.W, cmd.Rect.H)
	r.setCustomUniforms(up, cmd.Uniforms)
	r.dev.Draw(up.pipe, layer.pooled.tex, nil, 1)
}

func (r *Renderer) drawShaderQuad(cmd *gfx.Cmd, t target) {
	up := r.userProgram(cmd.Frag, uniformNames(cmd.Uniforms), false)
	if up == nil {
		return
	}
	r.setQuadUniforms(up, cmd.Rect, cmd.UV, t)
	up.pipe.Set("u_res", cmd.Rect.W, cmd.Rect.H)
	r.setCustomUniforms(up, cmd.Uniforms)
	r.dev.Draw(up.pipe, nil, nil, 1)
}

func (r *Renderer) setQuadUniforms(up *userProgram, rect gfx.Rect, uv [4]float32, t target) {
	p := up.pipe
	p.Set("u_rect", rect.X, rect.Y, rect.W, rect.H)
	p.Set("u_uvrect", uv[0], uv[1], uv[2], uv[3])
	p.Set("u_view", t.viewW, t.viewH)
	p.Set("u_origin", t.originX, t.originY)
	p.Set("u_flipY", t.flipY)
	p.Set("u_time", r.time)
}

func (r *Renderer) setCustomUniforms(up *userProgram, uniforms map[string]float32) {
	for k, v := range uniforms {
		up.pipe.Set(k, v)
	}
}

func uniformNames(uniforms map[string]float32) []string {
	if len(uniforms) == 0 {
		return nil
	}
	names := make([]string, 0, len(uniforms))
	for k := range uniforms {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func (r *Renderer) userProgram(frag string, names []string, withContent bool) *userProgram {
	kind := "px"
	if withContent {
		kind = "fx"
	}
	key := kind + "\x00" + strings.Join(names, ",") + "\x00" + frag
	r.progTick++
	if up, ok := r.userProgs[key]; ok {
		if up != nil {
			up.used = r.progTick
		}
		return up
	}
	if len(r.userProgs) >= maxUserProgs {
		oldestKey, oldest := "", uint64(math.MaxUint64)
		for k, p := range r.userProgs {
			t := uint64(0)
			if p != nil {
				t = p.used
			}
			if t < oldest {
				oldest, oldestKey = t, k
			}
		}
		if old := r.userProgs[oldestKey]; old != nil {
			r.dev.DeletePipeline(old.pipe)
		}
		delete(r.userProgs, oldestKey)
	}
	var up *userProgram
	pipe, err := r.dev.NewPipeline(quadVertSrc, wrapUserFrag(frag, names, withContent), false)
	if err != nil {
		log.Println("caution: custom shader failed to compile:", err)
	} else {
		up = &userProgram{pipe: pipe, used: r.progTick}
	}
	r.userProgs[key] = up
	return up
}

func (r *Renderer) emitRect(cmd *gfx.Cmd) {
	if cmd.Rect.W <= 0 || cmd.Rect.H <= 0 || cmd.Clip.W <= 0 || cmd.Clip.H <= 0 {
		return
	}
	r.inst(instance{rect: cmd.Rect, radii: cmd.Radii, color: cmd.Color, border: cmd.Border, borderWidth: cmd.BorderWidth, mode: modeRect, clip: cmd.Clip})
}

func (r *Renderer) emitShadow(cmd *gfx.Cmd) {
	if cmd.Rect.W <= 0 || cmd.Rect.H <= 0 || cmd.Clip.W <= 0 || cmd.Clip.H <= 0 {
		return
	}
	r.inst(instance{rect: cmd.Rect, radii: cmd.Radii, color: cmd.Color, mode: modeShadow, blur: cmd.Blur, clip: cmd.Clip})
}

func (r *Renderer) emitText(cmd *gfx.Cmd, dpr float32) {
	if cmd.Clip.W <= 0 || cmd.Clip.H <= 0 {
		return
	}
	run := r.Shaper.Shape(cmd.Font, dpr, cmd.Text)
	baseline := cmd.Y + run.Ascent
	penY := float32(math.Round(float64(baseline * dpr)))
	for _, g := range run.Glyphs {
		xDev := (cmd.X + g.X) * dpr
		penX := float32(math.Floor(float64(xDev)))
		phase := int(math.Round(float64(xDev-penX) * text.Phases))
		if phase >= text.Phases {
			penX++
			phase = 0
		}
		glyph := r.Atlas.Get(g.Face, cmd.Font.Size, dpr, g.GID, phase)
		if glyph == nil {
			continue
		}
		quad := gfx.Rect{
			X: (penX + float32(glyph.BX)) / dpr,
			Y: (penY + float32(glyph.BY)) / dpr,
			W: float32(glyph.W) / dpr,
			H: float32(glyph.H) / dpr,
		}
		color := cmd.Color
		if glyph.Colored {
			color = gfx.White
		}
		r.inst(instance{rect: quad, color: color, mode: modeTextured, clip: cmd.Clip, uv: [4]float32{
			float32(glyph.X0) / text.AtlasSize, float32(glyph.Y0) / text.AtlasSize,
			float32(glyph.X1) / text.AtlasSize, float32(glyph.Y1) / text.AtlasSize,
		}})
	}
}

type instance struct {
	rect        gfx.Rect
	radii       gfx.Corners
	color       gfx.Color
	border      gfx.Color
	borderWidth float32
	mode        int
	blur        float32
	uv          [4]float32
	clip        gfx.Rect
}

func (r *Renderer) inst(in instance) {
	if (r.count+1)*floatsPerInstance > len(r.data) {
		grown := make([]float32, len(r.data)*2)
		copy(grown, r.data)
		r.data = grown
	}
	d := r.data
	o := r.count * floatsPerInstance
	d[o+0], d[o+1], d[o+2], d[o+3] = in.rect.X, in.rect.Y, in.rect.W, in.rect.H
	d[o+4], d[o+5], d[o+6], d[o+7] = in.radii.TL, in.radii.TR, in.radii.BR, in.radii.BL
	d[o+8], d[o+9], d[o+10], d[o+11] = in.color.R, in.color.G, in.color.B, in.color.A
	d[o+12], d[o+13], d[o+14], d[o+15] = in.border.R, in.border.G, in.border.B, in.border.A
	d[o+16], d[o+17], d[o+18], d[o+19] = in.borderWidth, float32(in.mode), in.blur, 0
	d[o+20], d[o+21], d[o+22], d[o+23] = in.uv[0], in.uv[1], in.uv[2], in.uv[3]
	d[o+24], d[o+25], d[o+26], d[o+27] = in.clip.X, in.clip.Y, in.clip.W, in.clip.H
	r.count++
}

func (r *Renderer) PoolSizes() (pooledLayers, pooledBytes, userPrograms int) {
	for _, ls := range r.layerPool {
		pooledLayers += len(ls)
	}
	return pooledLayers, r.poolBytes, len(r.userProgs)
}
