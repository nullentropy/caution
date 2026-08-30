package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	caution "github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/native"
)

const fps = 12

// starsFrag: the shader tier - slow parallax starfield behind everything.
const starsFrag = `
float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
vec4 effect(vec2 uv) {
  vec2 p = uv * vec2(u_res.x / u_res.y, 1.0);
  vec3 c = vec3(0.024, 0.027, 0.07);
  for (int layer = 1; layer <= 2; layer++) {
    float fl = float(layer);
    vec2 q = p * (22.0 * fl) + vec2(u_time * 1.6 * fl, 0.0);
    vec2 cell = floor(q);
    if (hash(cell) > 0.972) {
      vec2 pos = fract(q) - 0.5;
      float d = length(pos);
      float tw = 0.6 + 0.4 * sin(u_time * 3.0 + hash(cell) * 30.0);
      c += tw * vec3(0.85, 0.9, 1.0) * max(0.0, 0.02 / (d + 0.02) - 0.08) / fl;
    }
  }
  return vec4(c, 1.0);
}`

func mount(s *caution.Session) *caution.Node {
	// Tier 1: the shader, filling the window.
	sky := caution.Shader(caution.FX{Frag: starsFrag, Animate: true}).
		Anchor(caution.A{Left: caution.Px(0), Top: caution.Px(0), Right: caution.Px(0), Bottom: caution.Px(0)})

	// Tier 2: the flipbook. Preload every frame before the first patch -
	// the `resource` op warms the terminal's image cache, so each src swap
	// hits a resident texture instead of starting a fetch mid-animation.
	srcs := make([]string, Frames)
	for i := range srcs {
		srcs[i] = fmt.Sprintf("/nyan/%d.png", i)
	}
	s.Preload(srcs...)

	cat := caution.Image(srcs[0]).Fit("contain").Alt("nyan cat").
		Anchor(caution.A{Left: caution.Px(40), CenterY: caution.Px(-30), Right: caution.Px(40)}).
		H(220)

	caption := caution.Label("flipbook - 6 frames, 12fps, one `src` prop per tick").
		FontSize(14).Color("$inkDim").
		Anchor(caution.A{Left: caution.Px(44), Bottom: caution.Px(74)})
	counter := caution.Label("frame 0").FontSize(13).Color("$inkFaint").Mono().
		Anchor(caution.A{Left: caution.Px(44), Bottom: caution.Px(48)})

	// The pump: one goroutine per session, patching a single prop per tick.
	// s.Update runs it on the session goroutine; s.Done stops the ticker.
	go func() {
		tick := time.NewTicker(time.Second / fps)
		defer tick.Stop()
		n := 0
		for {
			select {
			case <-s.Done():
				return
			case <-tick.C:
				n++
				s.Update(func() {
					cat.SetProp("src", srcs[n%Frames])
					if n%fps == 0 {
						counter.SetText(fmt.Sprintf("frame %d · %ds of flipbook", n, n/fps))
					}
				})
			}
		}
	}()

	return caution.Panel().Bg("#06070a").Kids(sky, cat, caption, counter)
}

// serveFrames registers /nyan/N.png. The frames are generated once at
// startup and served from memory.
func serveFrames() {
	frames := make([][]byte, Frames)
	for i := range frames {
		frames[i] = NyanFrame(i)
	}
	http.HandleFunc("/nyan/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/nyan/")
		i, err := strconv.Atoi(strings.TrimSuffix(name, ".png"))
		if err != nil || i < 0 || i >= Frames {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		// Frames are immutable and hit every cycle: let the client keep them.
		w.Header().Set("Cache-Control", "max-age=86400")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(frames[i]))
	})
}

func main() {
	addr := flag.String("addr", ":8791", "listen address for the browser terminal")
	nativeFlag := flag.Bool("native", false, "open the native window instead of serving")
	shot := flag.String("shot", "", "headless verification: render offscreen to this PNG and exit")
	settle := flag.Float64("settle", 2000, "shot mode: ms to run before capturing")
	flag.Parse()

	serveFrames()

	if *nativeFlag || *shot != "" || native.InBundle() {
		// Not log.Fatal: a finished -shot returns nil, and exiting 1 on
		// success breaks any script that checks the status.
		if err := native.Run(mount, native.Options{
			Title: "nyan - flipbook over the wire",
			W:     900, H: 520,
			Shot: *shot, SettleMs: *settle,
		}); err != nil {
			log.Fatal(err)
		}
		return
	}

	caution.ServeClient()
	fmt.Printf("nyan on http://localhost%s\n", *addr)
	log.Fatal(caution.Serve(*addr, mount))
}
