package cdp

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// chromeBin locates a Chrome/Chromium binary: the -chrome flag, then
// $CAUTION_CHROME, then well-known bundle paths, then $PATH.
func ChromeBin(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if v := os.Getenv("CAUTION_CHROME"); v != "" {
		return v, nil
	}
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Chrome/Chromium found - pass -chrome or set CAUTION_CHROME")
}

// cdp is the smallest useful Chrome DevTools Protocol client: one browser
// WebSocket, sequential commands routed by id, events discarded. Enough to
// open a tab, wait for the terminal to settle, and screenshot it.
type Client struct {
	cmd     *exec.Cmd
	conn    *websocket.Conn
	dataDir string

	mu      sync.Mutex
	nextID  int
	pending map[int]chan reply
}

type reply struct {
	result json.RawMessage
	err    string
}

var devtoolsRE = regexp.MustCompile(`DevTools listening on (ws://\S+)`)

// launchChrome starts a fresh headless instance with a throwaway profile.
// The device scale is set per target (Emulation.setDeviceMetricsOverride),
// not here, so the capture geometry can't drift from flag changes.
func Launch(bin string, w, h int) (*Client, error) {
	dataDir, err := os.MkdirTemp("", "caution-goldens-chrome-")
	if err != nil {
		return nil, err
	}
	args := []string{
		"--headless=new",
		"--remote-debugging-port=0",
		"--user-data-dir=" + dataDir,
		"--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--mute-audio", "--hide-scrollbars",
		// The terminal paints on rAF; never let a "background" heuristic
		// throttle it mid-capture.
		"--disable-background-timer-throttling",
		"--disable-renderer-backgrounding",
		"--disable-backgrounding-occluded-windows",
		fmt.Sprintf("--window-size=%d,%d", w, h),
	}
	if os.Geteuid() == 0 {
		// Chrome refuses to start its sandbox as root and exits before
		// printing an endpoint - which is every container, where root is
		// the normal case (see linux/Dockerfile). Never reached on a
		// desktop, where the sandbox stays on.
		args = append(args, "--no-sandbox", "--disable-dev-shm-usage")
	}
	args = append(args, "about:blank")
	cmd := exec.Command(bin, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Chrome prints the browser endpoint on stderr once the debugger is up.
	wsURL := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := devtoolsRE.FindStringSubmatch(sc.Text()); m != nil {
				wsURL <- m[1]
				break
			}
		}
		// Keep draining so Chrome never blocks on a full stderr pipe.
		for sc.Scan() {
		}
	}()

	var u string
	select {
	case u = <-wsURL:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("chrome: no DevTools endpoint within 20s")
	}

	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("chrome: dial %s: %w", u, err)
	}
	c := &Client{cmd: cmd, conn: conn, dataDir: dataDir, pending: map[int]chan reply{}}
	go c.readLoop()
	return c, nil
}

func (c *Client) Close() {
	_ = c.conn.Close()
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
	_ = os.RemoveAll(c.dataDir)
}

func (c *Client) readLoop() {
	for {
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := c.conn.ReadJSON(&msg); err != nil {
			// Connection down: fail every waiter instead of hanging them.
			c.mu.Lock()
			for id, ch := range c.pending {
				ch <- reply{err: "devtools connection closed"}
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		if msg.ID == 0 {
			continue // event - not ours
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		r := reply{result: msg.Result}
		if msg.Error != nil {
			r.err = msg.Error.Message
		}
		ch <- r
	}
}

// call issues one command and waits for its reply. sessionID targets a
// specific attached tab; "" is the browser itself.
func (c *Client) Call(sessionID, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan reply, 1)
	c.pending[id] = ch
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	err := c.conn.WriteJSON(msg) // single-writer: calls are sequential, reader never writes
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("cdp %s: %w", method, err)
	}
	select {
	case r := <-ch:
		if r.err != "" {
			return nil, fmt.Errorf("cdp %s: %s", method, r.err)
		}
		return r.result, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("cdp %s: no reply within 30s", method)
	}
}

// eval runs an expression in the page and returns its by-value result as
// raw JSON (the CDP value field).
func (c *Client) Eval(sessionID, expr string) (json.RawMessage, error) {
	raw, err := c.Call(sessionID, "Runtime.evaluate",
		map[string]any{"expression": expr, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if res.ExceptionDetails != nil {
		return nil, fmt.Errorf("eval: %s", res.ExceptionDetails.Text)
	}
	return res.Result.Value, nil
}

// capture opens a fresh tab (fresh caution session - sessionStorage is
// per-tab), forces the metrics to wxh at 2x to match the native shot
// framebuffer, waits for the terminal's readiness probe to go quiet,
// and writes the screenshot PNG.
func (c *Client) Capture(url, outPath string, w, h int) error {
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
	defer c.Call("", "Target.closeTarget", map[string]any{"targetId": t.TargetID})

	aRaw, err := c.Call("", "Target.attachToTarget",
		map[string]any{"targetId": t.TargetID, "flatten": true})
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

	if _, err := c.Call(sess, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": w, "height": h, "deviceScaleFactor": 2, "mobile": false,
	}); err != nil {
		return err
	}
	if _, err := c.Call(sess, "Page.enable", nil); err != nil {
		return err
	}
	if _, err := c.Call(sess, "Page.navigate", map[string]any{"url": url}); err != nil {
		return err
	}

	type probe struct {
		Frames  int  `json:"frames"`
		Mounted bool `json:"mounted"`
	}
	var last probe
	stable := 0
	deadline := time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("page never settled (last probe: %+v)", last)
		}
		time.Sleep(150 * time.Millisecond)
		v, err := c.Eval(sess, "JSON.stringify(window.__caution || null)")
		if err != nil {
			return err
		}
		var s string
		if json.Unmarshal(v, &s) != nil || s == "" || s == "null" {
			continue
		}
		var p probe
		if json.Unmarshal([]byte(s), &p) != nil {
			continue
		}
		if p.Mounted && p.Frames > 0 && p == last {
			if stable++; stable >= 3 {
				break
			}
		} else {
			stable = 0
		}
		last = p
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
	pngBytes, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, pngBytes, 0o644)
}
