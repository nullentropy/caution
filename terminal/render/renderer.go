package render

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/text"

	"github.com/nullentropy/caution/terminal/glx"
)

const floatsPerInstance = 28 // 7 x vec4

const (
	modeRect     = 0
	modeShadow   = 1
	modeTextured = 2
)

var (
	noCorners      = gfx.Corners{}
	imgPlaceholder = gfx.RGBA(0.5, 0.55, 0.6, 0.08)
	imgErrorTint   = gfx.RGBA(0.9, 0.3, 0.3, 0.12)
)

// the window, or a pooled offscreen layer
type target struct {
	fbo              uint32
	originX, originY float32
	viewW, viewH     float32
	devW, devH       int32
	flipY            float32
	pooled           *pooledLayer
}

type pooledLayer struct {
	fbo uint32
	tex uint32
	key string
}

type userProgram struct {
	prog uint32
	locs map[string]int32
	used uint64 // last-access tick, for LRU eviction (see maxUserProgs)
}

// compiled-shader cache cap. every unique frag source keeps a GL program, so
// an app swapping shader source as its state changes would grow it forever
// the per-frame working set is a handful, so this sits far above it and the
// LRU program is deleted on overflow
const maxUserProgs = 48

// root render target for one frame
type Frame struct {
	// logical size, what widget coordinates are in
	ViewW, ViewH float32
	// device pixels, framebuffer size
	DevW, DevH int32
	// 0 is the window's default framebuffer, anything else an offscreen
	// root (-shot)
	FBO  uint32
	Time float32
	// skip even the blit when no visible pixel changed, saves a full-frame GPU
	// copy for a caller patched faster than it can present. only set it if the
	// caller won't present a frame with FrameResult.Painted false, since the
	// skipped destination holds whatever it held and after a swap that's undefined
	SkipUnpainted bool
}

// the GL context must be current on the calling thread for every method
type Renderer struct {
	Shaper *text.Shaper
	Atlas  *text.Atlas
	Images *ImageStore

	prog             uint32
	vao, quadVao     uint32
	quadBuf, instBuf uint32
	tex              uint32
	uView            int32
	uOrigin          int32
	uFlipY           int32

	data      []float32
	count     int
	instCap   int // bytes allocated in instBuf (GPU side)
	lastDPR   float32
	time      float32
	layerPool map[string][]*pooledLayer
	userProgs map[string]*userProgram
	progTick  uint64 // monotonic access clock for userProgs LRU
	// logical size the pooled layers were sized for. the pool is keyed by pixel
	// size, so without freeing on resize every intermediate size of a drag leaks
	// its full-screen textures
	lastViewW, lastViewH float32

	// -- partial invalidation (see RenderPartial) --
	// every frame renders into scene (persistent offscreen framebuffer) and
	// blits to the destination. scene keeps last frame's pixels so scissored
	// repaints work, the window's backbuffer is undefined after a swap
	scene *sceneFBO
	// last rendered list, its damage plan and its frame-invariant layer
	// textures
	retained     *gfx.DisplayList
	plan         gfx.DamagePlan
	layerCache   map[int]target
	sceneScissor *[4]int32 // device px, GL bottom-origin; nil = off
	// a background change repaints everything and the display list doesn't
	// record it
	lastBg    gfx.Color
	lastBgSet bool
	// skipping the blit on an untouched frame is only safe when the destination
	// already holds those pixels, a different target holds something else or
	// nothing
	lastFBO    uint32
	lastFBOSet bool
	// how each kind of frame got serviced
	Frames FrameCounts
}

// for a shell deciding whether presenting is worth it
type FrameResult struct {
	// false only when the frame touched nothing: empty diff, no scissored pass,
	// scene still holds the previous image byte for byte. a caller can skip its
	// swap. under Frame.SkipUnpainted the blit is skipped too, so honoring this
	// is required, the destination wasn't written
	//
	// empty damage doesn't mean the lists were identical, only that every
	// difference was invisible, clipped or off the viewport. the new list is
	// still retained
	Painted bool
	// scissored regions repainted. Full means the whole frame was rebuilt
	// because the diff was declined (see Render)
	Rects int
	Full  bool
}

// the only way to tell from outside which path a frame took, the
// pixel-identity probes assert on it
type FrameCounts struct {
	// Full rebuilt every pixel. Damage repainted only what differs from the
	// retained list. Partial replayed retained animation damage, no tree walk
	Full, Damage, Partial int
}

// persistent whole-frame render target
type sceneFBO struct {
	fbo, tex     uint32
	devW, devH   int32
	viewW, viewH float32
}

func compile(xtype uint32, src string) (uint32, error) {
	sh := glx.CreateShader(xtype)
	csources, free := glx.Strs(src + "\x00")
	glx.ShaderSource(sh, 1, csources, nil)
	free()
	glx.CompileShader(sh)
	var status int32
	glx.GetShaderiv(sh, glx.COMPILE_STATUS, &status)
	if status == glx.FALSE {
		var n int32
		glx.GetShaderiv(sh, glx.INFO_LOG_LENGTH, &n)
		infoLog := strings.Repeat("\x00", int(n)+1)
		glx.GetShaderInfoLog(sh, n, nil, glx.Str(infoLog))
		glx.DeleteShader(sh)
		return 0, fmt.Errorf("shader compile failed: %s", strings.TrimRight(infoLog, "\x00"))
	}
	return sh, nil
}

func link(vert, frag string) (uint32, error) {
	vs, err := compile(glx.VERTEX_SHADER, vert)
	if err != nil {
		return 0, err
	}
	fs, err := compile(glx.FRAGMENT_SHADER, frag)
	if err != nil {
		glx.DeleteShader(vs)
		return 0, err
	}
	prog := glx.CreateProgram()
	glx.AttachShader(prog, vs)
	glx.AttachShader(prog, fs)
	glx.LinkProgram(prog)
	glx.DeleteShader(vs)
	glx.DeleteShader(fs)
	var status int32
	glx.GetProgramiv(prog, glx.LINK_STATUS, &status)
	if status == glx.FALSE {
		var n int32
		glx.GetProgramiv(prog, glx.INFO_LOG_LENGTH, &n)
		infoLog := strings.Repeat("\x00", int(n)+1)
		glx.GetProgramInfoLog(prog, n, nil, glx.Str(infoLog))
		glx.DeleteProgram(prog)
		return 0, fmt.Errorf("program link failed: %s", strings.TrimRight(infoLog, "\x00"))
	}
	return prog, nil
}

func uniform(prog uint32, name string) int32 {
	return glx.GetUniformLocation(prog, glx.Str(name+"\x00"))
}

// GL context must be current and glx.Init() done
func New() (*Renderer, error) {
	r := &Renderer{
		Shaper:     text.NewShaper(),
		Atlas:      text.NewAtlas(),
		Images:     newImageStore(),
		data:       make([]float32, 4096*floatsPerInstance),
		layerPool:  make(map[string][]*pooledLayer),
		userProgs:  make(map[string]*userProgram),
		layerCache: make(map[int]target),
	}

	prog, err := link(shaderSrc(vertSrc, false), shaderSrc(fragSrc, true))
	if err != nil {
		return nil, err
	}
	r.prog = prog
	r.uView = uniform(prog, "u_view")
	r.uOrigin = uniform(prog, "u_origin")
	r.uFlipY = uniform(prog, "u_flipY")

	glx.GenVertexArrays(1, &r.vao)
	glx.BindVertexArray(r.vao)

	glx.GenBuffers(1, &r.quadBuf)
	glx.BindBuffer(glx.ARRAY_BUFFER, r.quadBuf)
	unit := []float32{0, 0, 1, 0, 0, 1, 1, 1}
	glx.BufferData(glx.ARRAY_BUFFER, len(unit)*4, glx.Ptr(unit), glx.STATIC_DRAW)
	glx.EnableVertexAttribArray(0)
	glx.VertexAttribPointerWithOffset(0, 2, glx.FLOAT, false, 0, 0)

	glx.GenBuffers(1, &r.instBuf)
	glx.BindBuffer(glx.ARRAY_BUFFER, r.instBuf)
	// streaming capacity allocated once, flushes orphan and SubData into it
	r.instCap = len(r.data) * 4
	glx.BufferData(glx.ARRAY_BUFFER, r.instCap, nil, glx.DYNAMIC_DRAW)
	for i := uint32(0); i < 7; i++ {
		glx.EnableVertexAttribArray(1 + i)
		glx.VertexAttribPointerWithOffset(1+i, 4, glx.FLOAT, false, floatsPerInstance*4, uintptr(i*16))
		glx.VertexAttribDivisor(1+i, 1)
	}
	glx.BindVertexArray(0)

	// plain unit quad for custom-shader passes, no instancing
	glx.GenVertexArrays(1, &r.quadVao)
	glx.BindVertexArray(r.quadVao)
	glx.BindBuffer(glx.ARRAY_BUFFER, r.quadBuf)
	glx.EnableVertexAttribArray(0)
	glx.VertexAttribPointerWithOffset(0, 2, glx.FLOAT, false, 0, 0)
	glx.BindVertexArray(0)

	glx.GenTextures(1, &r.tex)
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, r.tex)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MIN_FILTER, glx.LINEAR)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MAG_FILTER, glx.LINEAR)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_S, glx.CLAMP_TO_EDGE)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_T, glx.CLAMP_TO_EDGE)
	glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, text.AtlasSize, text.AtlasSize, 0, glx.RGBA, glx.UNSIGNED_BYTE, nil)

	glx.Disable(glx.DEPTH_TEST)
	glx.Enable(glx.BLEND)
	glx.BlendFunc(glx.ONE, glx.ONE_MINUS_SRC_ALPHA) // premultiplied

	glx.UseProgram(prog)
	glx.Uniform1i(uniform(prog, "u_tex"), 0)
	return r, nil
}

// paints into the persistent scene framebuffer and blits to fr.FBO. when the
// new list provably differs from the retained one in a few places (a server
// op touching one label, a hover, a caret blink) only those regions repaint
// (gfx.DiffDamage), otherwise every pixel is rebuilt. either way the list is
// retained with its damage plan and layer textures so pure animation can go
// through RenderPartial next
//
// an empty diff does no GL work and reports Painted false. the result is
// safe to ignore
func (r *Renderer) Render(dl *gfx.DisplayList, background gfx.Color, fr Frame) FrameResult {
	r.time = fr.Time
	imagesChanged := r.Images.drainUploads()
	dpr := float32(fr.DevW) / fr.ViewW
	dprChanged := dpr != r.lastDPR
	if dprChanged {
		// glyphs are rasterized at device-pixel size, so a DPR change
		// invalidates them all, and the pooled layer textures too
		if r.lastDPR != 0 {
			r.Atlas.Reset()
			r.flushLayerCache()
			r.clearLayerPool()
		}
		r.lastDPR = dpr
	}
	// a resize orphans every pooled layer texture at the old size. free them or
	// a resize drag (hundreds of intermediate sizes) leaks a full-screen texture
	// per size
	if fr.ViewW != r.lastViewW || fr.ViewH != r.lastViewH {
		if r.lastViewW != 0 {
			r.flushLayerCache()
			r.clearLayerPool()
		}
		r.lastViewW, r.lastViewH = fr.ViewW, fr.ViewH
	}
	view := gfx.R(0, 0, fr.ViewW, fr.ViewH)
	plan := gfx.PlanDamage(dl, view)
	// rule out what the display list can't tell us before trusting a diff: same
	// scene surface, glyphs still where they were, no image finished loading
	// (same command, different texture), same clear color
	var damage []gfx.Rect
	partial := false
	if !dprChanged && !imagesChanged && r.sceneMatches(fr) &&
		r.lastBgSet && background == r.lastBg {
		damage, partial = gfx.DiffDamage(r.retained, dl, view, plan.Rects)
	}

	r.flushLayerCache() // cached textures belong to the outgoing list
	r.ensureScene(fr)
	r.retained = dl
	r.plan = plan
	r.lastBg, r.lastBgSet = background, true
	r.sceneScissor = nil
	r.count = 0

	if partial {
		r.Frames.Damage++
		if len(damage) == 0 && fr.SkipUnpainted && r.lastFBOSet && r.lastFBO == fr.FBO {
			// nothing visible differs. the scene is untouched (no pass ran and the
			// partial path never clears outside a scissor) so the destination
			// still holds these pixels
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
	glx.ClearColor(background.R, background.G, background.B, 1)
	glx.Clear(glx.COLOR_BUFFER_BIT)
	r.execRange(dl.Cmds, 0, len(dl.Cmds), &targets, dpr)
	r.flushBatch(targets[len(targets)-1], 0)
	r.blit(fr)
	return FrameResult{Painted: true, Full: true}
}

// false when the scene is about to be reallocated, its contents are then
// undefined and nothing can be kept
func (r *Renderer) sceneMatches(fr Frame) bool {
	s := r.scene
	return s != nil && s.devW == fr.DevW && s.devH == fr.DevH &&
		s.viewW == fr.ViewW && s.viewH == fr.ViewH
}

// the full-render body, also used by partial passes for layer ranges that
// re-render live. a finished cacheable layer keeps its texture for partial
// replay instead of going back to the pool, layers render unscissored on
// either path so the texture is just as good
func (r *Renderer) execRange(cmds []gfx.Cmd, start, end int, targets *[]target, dpr float32) {
	var beginIdxs []int
	for i := start; i < end; i++ {
		cmd := &cmds[i]
		switch cmd.Kind {
		case gfx.CmdRect:
			r.emitRect(cmd)
		case gfx.CmdShadow:
			r.emitShadow(cmd)
		case gfx.CmdText:
			r.emitText(cmd, dpr)
		case gfx.CmdLayerBegin:
			r.flushBatch((*targets)[len(*targets)-1], 0)
			t := r.acquireLayer(cmd.Rect, dpr)
			*targets = append(*targets, t)
			beginIdxs = append(beginIdxs, i)
			r.bindTarget(t)
			glx.ClearColor(0, 0, 0, 0)
			glx.Clear(glx.COLOR_BUFFER_BIT)
		case gfx.CmdLayerEnd:
			layer := (*targets)[len(*targets)-1]
			r.flushBatch(layer, 0)
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
					r.layerCache[begin] = layer
				} else {
					r.releaseLayer(layer.pooled)
				}
			}
		case gfx.CmdShaderQuad:
			t := (*targets)[len(*targets)-1]
			r.flushBatch(t, 0)
			r.drawShaderQuad(cmd, t)
		case gfx.CmdImage:
			r.drawImage(cmd, (*targets)[len(*targets)-1])
		}
	}
}

// the retained frame wants animation but has no damage in the viewport, say
// an animated effect layer scrolled out of view (EndLayer asks for animation
// whether or not its rect survives clipping). the shell skips such a frame
// outright: no build, diff, blit, swap or event-loop wake
func (r *Renderer) AnimationIsInvisible() bool {
	return r.retained != nil && r.retained.WantsAnimation() &&
		!r.retained.GeomAnimation && len(r.plan.Rects) == 0
}

// pure animation continuation: repaints the retained animated damage rects
// and blits, no tree walk, no list build, clean pixels untouched
// false when it can't: nothing retained, geometric animation mid-flight
// (bounds move every frame), or size/DPR changed under the retained list
// empty animation damage is false too, but the shell checks
// AnimationIsInvisible first and skips the frame instead of calling Render
func (r *Renderer) RenderPartial(background gfx.Color, fr Frame) bool {
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

// scissor snapped outward around d, clear to background, then replay the
// commands whose painted extent intersects it in the same order a full frame
// would, so pixels inside come out identical. cmds is the retained list for
// animation, the fresh one for real damage
// layers render in full here (no scissor inside a layer target), so a
// frame-invariant one is cached like a full frame would and the next pass
// just composites the texture
func (r *Renderer) partialPass(cmds []gfx.Cmd, d gfx.Rect, background gfx.Color, dpr float32) {
	scene := r.sceneTarget()
	x0 := max(0, int32(math.Floor(float64(d.X*dpr))))
	y0 := max(0, int32(math.Floor(float64(d.Y*dpr))))
	x1 := min(scene.devW, int32(math.Ceil(float64((d.X+d.W)*dpr))))
	y1 := min(scene.devH, int32(math.Ceil(float64((d.Y+d.H)*dpr))))
	if x1 <= x0 || y1 <= y0 {
		return
	}
	// GL scissor is bottom-origin, the scene renders window-oriented
	sc := [4]int32{x0, scene.devH - y1, x1 - x0, y1 - y0}
	r.sceneScissor = &sc
	targets := []target{scene}
	r.bindTarget(scene)
	glx.ClearColor(background.R, background.G, background.B, 1)
	glx.Clear(glx.COLOR_BUFFER_BIT)
	for i := 0; i < len(cmds); i++ {
		cmd := &cmds[i]
		if cmd.Kind == gfx.CmdLayerBegin {
			span, ok := r.plan.Layers[i]
			if !ok {
				continue // unbalanced list, PaintTree can't produce this
			}
			if !gfx.Overlaps(cmd.Rect, d) {
				i = span.End // the whole subtree misses this pass
				continue
			}
			if span.Cacheable {
				if cached, hit := r.layerCache[i]; hit {
					// frame-invariant, composite the retained texture (fresh
					// u_time still applies, the CRT case costs one quad). flush
					// first, the batch holds commands from *below* the composite
					r.flushBatch(scene, 0)
					r.drawComposite(&cmds[span.End], cached, scene)
					i = span.End
					continue
				}
			}
			// animates, re-render live. bindTarget drops the scissor inside
			// layer targets and re-applies it when the composite lands back
			// on the scene
			r.execRange(cmds, i, span.End+1, &targets, dpr)
			i = span.End
			continue
		}
		if cmd.Kind == gfx.CmdLayerEnd {
			continue // consumed by the ranges above
		}
		if !gfx.Overlaps(gfx.PaintedBounds(cmd), d) {
			continue
		}
		switch cmd.Kind {
		case gfx.CmdRect:
			r.emitRect(cmd)
		case gfx.CmdShadow:
			r.emitShadow(cmd)
		case gfx.CmdText:
			r.emitText(cmd, dpr)
		case gfx.CmdShaderQuad:
			r.flushBatch(scene, 0)
			r.drawShaderQuad(cmd, scene)
		case gfx.CmdImage:
			r.drawImage(cmd, scene)
		}
	}
	r.flushBatch(scene, 0)
}

// at the frame's device size
func (r *Renderer) ensureScene(fr Frame) {
	s := r.scene
	if s != nil && s.devW == fr.DevW && s.devH == fr.DevH {
		s.viewW, s.viewH = fr.ViewW, fr.ViewH
		return
	}
	if s != nil {
		glx.DeleteFramebuffers(1, &s.fbo)
		glx.DeleteTextures(1, &s.tex)
	}
	s = &sceneFBO{devW: fr.DevW, devH: fr.DevH, viewW: fr.ViewW, viewH: fr.ViewH}
	glx.GenTextures(1, &s.tex)
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, s.tex)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MIN_FILTER, glx.NEAREST)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MAG_FILTER, glx.NEAREST)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_S, glx.CLAMP_TO_EDGE)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_T, glx.CLAMP_TO_EDGE)
	glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, s.devW, s.devH, 0, glx.RGBA, glx.UNSIGNED_BYTE, nil)
	glx.GenFramebuffers(1, &s.fbo)
	glx.BindFramebuffer(glx.FRAMEBUFFER, s.fbo)
	glx.FramebufferTexture2D(glx.FRAMEBUFFER, glx.COLOR_ATTACHMENT0, glx.TEXTURE_2D, s.tex, 0)
	r.scene = s
}

// window-oriented (flipY -1) so the blit needs no flip and shot captures
// keep their row order
func (r *Renderer) sceneTarget() target {
	s := r.scene
	return target{fbo: s.fbo, viewW: s.viewW, viewH: s.viewH, devW: s.devW, devH: s.devH, flipY: -1}
}

// to the window backbuffer or a shot FBO, whole frame, deliberately no
// scissor
func (r *Renderer) blit(fr Frame) {
	r.lastFBO, r.lastFBOSet = fr.FBO, true
	glx.Disable(glx.SCISSOR_TEST)
	glx.BindFramebuffer(glx.READ_FRAMEBUFFER, r.scene.fbo)
	glx.BindFramebuffer(glx.DRAW_FRAMEBUFFER, fr.FBO)
	glx.BlitFramebuffer(0, 0, r.scene.devW, r.scene.devH, 0, 0, fr.DevW, fr.DevH, glx.COLOR_BUFFER_BIT, glx.NEAREST)
	glx.BindFramebuffer(glx.FRAMEBUFFER, fr.FBO)
}

// the retained list they were rendered for is going away
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
		r.inst(cmd.Rect, cmd.Radii, tint, tint, 0, modeRect, 0, 0, 0, 0, 0, cmd.Clip)
		return
	}
	// the image is its own texture, so its own single-instance draw
	r.flushBatch(t, 0)
	quad, u0, v0, u1, v1 := fitImage(cmd.Rect, img.w, img.h, cmd.Fit)
	r.inst(quad, cmd.Radii, gfx.White, gfx.White, 0, modeTextured, 0, u0, v0, u1, v1, cmd.Clip)
	r.flushBatch(t, img.tex)
}

func (r *Renderer) flushBatch(t target, tex uint32) {
	if r.count == 0 {
		return
	}
	if r.Atlas.Dirty {
		glx.ActiveTexture(glx.TEXTURE0)
		glx.BindTexture(glx.TEXTURE_2D, r.tex)
		glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, text.AtlasSize, text.AtlasSize, 0,
			glx.RGBA, glx.UNSIGNED_BYTE, glx.Ptr(r.Atlas.Pix))
		r.Atlas.Dirty = false
	}
	glx.UseProgram(r.prog)
	glx.Uniform2f(r.uView, t.viewW, t.viewH)
	glx.Uniform2f(r.uOrigin, t.originX, t.originY)
	glx.Uniform1f(r.uFlipY, t.flipY)
	glx.ActiveTexture(glx.TEXTURE0)
	if tex != 0 {
		glx.BindTexture(glx.TEXTURE_2D, tex)
	} else {
		glx.BindTexture(glx.TEXTURE_2D, r.tex)
	}
	glx.BindVertexArray(r.vao)
	glx.BindBuffer(glx.ARRAY_BUFFER, r.instBuf)
	// orphan + SubData at a constant size. BufferData per flush at varying sizes
	// makes the driver allocate and copy every flush, which profiled as the
	// dominant per-frame GL cost. orphaning gives a fresh region of the same
	// allocation and SubData moves only this batch's bytes without waiting on
	// draws still reading the old region
	size := r.count * floatsPerInstance * 4
	if size > r.instCap {
		r.instCap = len(r.data) * 4 // the CPU slice grew (inst), follow it
	}
	glx.BufferData(glx.ARRAY_BUFFER, r.instCap, nil, glx.DYNAMIC_DRAW)
	glx.BufferSubData(glx.ARRAY_BUFFER, 0, size, glx.Ptr(r.data))
	glx.DrawArraysInstanced(glx.TRIANGLE_STRIP, 0, 4, int32(r.count))
	glx.BindVertexArray(0)
	r.count = 0
}

// also owns the scissor. a partial pass's scissor only applies while drawing
// the scene itself, offscreen layers render in full and their composite is
// clipped on the way back
func (r *Renderer) bindTarget(t target) {
	glx.BindFramebuffer(glx.FRAMEBUFFER, t.fbo)
	glx.Viewport(0, 0, t.devW, t.devH)
	if r.sceneScissor != nil && t.fbo == r.scene.fbo {
		sc := *r.sceneScissor
		glx.Enable(glx.SCISSOR_TEST)
		glx.Scissor(sc[0], sc[1], sc[2], sc[3])
	} else {
		glx.Disable(glx.SCISSOR_TEST)
	}
}

// -- offscreen layers -----------------------------------------------------------

func (r *Renderer) acquireLayer(rect gfx.Rect, dpr float32) target {
	devW := int32(math.Max(1, math.Round(float64(rect.W*dpr))))
	devH := int32(math.Max(1, math.Round(float64(rect.H*dpr))))
	key := fmt.Sprintf("%dx%d", devW, devH)
	var pooled *pooledLayer
	if list := r.layerPool[key]; len(list) > 0 {
		pooled = list[len(list)-1]
		r.layerPool[key] = list[:len(list)-1]
	} else {
		pooled = &pooledLayer{key: key}
		glx.GenTextures(1, &pooled.tex)
		glx.ActiveTexture(glx.TEXTURE0)
		glx.BindTexture(glx.TEXTURE_2D, pooled.tex)
		glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MIN_FILTER, glx.LINEAR)
		glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MAG_FILTER, glx.LINEAR)
		glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_S, glx.CLAMP_TO_EDGE)
		glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_T, glx.CLAMP_TO_EDGE)
		glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, devW, devH, 0, glx.RGBA, glx.UNSIGNED_BYTE, nil)
		glx.GenFramebuffers(1, &pooled.fbo)
		glx.BindFramebuffer(glx.FRAMEBUFFER, pooled.fbo)
		glx.FramebufferTexture2D(glx.FRAMEBUFFER, glx.COLOR_ATTACHMENT0, glx.TEXTURE_2D, pooled.tex, 0)
	}
	return target{
		fbo: pooled.fbo, originX: rect.X, originY: rect.Y,
		viewW: rect.W, viewH: rect.H, devW: devW, devH: devH,
		flipY:  1, // texture row 0 = layer top, so effects sample uv directly
		pooled: pooled,
	}
}

func (r *Renderer) releaseLayer(p *pooledLayer) {
	r.layerPool[p.key] = append(r.layerPool[p.key], p)
}

func (r *Renderer) clearLayerPool() {
	for _, list := range r.layerPool {
		for _, p := range list {
			glx.DeleteFramebuffers(1, &p.fbo)
			glx.DeleteTextures(1, &p.tex)
		}
	}
	clear(r.layerPool)
}

// -- custom shader passes ----------------------------------------------------------

func (r *Renderer) drawComposite(cmd *gfx.Cmd, layer, parent target) {
	up := r.userProgram(cmd.Frag, uniformNames(cmd.Uniforms), true)
	if up == nil {
		up = r.userProgram(passthroughFrag, nil, true)
	}
	if up == nil {
		return
	}
	glx.UseProgram(up.prog)
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, layer.pooled.tex)
	r.setQuadUniforms(up, cmd.Rect, [4]float32{0, 0, 1, 1}, parent)
	glx.Uniform1i(up.locs["u_tex"], 0)
	glx.Uniform2f(up.locs["u_res"], cmd.Rect.W, cmd.Rect.H)
	r.setCustomUniforms(up, cmd.Uniforms)
	glx.BindVertexArray(r.quadVao)
	glx.DrawArrays(glx.TRIANGLE_STRIP, 0, 4)
	glx.BindVertexArray(0)
}

func (r *Renderer) drawShaderQuad(cmd *gfx.Cmd, t target) {
	up := r.userProgram(cmd.Frag, uniformNames(cmd.Uniforms), false)
	if up == nil {
		return // compile error already logged, draw nothing
	}
	glx.UseProgram(up.prog)
	r.setQuadUniforms(up, cmd.Rect, cmd.UV, t)
	glx.Uniform2f(up.locs["u_res"], cmd.Rect.W, cmd.Rect.H)
	r.setCustomUniforms(up, cmd.Uniforms)
	glx.BindVertexArray(r.quadVao)
	glx.DrawArrays(glx.TRIANGLE_STRIP, 0, 4)
	glx.BindVertexArray(0)
}

func (r *Renderer) setQuadUniforms(up *userProgram, rect gfx.Rect, uv [4]float32, t target) {
	glx.Uniform4f(up.locs["u_rect"], rect.X, rect.Y, rect.W, rect.H)
	glx.Uniform4f(up.locs["u_uvrect"], uv[0], uv[1], uv[2], uv[3])
	glx.Uniform2f(up.locs["u_view"], t.viewW, t.viewH)
	glx.Uniform2f(up.locs["u_origin"], t.originX, t.originY)
	glx.Uniform1f(up.locs["u_flipY"], t.flipY)
	glx.Uniform1f(up.locs["u_time"], r.time)
}

func (r *Renderer) setCustomUniforms(up *userProgram, uniforms map[string]float32) {
	for k, v := range uniforms {
		if loc, ok := up.locs[k]; ok && loc >= 0 {
			glx.Uniform1f(loc, v)
		}
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

// nil = compile error, cached so a bad shader logs once
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
	// evict the LRU entry and free its program before adding. nil entries are
	// compile failures, they count against the cap (unique broken sources grow
	// the map too) and evict first as tick 0 since losing one costs one failed
	// recompile. the requested key isn't in the map yet so it can't be evicted
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
			glx.DeleteProgram(old.prog)
		}
		delete(r.userProgs, oldestKey)
	}
	var up *userProgram
	prog, err := link(shaderSrc(quadVertSrc, false), shaderSrc(wrapUserFrag(frag, names, withContent), true))
	if err != nil {
		log.Println("caution: custom shader failed to compile:", err)
	} else {
		locs := make(map[string]int32)
		for _, n := range append([]string{"u_rect", "u_uvrect", "u_view", "u_origin", "u_flipY", "u_res", "u_time", "u_tex"}, names...) {
			locs[n] = uniform(prog, n)
		}
		up = &userProgram{prog: prog, locs: locs, used: r.progTick}
	}
	r.userProgs[key] = up
	return up
}

// -- instance emitters ---------------------------------------------------------------

func (r *Renderer) emitRect(cmd *gfx.Cmd) {
	if cmd.Rect.W <= 0 || cmd.Rect.H <= 0 || cmd.Clip.W <= 0 || cmd.Clip.H <= 0 {
		return
	}
	r.inst(cmd.Rect, cmd.Radii, cmd.Color, cmd.Border, cmd.BorderWidth, modeRect, 0, 0, 0, 0, 0, cmd.Clip)
}

func (r *Renderer) emitShadow(cmd *gfx.Cmd) {
	if cmd.Rect.W <= 0 || cmd.Rect.H <= 0 || cmd.Clip.W <= 0 || cmd.Clip.H <= 0 {
		return
	}
	r.inst(cmd.Rect, cmd.Radii, cmd.Color, cmd.Color, 0, modeShadow, cmd.Blur, 0, 0, 0, 0, cmd.Clip)
}

func (r *Renderer) emitText(cmd *gfx.Cmd, dpr float32) {
	if cmd.Clip.W <= 0 || cmd.Clip.H <= 0 {
		return
	}
	run := r.Shaper.Shape(cmd.Font, dpr, cmd.Text)
	baseline := cmd.Y + run.Ascent
	penY := float32(math.Round(float64(baseline * dpr)))
	for _, g := range run.Glyphs {
		// snap the pen to a whole device pixel plus one of Phases subpixel
		// steps and use the atlas variant for that step. whole-pixel rounding
		// leaves gaps off by up to half a pixel, which reads as bad kerning
		xDev := (cmd.X + g.X) * dpr
		penX := float32(math.Floor(float64(xDev)))
		phase := int(math.Round(float64(xDev-penX) * text.Phases))
		if phase >= text.Phases {
			penX++
			phase = 0
		}
		glyph := r.Atlas.Get(cmd.Font, dpr, g.GID, phase, g.Emoji)
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
		r.inst(quad, noCorners, color, cmd.Color, 0, modeTextured, 0,
			float32(glyph.X0)/text.AtlasSize, float32(glyph.Y0)/text.AtlasSize,
			float32(glyph.X1)/text.AtlasSize, float32(glyph.Y1)/text.AtlasSize,
			cmd.Clip)
	}
}

func (r *Renderer) inst(
	rect gfx.Rect, radii gfx.Corners, color, border gfx.Color,
	borderWidth float32, mode int, blur float32,
	u0, v0, u1, v1 float32,
	clip gfx.Rect,
) {
	if (r.count+1)*floatsPerInstance > len(r.data) {
		grown := make([]float32, len(r.data)*2)
		copy(grown, r.data)
		r.data = grown
	}
	d := r.data
	o := r.count * floatsPerInstance
	d[o+0], d[o+1], d[o+2], d[o+3] = rect.X, rect.Y, rect.W, rect.H
	d[o+4], d[o+5], d[o+6], d[o+7] = radii.TL, radii.TR, radii.BR, radii.BL
	d[o+8], d[o+9], d[o+10], d[o+11] = color.R, color.G, color.B, color.A
	d[o+12], d[o+13], d[o+14], d[o+15] = border.R, border.G, border.B, border.A
	d[o+16], d[o+17], d[o+18], d[o+19] = borderWidth, float32(mode), blur, 0
	d[o+20], d[o+21], d[o+22], d[o+23] = u0, v0, u1, v1
	d[o+24], d[o+25], d[o+26], d[o+27] = clip.X, clip.Y, clip.W, clip.H
	r.count++
}

// cmd/loopprobe asserts both stay bounded across resize and shader-mint
// storms
func (r *Renderer) PoolSizes() (pooledLayers, userPrograms int) {
	for _, ls := range r.layerPool {
		pooledLayers += len(ls)
	}
	return pooledLayers, len(r.userProgs)
}
