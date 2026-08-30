# Linux

Two things live here: a container that verifies everything about Linux that
can be verified without a Linux desktop, and a checklist of what can't be.

    linux/verify.sh          # build + tests + render + parity, artifacts in linux/out

Requires Docker. First run builds the image (`linux/Dockerfile`) and later runs
reuse it. The host's Go module cache is mounted read-only.

## What the container proves

Verified on Debian trixie / Go 1.26 / arm64, with **Mesa llvmpipe** (software
GL advertising 4.5 core, so the renderer's 4.1-core shaders compile for real)
under **Xvfb**:

| Check | Result |
| --- | --- |
| `cmd/term` builds (cgo, GLFW, X11, GL) | passes |
| `go build -tags wayland` | passes (compiles only - see below) |
| `go build ./...` | passes |
| Go test suites | 7 packages |
| TS suite (esbuild + node) | 33 tests |
| Renderer scene, offscreen | `out/linux-scene.png` |
| Full app stack: demo server + native terminal over the wire | `out/linux-demo.png` |
| goldens, both terminals, on Linux | 10/10 scenes |
| `cmd/linuxapp` staging + `desktop-file-validate` | clean |
| window `WM_CLASS` matches the entry's `StartupWMClass` | matches |

**The renderer is platform-independent.** The same `-scene` shot rendered by
Apple's GL on macOS and by Mesa's llvmpipe on Linux differs by a max cell
delta of **0.1** (`goldens a.png b.png` for the metric). Everything is SDF
math and a shared text engine, so two unrelated GL stacks agree to within
rounding.

**Cross-terminal parity holds on Linux too.** goldens' full run inside the
container (native GL against Chromium's WebGL) passes all ten scenes at the
same thresholds as macOS. The in-window menu bar is the default off macOS, so
`out/linux-demo.png` is also the first render of that widget on its home
platform.

## Packaging

`cmd/linuxapp` is the counterpart to `cmd/appbundle`. There is no bundle
format on Linux, so it writes the XDG tree instead: binary, entry, icons at
eight hicolor sizes:

    go run ./cmd/linuxapp -pkg ./cmd/demo -name "Caution Demo"            # stage
    go run ./cmd/linuxapp -pkg ./cmd/demo -name "Caution Demo" -install   # into ~/.local

    bin/caution-demo
    share/applications/caution-demo.desktop
    share/icons/hicolor/{16,24,32,48,64,128,256,512}x…/apps/caution-demo.png

It shares the generated app mark with the macOS packager
(`go/internal/appicon`), validates the entry with `desktop-file-validate`
when that tool is present, and runs on any OS - writing files needs no Linux,
so `GOOS=linux GOARCH=amd64 go run ./cmd/linuxapp …` packages from a Mac.

The entry's `StartupWMClass` is what lets a desktop tie the running window to
the installed entry (icon, grouping). The terminal advertises it as `WM_CLASS`
from `terminal.Options.AppID` (`cmd/term -appid`, defaulting to a slug of the
title), and `verify.sh` asserts the two match by reading `xprop` off a live
window - the one part of packaging that is a claim about runtime rather than
about a file.

**The Wayland gap**
GLFW 3.3 never calls `xdg_toplevel.set_app_id`, so under a native Wayland 
session the window carries no app id and a compositor cannot match it 
to the entry. Under XWayland the X11 path applies and it works. The fix upstream
is GLFW 3.4's `GLFW_WAYLAND_APP_ID`. Until then this is a real limitation of 
running on Wayland, and the first thing to check on the desktop pass.

## What needs a real desktop

- **Wayland at runtime.** The Wayland backend *compiles* (`-tags wayland`);
  nothing has run under a compositor. GLFW 3.3's Wayland support is the
  weakest part of this stack: expect to find things here.
- **Client-side decorations.** GLFW 3.3 does not draw them, so a window may
  come up undecorated under GNOME. If so, that's a packaging decision
  (ship a titlebar widget, like the demo's nib, or depend on the compositor).
- **Real GPU drivers.** llvmpipe exercises Mesa's GLSL compiler, but
  proprietary NVIDIA/AMD compilers reject things Mesa accepts. Worth a run on
  each.
- **HiDPI and fractional scaling.** DPR comes from the framebuffer/window size
  ratio
- **Frame pacing and the swap skip.** llvmpipe has no real vsync, so the
  presentation work (`MaxFPS`, the skip-when-nothing-painted path) can't be
  measured here. Count presented versus skipped frames on real hardware.
- **Fullscreen and kiosk.** `-fullscreen`, `HideCursor`,
  `ExitOnInput` (the screensaver) all negotiate with the compositor.
- **Menu bar interaction.** The widget renders, clicking, hover-switching and
  key equivalents want a real pointer under a real WM.
- **Clipboard and IME.** GLFW routes clipboard through the compositor's data
  device. Text input on Wayland (`text-input-v3`) is not something GLFW 3.3
  offers, so the browser terminal is the IME answer, same as macOS.
- **Whether the packaged app actually appears.** The entry validates and the
  window class matches, but does it show up in the launcher with the right icon,
  and groups correctly when running?

## Notes for a desktop pass

- `cmd/term -scene -shot out.png` is the smallest useful check: no server, no
  session, just the renderer. If that works, GL is fine.
- `go run ./cmd/demo` then `cmd/term -url ws://localhost:8787/ws` exercises
  the whole stack, and `-shot` makes it headless.
- goldens needs a Chrome/Chromium binary (`-chrome` or `$CHROME_BIN`). It adds
  `--no-sandbox` only when running as root, which is the container case; on a
  desktop the sandbox stays on.
- The artifacts in `linux/out` are gitignored. Commit a PNG only if it is
  evidence for something.
