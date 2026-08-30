package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"time"

	caution "github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/internal/cdp"
)

// runBrowser serves the probe scene from an in-process caution server, opens
// it in headless Chrome, and asserts the loop behavior from the outside:
// the insert must produce a burst of build frames (the fade advancing) and
// the final pixels must be solid.
// Headless Chrome is the one place browser rAF demonstrably free-runs: a
// visible tab that loses OS compositing throttles rAF and fakes the
// starvation this probe hunts.
func runBrowser(chromeFlag string) error {
	caution.ServeClient()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	insertAt := 3 * time.Second
	mount := func(s *caution.Session) *caution.Node {
		root := caution.Panel().Bg("#16181d")
		root.Kids(caution.Shader(caution.FX{
			Frag:    "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }",
			Animate: true,
		}).Frame(20, 20, 160, 120))
		go func() {
			time.Sleep(insertAt)
			s.Update(func() {
				root.Add(caution.Panel().Bg("#ff2030").Frame(40, 200, 80, 60))
				root.Add(caution.Panel().Bg("#ff2030").Frame(220, 40, 120, 100).Effect(caution.FX{
					Frag:    "vec4 effect(vec2 uv) { return src(uv) * (0.9 + 0.1*sin(u_time*20.0 + uv.y*80.0)); }",
					Animate: true,
				}).Kids(caution.Shader(caution.FX{
					Frag:    "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }",
					Animate: true,
				}).Frame(10, 10, 40, 30)))
			})
		}()
		return root
	}
	go caution.ServeListener(l, mount, caution.Options{})

	bin, err := cdp.ChromeBin(chromeFlag)
	if err != nil {
		return err
	}
	c, err := cdp.Launch(bin, 520, 400)
	if err != nil {
		return err
	}
	defer c.Close()
	tRaw, err := c.Call("", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		return err
	}
	var t struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(tRaw, &t); err != nil {
		return err
	}
	aRaw, err := c.Call("", "Target.attachToTarget", map[string]any{"targetId": t.TargetID, "flatten": true})
	if err != nil {
		return err
	}
	var a struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(aRaw, &a); err != nil {
		return err
	}
	sess := a.SessionID
	if _, err := c.Call(sess, "Emulation.setDeviceMetricsOverride",
		map[string]any{"width": 520, "height": 400, "deviceScaleFactor": 2, "mobile": false}); err != nil {
		return err
	}
	if _, err := c.Call(sess, "Page.enable", nil); err != nil {
		return err
	}
	if _, err := c.Call(sess, "Page.navigate",
		map[string]any{"url": fmt.Sprintf("http://%s/", l.Addr())}); err != nil {
		return err
	}

	// The whole timeline fits in insertAt + fade + slack. Sample the damage
	// counter before and after the insert: the fade must show up as a burst
	// of build frames while the shader's continuation keeps running.
	type probe struct {
		Frames, Damaged, Partials int
		Mounted                   bool
	}
	read := func() (probe, error) {
		var p probe
		v, err := c.Eval(sess, `JSON.stringify({Frames:(window.__caution||{}).frames||0,Damaged:(window.__caution||{}).damaged||0,Partials:(window.__caution||{}).partials||0,Mounted:!!(window.__caution||{}).mounted})`)
		if err != nil {
			return p, err
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return p, err
		}
		return p, json.Unmarshal([]byte(s), &p)
	}
	deadline := time.Now().Add(15 * time.Second)
	var before probe
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("page never settled (last: %+v)", before)
		}
		p, err := read()
		if err == nil && p.Mounted && p.Partials > 20 && p.Damaged >= 0 {
			before = p
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	time.Sleep(insertAt + 1500*time.Millisecond)
	after, err := read()
	if err != nil {
		return err
	}
	fmt.Printf("browser: before insert %+v, after %+v\n", before, after)
	if after.Damaged-before.Damaged < 8 {
		return fmt.Errorf("insert produced %d build frames; a 200ms fade needs a burst (starvation)",
			after.Damaged-before.Damaged)
	}
	if after.Partials <= before.Partials {
		return fmt.Errorf("continuation stopped (partials %d -> %d)", before.Partials, after.Partials)
	}

	shot, err := c.Call(sess, "Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return err
	}
	var img struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(shot, &img); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return err
	}
	im, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	redAt := func(lx, ly int) uint32 {
		r, _, _, _ := im.At(lx*2, ly*2).RGBA()
		return r >> 8
	}
	plain, overlay := redAt(80, 230), redAt(280, 90)
	fmt.Printf("browser: plain=%d overlay=%d\n", plain, overlay)
	if plain < 240 {
		return fmt.Errorf("plain insert never finished fading (red=%d)", plain)
	}
	if overlay < 180 {
		return fmt.Errorf("effect-layer insert never finished fading (red=%d)", overlay)
	}
	fmt.Println("browser: ok - fades complete under an animated composite")
	return nil
}
