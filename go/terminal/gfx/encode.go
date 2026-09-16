package gfx

import (
	"encoding/binary"
	"errors"
	"math"
	"sort"
)

// Encode writes a display list into a byte stream the browser terminal decodes
// into its renderer's command list, appending to buf. Little endian throughout.
// The layout is a flag byte, the background color, a string table, and then
// one record per command whose shape depends on its kind.
func Encode(dl *DisplayList, bg Color, buf []byte) []byte {
	e := encoder{buf: buf, strs: map[string]uint32{}}
	var flags byte
	if dl.GeomAnimation {
		flags |= 1
	}
	e.u8(flags)
	e.color(bg)

	// every string a command names goes in the table once. two passes: the
	// table has to land before the commands that index into it
	for i := range dl.Cmds {
		c := &dl.Cmds[i]
		switch c.Kind {
		case CmdText:
			e.intern(c.Text)
		case CmdImage:
			e.intern(c.Src)
		case CmdLayerEnd, CmdShaderQuad:
			e.intern(c.Frag)
			for k := range c.Uniforms {
				e.intern(k)
			}
		}
	}
	e.u32(uint32(len(e.table)))
	for _, s := range e.table {
		e.u32(uint32(len(s)))
		e.buf = append(e.buf, s...)
	}

	e.u32(uint32(len(dl.Cmds)))
	for i := range dl.Cmds {
		c := &dl.Cmds[i]
		e.u8(byte(c.Kind))
		switch c.Kind {
		case CmdRect:
			e.rect(c.Rect)
			e.corners(c.Radii)
			e.color(c.Color)
			e.color(c.Border)
			e.f32(c.BorderWidth)
			e.rect(c.Clip)
		case CmdShadow:
			e.rect(c.Rect)
			e.corners(c.Radii)
			e.color(c.Color)
			e.f32(c.Blur)
			e.rect(c.Clip)
		case CmdText:
			e.f32(c.X)
			e.f32(c.Y)
			e.rect(c.Rect)
			e.str(c.Text)
			e.font(c.Font)
			e.color(c.Color)
			e.rect(c.Clip)
		case CmdLayerBegin:
			e.rect(c.Rect)
		case CmdLayerEnd:
			e.rect(c.Rect)
			e.str(c.Frag)
			e.bool(c.Animated)
			e.uniforms(c.Uniforms)
		case CmdShaderQuad:
			e.rect(c.Rect)
			for _, v := range c.UV {
				e.f32(v)
			}
			e.str(c.Frag)
			e.bool(c.Animated)
			e.uniforms(c.Uniforms)
		case CmdImage:
			e.rect(c.Rect)
			e.str(c.Src)
			e.u8(fitCode(c.Fit))
			e.corners(c.Radii)
			e.rect(c.Clip)
		}
	}
	return e.buf
}

type encoder struct {
	buf   []byte
	strs  map[string]uint32
	table []string
}

func (e *encoder) intern(s string) {
	if _, ok := e.strs[s]; !ok {
		e.strs[s] = uint32(len(e.table))
		e.table = append(e.table, s)
	}
}

func (e *encoder) u8(v byte)    { e.buf = append(e.buf, v) }
func (e *encoder) u16(v uint16) { e.buf = binary.LittleEndian.AppendUint16(e.buf, v) }
func (e *encoder) u32(v uint32) { e.buf = binary.LittleEndian.AppendUint32(e.buf, v) }
func (e *encoder) f32(v float32) {
	e.buf = binary.LittleEndian.AppendUint32(e.buf, math.Float32bits(v))
}
func (e *encoder) bool(v bool) {
	if v {
		e.u8(1)
	} else {
		e.u8(0)
	}
}
func (e *encoder) str(s string) { e.u32(e.strs[s]) }
func (e *encoder) rect(r Rect) {
	e.f32(r.X)
	e.f32(r.Y)
	e.f32(r.W)
	e.f32(r.H)
}
func (e *encoder) corners(c Corners) {
	e.f32(c.TL)
	e.f32(c.TR)
	e.f32(c.BR)
	e.f32(c.BL)
}
func (e *encoder) color(c Color) {
	e.f32(c.R)
	e.f32(c.G)
	e.f32(c.B)
	e.f32(c.A)
}
func (e *encoder) font(f Font) {
	e.f32(f.Size)
	e.u16(uint16(f.Weight))
	var flags byte
	if f.Italic {
		flags |= 1
	}
	if f.Mono {
		flags |= 2
	}
	e.u8(flags)
}

// uniforms are written in key order so the same map always encodes the same way
func (e *encoder) uniforms(u map[string]float32) {
	keys := make([]string, 0, len(u))
	for k := range u {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	e.u16(uint16(len(keys)))
	for _, k := range keys {
		e.str(k)
		e.f32(u[k])
	}
}

func fitCode(fit string) byte {
	switch fit {
	case "cover":
		return 1
	case "fill":
		return 2
	}
	return 0
}

func fitName(code byte) string {
	switch code {
	case 1:
		return "cover"
	case 2:
		return "fill"
	}
	return "contain"
}

// Decode is Encode's inverse. The browser has its own decoder; this one keeps
// the format honest in tests.
func Decode(data []byte) (*DisplayList, Color, error) {
	d := decoder{buf: data}
	flags := d.u8()
	bg := d.color()
	n := d.u32()
	table := make([]string, n)
	for i := range table {
		l := d.u32()
		if d.err == nil && int(l) > len(d.buf)-d.pos {
			d.err = errShort
		}
		if d.err != nil {
			return nil, Color{}, d.err
		}
		table[i] = string(d.buf[d.pos : d.pos+int(l)])
		d.pos += int(l)
	}
	d.table = table
	dl := &DisplayList{GeomAnimation: flags&1 != 0}
	count := d.u32()
	for i := uint32(0); i < count && d.err == nil; i++ {
		c := Cmd{Kind: CmdKind(d.u8())}
		switch c.Kind {
		case CmdRect:
			c.Rect = d.rect()
			c.Radii = d.corners()
			c.Color = d.color()
			c.Border = d.color()
			c.BorderWidth = d.f32()
			c.Clip = d.rect()
		case CmdShadow:
			c.Rect = d.rect()
			c.Radii = d.corners()
			c.Color = d.color()
			c.Blur = d.f32()
			c.Clip = d.rect()
		case CmdText:
			c.X = d.f32()
			c.Y = d.f32()
			c.Rect = d.rect()
			c.Text = d.str()
			c.Font = d.font()
			c.Color = d.color()
			c.Clip = d.rect()
		case CmdLayerBegin:
			c.Rect = d.rect()
		case CmdLayerEnd:
			c.Rect = d.rect()
			c.Frag = d.str()
			c.Animated = d.bool()
			c.Uniforms = d.uniforms()
		case CmdShaderQuad:
			c.Rect = d.rect()
			for j := range c.UV {
				c.UV[j] = d.f32()
			}
			c.Frag = d.str()
			c.Animated = d.bool()
			c.Uniforms = d.uniforms()
		case CmdImage:
			c.Rect = d.rect()
			c.Src = d.str()
			c.Fit = fitName(d.u8())
			c.Radii = d.corners()
			c.Clip = d.rect()
		default:
			return nil, Color{}, errors.New("gfx: unknown command kind")
		}
		dl.Cmds = append(dl.Cmds, c)
	}
	if d.err != nil {
		return nil, Color{}, d.err
	}
	return dl, bg, nil
}

var errShort = errors.New("gfx: truncated frame")

type decoder struct {
	buf   []byte
	pos   int
	err   error
	table []string
}

func (d *decoder) need(n int) bool {
	if d.err != nil {
		return false
	}
	if d.pos+n > len(d.buf) {
		d.err = errShort
		return false
	}
	return true
}

func (d *decoder) u8() byte {
	if !d.need(1) {
		return 0
	}
	v := d.buf[d.pos]
	d.pos++
	return v
}

func (d *decoder) u16() uint16 {
	if !d.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(d.buf[d.pos:])
	d.pos += 2
	return v
}

func (d *decoder) u32() uint32 {
	if !d.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(d.buf[d.pos:])
	d.pos += 4
	return v
}

func (d *decoder) f32() float32 { return math.Float32frombits(d.u32()) }
func (d *decoder) bool() bool   { return d.u8() != 0 }

func (d *decoder) str() string {
	i := d.u32()
	if d.err == nil && int(i) >= len(d.table) {
		d.err = errors.New("gfx: string index out of range")
	}
	if d.err != nil {
		return ""
	}
	return d.table[i]
}

func (d *decoder) rect() Rect       { return Rect{d.f32(), d.f32(), d.f32(), d.f32()} }
func (d *decoder) corners() Corners { return Corners{d.f32(), d.f32(), d.f32(), d.f32()} }
func (d *decoder) color() Color     { return Color{d.f32(), d.f32(), d.f32(), d.f32()} }

func (d *decoder) font() Font {
	size := d.f32()
	weight := int(d.u16())
	flags := d.u8()
	return NewFont(size, FontOpts{Weight: weight, Italic: flags&1 != 0, Mono: flags&2 != 0})
}

func (d *decoder) uniforms() map[string]float32 {
	n := d.u16()
	if n == 0 {
		return nil
	}
	out := make(map[string]float32, n)
	for i := 0; i < int(n) && d.err == nil; i++ {
		k := d.str()
		out[k] = d.f32()
	}
	return out
}
