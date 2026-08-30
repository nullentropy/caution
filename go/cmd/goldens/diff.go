package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"sort"
)

// Frames are compared as a grid of cell means: each cell's delta is the largest
// per-channel difference of its average color. Both terminals shape and
// rasterize text with the same wasm engine and the same fonts, so even glyphs
// match to the pixel and residual noise is GPU rasterization and AA at sub-cell
// scale. Structural drift, a panel missing or a border the wrong color or a
// widget in the wrong place, moves whole runs of cells, hard.
const cellPx = 32

// Verdict thresholds. The eight fixture scenes baseline at soft 0.00-0.04%,
// hard 0%, blob <=1, the one known divergence being the checkbox glyph. The
// limits keep generous headroom over that while still catching a single moved
// or missing button-sized element (~14 cells).
const (
	hardTol = 48  // a cell this different is never rasterization noise
	softTol = 12  // above this a cell counts toward connected regions
	maxHard = 0.1 // % of cells allowed above hardTol
	maxBlob = 6   // largest allowed connected region of soft cells
	maxSoft = 0.5 // % of cells allowed above softTol
)

type diffResult struct {
	name             string
	gw, gh           int
	softPct, hardPct float64
	blob             int
	p50, p95, max    float64
	artifact         string
}

func (d diffResult) pass() bool {
	return d.hardPct <= maxHard && d.blob <= maxBlob && d.softPct <= maxSoft
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// cellMeans reduces an image to per-cell average RGB.
func cellMeans(img image.Image, gw, gh int) [][3]float64 {
	b := img.Bounds()
	sums := make([][4]float64, gw*gh) // r, g, b, count
	for y := b.Min.Y; y < b.Max.Y; y++ {
		cy := (y - b.Min.Y) / cellPx
		for x := b.Min.X; x < b.Max.X; x++ {
			cx := (x - b.Min.X) / cellPx
			r, g, bl, _ := img.At(x, y).RGBA()
			s := &sums[cy*gw+cx]
			s[0] += float64(r >> 8)
			s[1] += float64(g >> 8)
			s[2] += float64(bl >> 8)
			s[3]++
		}
	}
	out := make([][3]float64, gw*gh)
	for i, s := range sums {
		if s[3] > 0 {
			out[i] = [3]float64{s[0] / s[3], s[1] / s[3], s[2] / s[3]}
		}
	}
	return out
}

// largestBlob returns the size of the biggest 4-connected region of cells
// whose delta exceeds softTol.
func largestBlob(delta []float64, gw, gh int) int {
	seen := make([]bool, len(delta))
	best := 0
	var stack []int
	for i := range delta {
		if seen[i] || delta[i] <= softTol {
			continue
		}
		size := 0
		stack = append(stack[:0], i)
		seen[i] = true
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			cx, cy := c%gw, c/gw
			for _, n := range [][2]int{{cx - 1, cy}, {cx + 1, cy}, {cx, cy - 1}, {cx, cy + 1}} {
				if n[0] < 0 || n[0] >= gw || n[1] < 0 || n[1] >= gh {
					continue
				}
				j := n[1]*gw + n[0]
				if !seen[j] && delta[j] > softTol {
					seen[j] = true
					stack = append(stack, j)
				}
			}
		}
		if size > best {
			best = size
		}
	}
	return best
}

// diffFrames compares the two captures and writes a side-by-side artifact:
// browser | native | heatmap (native dimmed, hot cells tinted by severity).
func diffFrames(name, browserPath, nativePath, outDir string) (diffResult, error) {
	bImg, err := loadPNG(browserPath)
	if err != nil {
		return diffResult{name: name}, err
	}
	nImg, err := loadPNG(nativePath)
	if err != nil {
		return diffResult{name: name}, err
	}
	bw, bh := bImg.Bounds().Dx(), bImg.Bounds().Dy()
	nw, nh := nImg.Bounds().Dx(), nImg.Bounds().Dy()
	if bw != nw || bh != nh {
		return diffResult{name: name}, fmt.Errorf("size mismatch: browser %dx%d vs native %dx%d", bw, bh, nw, nh)
	}

	gw, gh := (bw+cellPx-1)/cellPx, (bh+cellPx-1)/cellPx
	bm := cellMeans(bImg, gw, gh)
	nm := cellMeans(nImg, gw, gh)

	delta := make([]float64, gw*gh)
	soft, hard := 0, 0
	for i := range delta {
		d := 0.0
		for ch := 0; ch < 3; ch++ {
			if v := abs(bm[i][ch] - nm[i][ch]); v > d {
				d = v
			}
		}
		delta[i] = d
		if d > softTol {
			soft++
		}
		if d > hardTol {
			hard++
		}
	}

	sorted := append([]float64(nil), delta...)
	sort.Float64s(sorted)
	total := float64(len(delta))
	res := diffResult{
		name: name, gw: gw, gh: gh,
		softPct: 100 * float64(soft) / total,
		hardPct: 100 * float64(hard) / total,
		blob:    largestBlob(delta, gw, gh),
		p50:     sorted[len(sorted)/2],
		p95:     sorted[len(sorted)*95/100],
		max:     sorted[len(sorted)-1],
	}

	res.artifact = filepath.Join(outDir, name+"-diff.png")
	if err := writeComposite(res.artifact, bImg, nImg, delta, gw); err != nil {
		return res, err
	}
	return res, nil
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func writeComposite(path string, bImg, nImg image.Image, delta []float64, gw int) error {
	w, h := bImg.Bounds().Dx(), bImg.Bounds().Dy()
	const gut = 16
	out := image.NewRGBA(image.Rect(0, 0, w*3+gut*2, h))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.RGBA{24, 24, 28, 255}}, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, 0, w, h), bImg, bImg.Bounds().Min, draw.Src)
	draw.Draw(out, image.Rect(w+gut, 0, 2*w+gut, h), nImg, nImg.Bounds().Min, draw.Src)

	// Heat panel: the native frame at 35% brightness, hot cells tinted.
	hx := 2 * (w + gut)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := nImg.At(nImg.Bounds().Min.X+x, nImg.Bounds().Min.Y+y).RGBA()
			pr, pg, pb := float64(r>>8)*0.35, float64(g>>8)*0.35, float64(b>>8)*0.35
			d := delta[(y/cellPx)*gw+x/cellPx]
			switch {
			case d > hardTol: // red
				pr, pg, pb = pr*0.4+215*0.6, pg*0.4+50*0.6, pb*0.4+45*0.6
			case d > 24: // orange
				pr, pg, pb = pr*0.5+235*0.5, pg*0.5+140*0.5, pb*0.5+30*0.5
			case d > softTol: // dim yellow
				pr, pg, pb = pr*0.7+190*0.3, pg*0.7+180*0.3, pb*0.7+60*0.3
			}
			out.SetRGBA(hx+x, y, color.RGBA{uint8(pr), uint8(pg), uint8(pb), 255})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}
