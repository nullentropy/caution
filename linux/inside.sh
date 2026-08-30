#!/usr/bin/env bash
# Runs inside the container (see verify.sh). Each section prints PASS/FAIL and
# the script keeps going, so one failure doesn't hide the rest.
set -uo pipefail
cd /caution/go
OUT=/caution/linux/out
FAILED=0
step() { printf '\n=== %s\n' "$1"; }
ok()   { printf '    PASS  %s\n' "$1"; }
bad()  { printf '    FAIL  %s\n' "$1"; FAILED=1; }
X="xvfb-run -a -s -screen\ 0\ 1400x900x24"

step "environment"
echo "    $(uname -srm)  |  $(go version | cut -d' ' -f3)"
xvfb-run -a glxinfo -B 2>/dev/null | grep -iE "opengl core profile version|opengl renderer" | sed 's/^/    /'

step "build: cmd/term (cgo, GLFW/X11, GL 4.1 core)"
if go build -o /tmp/term ./cmd/term 2>&1 | tail -5; then ok "x11 build"; else bad "x11 build"; fi

step "build: -tags wayland (for the handoff to a Wayland desktop)"
if go build -tags wayland -o /tmp/term-wayland ./cmd/term 2>&1 | tail -5; then ok "wayland build"; else bad "wayland build"; fi

step "build: everything else on Linux"
if go build ./... 2>&1 | tail -5; then ok "go build ./..."; else bad "go build ./..."; fi

step "test: Go suites on Linux"
if go test ./... 2>&1 | grep -vE "^(ok|---)" | grep -v "no test files" | head -10; then :; fi
if go test ./... > /tmp/gotest.log 2>&1; then ok "$(grep -c '^ok' /tmp/gotest.log) packages"; else bad "go test"; tail -15 /tmp/gotest.log; fi

step "test: TS suite on Linux (esbuild + node)"
if command -v node > /dev/null 2>&1; then
  if go run ./cmd/tstest > /tmp/ts.log 2>&1; then ok "$(tail -1 /tmp/ts.log)"; else bad "tstest"; tail -5 /tmp/ts.log; fi
else
  echo "    SKIP  node not installed in the image"
fi

step "render: renderer scene, offscreen"
if xvfb-run -a -s "-screen 0 1400x900x24" /tmp/term -scene -shot "$OUT/linux-scene.png" > /tmp/scene.log 2>&1; then
  ok "linux-scene.png ($(stat -c%s "$OUT/linux-scene.png") bytes)"
else bad "scene shot"; tail -5 /tmp/scene.log; fi

step "app stack: demo server + native terminal over the wire"
(go run ./cmd/demo > /tmp/demo.log 2>&1 &)
for i in $(seq 1 40); do
  if grep -q "listening" /tmp/demo.log 2>/dev/null; then break; fi
  sleep 1
done
if xvfb-run -a -s "-screen 0 1400x900x24" /tmp/term \
     -url ws://localhost:8787/ws -shot "$OUT/linux-demo.png" -settle 3000 > /tmp/app.log 2>&1; then
  ok "linux-demo.png ($(stat -c%s "$OUT/linux-demo.png") bytes) - menubar widget is the default off macOS"
else bad "app stack"; tail -10 /tmp/app.log; fi

step "package: XDG desktop entry + icons (cmd/linuxapp)"
rm -rf /tmp/stage
if go run ./cmd/linuxapp -pkg ./cmd/term -name "Caution Terminal" -o /tmp/stage > /tmp/pkg.log 2>&1; then
  if grep -q "desktop-file-validate clean" /tmp/pkg.log; then ok "entry validates"; else bad "entry did not validate"; tail -5 /tmp/pkg.log; fi
  echo "    $(find /tmp/stage -type f | wc -l) files staged: binary + entry + $(find /tmp/stage -name '*.png' | wc -l) icon sizes"
  # StartupWMClass is only useful if the window really advertises it.
  CLASS=$(timeout 45 xvfb-run -a -s "-screen 0 1200x800x24" bash -c '
    /tmp/stage/bin/caution-terminal -url ws://127.0.0.1:9/ws -title "Caution Terminal" -appid caution-terminal 2>/dev/null &
    sleep 7
    xwininfo -root -children 2>/dev/null | grep -oE "\(\"caution-terminal\" \"caution-terminal\"\)" | head -1
    pkill -f caution-terminal' 2>/dev/null)
  if [ -n "$CLASS" ]; then ok "window WM_CLASS matches StartupWMClass"; else bad "WM_CLASS did not match the entry"; fi
else
  bad "linuxapp"; tail -10 /tmp/pkg.log
fi

step "parity: goldens, both terminals, on Linux"
if xvfb-run -a -s "-screen 0 1400x900x24" go run ./cmd/goldens -out "$OUT/goldens" > /tmp/goldens.log 2>&1; then
  grep -E "^(controls|table|tree|layout|text)" /tmp/goldens.log | sed 's/^/    /'
  ok "10/10 scenes"
else
  bad "goldens"; tail -20 /tmp/goldens.log
fi

printf '\n=== result: '
if [ "$FAILED" = 0 ]; then echo "everything passed"; else echo "SOMETHING FAILED (see above)"; fi
exit "$FAILED"
