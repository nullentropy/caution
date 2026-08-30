package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	caution "github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/native"
)

// The scenes: full-bleed fragment shaders in the shared ES3 subset, dark and
// slow on purpose. Each gets u_time in seconds and u_res in logical px.
var scenes = []struct {
	name string
	frag string
}{
	{"aurora", `
vec4 effect(vec2 uv) {
  vec2 p = uv * vec2(u_res.x / u_res.y, 1.0);
  float t = u_time * 0.05;
  float glow = 0.0;
  for (int i = 1; i <= 4; i++) {
    float fi = float(i);
    float band = uv.y - 0.35
      - 0.16 * sin(p.x * (1.2 + fi * 0.7) + t * (3.0 + fi))
      - 0.05 * sin(p.x * 5.3 + t * 7.0 + fi * 2.1);
    glow += (0.014 + 0.004 * sin(t * 5.0 + fi)) / (abs(band) + 0.028) / fi;
  }
  vec3 sky = mix(vec3(0.01, 0.015, 0.04), vec3(0.02, 0.05, 0.09), uv.y);
  vec3 c = sky + glow * mix(vec3(0.05, 0.65, 0.45), vec3(0.25, 0.35, 0.85), uv.y + 0.2 * sin(t * 2.0));
  return vec4(c, 1.0);
}`},
	{"starfield", `
float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
vec4 effect(vec2 uv) {
  vec2 p = uv * vec2(u_res.x / u_res.y, 1.0);
  vec3 c = mix(vec3(0.012, 0.014, 0.03), vec3(0.03, 0.02, 0.05), uv.y);
  for (int layer = 1; layer <= 3; layer++) {
    float fl = float(layer);
    vec2 q = p * (26.0 * fl) + vec2(u_time * 0.9 * fl, 0.0);
    vec2 cell = floor(q);
    float star = hash(cell);
    if (star > 0.966) {
      vec2 pos = fract(q) - 0.5;
      float tw = 0.55 + 0.45 * sin(u_time * (1.0 + star * 4.0) + star * 40.0);
      float d = length(pos - (vec2(hash(cell + 1.0), hash(cell + 2.0)) - 0.5) * 0.5);
      // The glow must hit zero inside the cell, or its tail clips at the
      // cell boundary and every star sits in a faint square.
      float g = max(0.0, 0.02 / (d + 0.014) - 0.075);
      c += tw * vec3(0.9, 0.95, 1.0) * g / fl;
    }
  }
  return vec4(c, 1.0);
}`},
	{"lava", `
vec4 effect(vec2 uv) {
  vec2 p = uv * vec2(u_res.x / u_res.y, 1.0) * 3.0;
  float t = u_time * 0.12;
  float v = sin(p.x + t) + sin(p.y + t * 1.3)
    + sin(p.x + p.y + t * 1.7)
    + sin(length(p - vec2(1.6, 1.5)) * 1.8 - t * 2.2);
  float k = 0.5 + 0.5 * sin(v);
  vec3 a = vec3(0.03, 0.01, 0.06);
  vec3 b = vec3(0.35, 0.08, 0.30);
  vec3 c = vec3(0.95, 0.45, 0.15);
  vec3 col = mix(a, b, smoothstep(0.15, 0.75, k));
  col = mix(col, c, smoothstep(0.82, 0.98, k) * 0.7);
  return vec4(col, 1.0);
}`},
}

const sceneEvery = 30 * time.Second

// startScene is the -scene flag's pick (an index into scenes); cycling
// continues from it.
var startScene = 0

func mount(s *caution.Session) *caution.Node {
	pane := caution.Shader(caution.FX{Frag: scenes[startScene].frag, Animate: true}).
		Anchor(caution.A{Left: caution.Px(0), Top: caution.Px(0), Right: caution.Px(0), Bottom: caution.Px(0)})

	now := time.Now()
	clock := caution.Label(now.Format("15:04")).FontSize(120).Weight(600).Color("#e8eaf0").
		Anchor(caution.A{Left: caution.Px(72), Bottom: caution.Px(96)})
	date := caution.Label(now.Format("Monday, January 2")).FontSize(22).Color("#9aa3b2").
		Anchor(caution.A{Left: caution.Px(76), Bottom: caution.Px(64)})
	sceneName := caution.Label(scenes[startScene].name).FontSize(13).Color("#6b7280").
		Anchor(caution.A{Right: caution.Px(76), Bottom: caution.Px(64)})

	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		start := time.Now()
		scene := startScene
		for {
			select {
			case <-s.Done():
				return
			case <-tick.C:
				s.Update(func() {
					t := time.Now()
					clock.SetText(t.Format("15:04"))
					date.SetText(t.Format("Monday, January 2"))
					if next := (startScene + int(time.Since(start)/sceneEvery)) % len(scenes); next != scene {
						scene = next
						pane.SetProp("frag", scenes[scene].frag)
						sceneName.SetText(scenes[scene].name)
					}
				})
			}
		}
	}()

	return caution.Panel().Bg("#06070a").Kids(pane, clock, date, sceneName)
}

func main() {
	windowed := flag.Bool("windowed", false, "run in a window instead of fullscreen")
	serve := flag.String("serve", "", "also serve the browser terminal on this address (e.g. :8790)")
	sceneFlag := flag.String("scene", "", "start on this scene (aurora | starfield | lava)")
	shot := flag.String("shot", "", "headless verification: render offscreen to this PNG and exit")
	settle := flag.Float64("settle", 2500, "shot mode: ms to run before capturing")
	flag.Parse()

	for i, sc := range scenes {
		if sc.name == *sceneFlag {
			startScene = i
		}
	}

	if *serve != "" {
		caution.ServeClient()
		go func() {
			fmt.Printf("browser terminal on %s\n", *serve)
			// The browser side is a bonus: a taken port must not take the
			// native saver down with it.
			if err := caution.Serve(*serve, mount); err != nil {
				log.Println("browser terminal:", err)
			}
		}()
	}

	err := native.Run(mount, native.Options{
		Title:       "caution screensaver",
		W:           1280,
		H:           800,
		MaxFPS:      30, // a screensaver should sip, not gulp
		Fullscreen:  !*windowed && *shot == "",
		HideCursor:  *shot == "",
		ExitOnInput: *shot == "",
		Shot:        *shot,
		SettleMs:    *settle,
	})
	if err != nil {
		log.Fatal(err)
	}
}
