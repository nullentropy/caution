package gpu

type Texture interface {
	Size() (w, h int)
}

type Pipeline interface {
	Set(name string, v ...float32)
}

const InstanceFloats = 28

type Filter int

const (
	Linear Filter = iota
	Nearest
)

type Device interface {
	MaxTextureSize() int
	NewTexture(w, h int, f Filter) (Texture, error)
	WriteTexture(t Texture, y0 int, rows []byte) error
	DeleteTexture(t Texture)

	NewPipeline(vert, frag string, instanced bool) (Pipeline, error)
	DeletePipeline(p Pipeline)

	SetTarget(t Texture, devW, devH int)
	Clear(r, g, b, a float32)
	SetScissor(r *[4]int32)
	Draw(p Pipeline, tex Texture, inst []float32, count int)
	Copy(src Texture)

	FlipY(layer bool) float32

	ReadPixels(t Texture) ([]byte, error)
	Present() bool
	Lost() bool
}
