#!/usr/bin/env bash
set -uo pipefail
cd /caution
OUT=/caution/linux/out
FAILED=0
step() { printf '\n=== %s\n' "$1"; }
ok()   { printf '    PASS  %s\n' "$1"; }
bad()  { printf '    FAIL  %s\n' "$1"; FAILED=1; }
XVFB=(xvfb-run -a -s "-screen 0 1400x900x24")

step "environment"
echo "    $(uname -srm)  |  $(go version | cut -d' ' -f3)"
vulkaninfo --summary 2>/dev/null | grep -E "^\s+(apiVersion|deviceName|driverInfo)" | head -3 | sed 's/^\s*/    /'

step "build: cmd/term (cgo, GLFW/X11, Vulkan)"
if go build -o /tmp/term ./cmd/term 2>&1 | tail -5; then ok "x11 build"; else bad "x11 build"; fi
# go-gl/glfw passes -lGL on Linux, but nothing references it, so the linker's
# --as-needed (the Debian default) leaves libvulkan as the only GPU library
NEEDED=$(readelf -d /tmp/term | grep -oE 'lib(vulkan|GL|EGL|GLX|OpenGL)[^]]*' | sort -u | tr '\n' ' ')
echo "    NEEDED: $NEEDED"
if echo "$NEEDED" | grep -qE 'lib(GL|EGL|GLX|OpenGL)'; then bad "binary links a GL library"; else ok "no GL, EGL or GLX library linked"; fi

step "build: -tags wayland (for the handoff to a Wayland desktop)"
if go build -tags wayland -o /tmp/term-wayland ./cmd/term 2>&1 | tail -5; then ok "wayland build"; else bad "wayland build"; fi

step "build: everything else on Linux"
if go build ./... 2>&1 | tail -5; then ok "go build ./..."; else bad "go build ./..."; fi

step "test: Go suites on Linux"
if go test ./... > /tmp/gotest.log 2>&1; then ok "$(grep -c '^ok' /tmp/gotest.log) packages"; else bad "go test"; tail -15 /tmp/gotest.log; fi

step "render: renderer scene, offscreen (hidden window, textures only)"
if "${XVFB[@]}" /tmp/term -scene -shot "$OUT/linux-scene.png" > /tmp/scene.log 2>&1; then
  ok "linux-scene.png ($(stat -c%s "$OUT/linux-scene.png") bytes)"
else bad "scene shot"; tail -5 /tmp/scene.log; fi

step "render: same shot under the Khronos validation layer"
if CAUTION_VK_VALIDATION=1 VK_LOADER_DEBUG=layer "${XVFB[@]}" /tmp/term -scene -shot /tmp/scene-val.png > /tmp/val.log 2>&1; then
  if grep -q 'Insert instance layer "VK_LAYER_KHRONOS_validation"' /tmp/val.log; then
    N=$(grep -c "Validation Error" /tmp/val.log)
    if [ "$N" = 0 ]; then ok "layer loaded, 0 validation errors"; else bad "$N validation errors"; grep -m3 "Validation Error" /tmp/val.log; fi
  else bad "validation layer did not load"; fi
else bad "validation shot"; tail -5 /tmp/val.log; fi

step "window: swapchain present and resize under Xvfb (validation on)"
cat > /tmp/win.sh <<'EOF'
CAUTION_VK_VALIDATION=1 /tmp/term -scene -w 900 -h 600 > /tmp/win.log 2>&1 &
sleep 6
id=$(xdotool search --name caution | head -1)
import -window "$id" /caution/linux/out/linux-window.png
xdotool windowsize "$id" 700 450
sleep 4
import -window "$id" /caution/linux/out/linux-window-resized.png
kill %1
EOF
"${XVFB[@]}" bash /tmp/win.sh > /tmp/winrun.log 2>&1
SZ1=$(identify -format '%wx%h' "$OUT/linux-window.png" 2>/dev/null)
SZ2=$(identify -format '%wx%h' "$OUT/linux-window-resized.png" 2>/dev/null)
C1=$(convert "$OUT/linux-window.png" -format '%k' info: 2>/dev/null || echo 0)
C2=$(convert "$OUT/linux-window-resized.png" -format '%k' info: 2>/dev/null || echo 0)
NV=$(grep -c "Validation Error" /tmp/win.log)
echo "    window $SZ1 ($C1 colors), after resize $SZ2 ($C2 colors), $NV validation errors"
if [ "$SZ1" = 900x600 ] && [ "$SZ2" = 700x450 ] && [ "${C1:-0}" -gt 100 ] && [ "${C2:-0}" -gt 100 ] && [ "$NV" = 0 ]; then
  ok "linux-window.png, linux-window-resized.png"
else bad "window present"; tail -5 /tmp/win.log /tmp/winrun.log; fi

step "app stack: demo server + native terminal over the wire"
(go run ./cmd/demo > /tmp/demo.log 2>&1 &)
for i in $(seq 1 40); do
  if grep -q "listening" /tmp/demo.log 2>/dev/null; then break; fi
  sleep 1
done
if "${XVFB[@]}" /tmp/term \
     -url ws://localhost:8787/ws -shot "$OUT/linux-demo.png" -settle 3000 > /tmp/app.log 2>&1; then
  ok "linux-demo.png ($(stat -c%s "$OUT/linux-demo.png") bytes): menubar widget is the default off macOS"
else bad "app stack"; tail -10 /tmp/app.log; fi

step "probes: damage path, loop pacing, resource bounds"
if "${XVFB[@]}" go run ./cmd/damageprobe > /tmp/damage.log 2>&1; then
  ok "damageprobe: $(grep -E '^all [0-9]+ steps' /tmp/damage.log | cut -d';' -f1)"
else bad "damageprobe"; tail -10 /tmp/damage.log; fi
if "${XVFB[@]}" go run ./cmd/loopprobe > /tmp/loop.log 2>&1; then
  ok "loopprobe: $(grep '^native: ok' /tmp/loop.log)"
else bad "loopprobe"; tail -10 /tmp/loop.log; fi
if "${XVFB[@]}" go run ./cmd/loopprobe -leaks > /tmp/leaks.log 2>&1; then
  ok "loopprobe -leaks: $(grep '^leaks: ok' /tmp/leaks.log)"
else bad "loopprobe -leaks"; tail -10 /tmp/leaks.log; fi

step "package: XDG desktop entry + icons (cmd/linuxapp)"
rm -rf /tmp/stage
if go run ./cmd/linuxapp -pkg ./cmd/term -name "Caution Terminal" -o /tmp/stage > /tmp/pkg.log 2>&1; then
  if grep -q "desktop-file-validate clean" /tmp/pkg.log; then ok "entry validates"; else bad "entry did not validate"; tail -5 /tmp/pkg.log; fi
  echo "    $(find /tmp/stage -type f | wc -l) files staged: binary + entry + notices + $(find /tmp/stage -name '*.png' | wc -l) icon sizes"
  CLASS=$(timeout 45 "${XVFB[@]}" bash -c '
    /tmp/stage/bin/caution-terminal -url ws://127.0.0.1:9/ws -title "Caution Terminal" -appid caution-terminal 2>/dev/null &
    sleep 7
    xwininfo -root -children 2>/dev/null | grep -oE "\(\"caution-terminal\" \"caution-terminal\"\)" | head -1
    pkill -f caution-terminal' 2>/dev/null)
  if [ -n "$CLASS" ]; then ok "window WM_CLASS matches StartupWMClass"; else bad "WM_CLASS did not match the entry"; fi
else
  bad "linuxapp"; tail -10 /tmp/pkg.log
fi

step "parity: goldens, native Vulkan (lavapipe) against Chromium"
if "${XVFB[@]}" go run ./cmd/goldens -out "$OUT/goldens" > /tmp/goldens.log 2>&1; then
  grep -E "^(scene|[a-z]+-[a-z]+ )" /tmp/goldens.log | sed 's/^/    /'
  ok "$(grep -cE ' ok$' /tmp/goldens.log) scenes"
else
  bad "goldens"; tail -20 /tmp/goldens.log
fi

printf '\n=== result: '
if [ "$FAILED" = 0 ]; then echo "everything passed"; else echo "SOMETHING FAILED (see above)"; fi
exit "$FAILED"
