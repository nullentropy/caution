# Linux

A container that checks everything about Linux that runs without a desktop,
and a list of what it can't check.

    linux/verify.sh              # build + tests + render + probes + parity, artifacts in linux/out
    IMAGE=name linux/verify.sh   # use (or build) another image name

Requires Docker. The first run builds the image (`linux/Dockerfile`) and later
runs reuse it. After changing `linux/Dockerfile`, remove the image (or pick a
new `IMAGE`) so it rebuilds. The host's Go module cache is mounted read-only
and the Go build cache lives in a named volume, so shaderx's vendored glslang
and SPIRV-Cross compile once (about a minute) instead of on every run.

## What the container checks

The container runs Debian trixie with **Mesa lavapipe** (software Vulkan)
under **Xvfb**. The native terminal draws through `terminal/gpu/vulkan`.

| Check | Passes when |
| --- | --- |
| `cmd/term` builds (cgo, GLFW, X11, Vulkan) | `libvulkan.so.1` is the only GPU library linked |
| `go build -tags wayland` | it compiles |
| `go build ./...` | it compiles |
| Go test suites | they pass |
| Renderer scene, offscreen (hidden window, textures only) | `out/linux-scene.png` is written |
| Same shot under `VK_LAYER_KHRONOS_validation` | no validation errors |
| Live window: swapchain present, then a resize to 700x450 | `out/linux-window.png` and `out/linux-window-resized.png` have content, no validation errors |
| Full app stack: demo server + native terminal over the wire | `out/linux-demo.png` is written |
| `cmd/damageprobe` (damage path vs. a fresh render) | every step is identical |
| `cmd/loopprobe`, `cmd/loopprobe -leaks` | they pass |
| goldens, native Vulkan against Chromium | every scene is within the drift limits |
| `cmd/linuxapp` staging + `desktop-file-validate` | the entry validates |
| window `WM_CLASS` | it matches the entry's `StartupWMClass` |

goldens compares the native terminal on lavapipe with Chromium's WebGL.
Everything is SDF math and a shared text engine, so unrelated GPU stacks
agree to within rounding. The in-window menu bar is the default off macOS, so
`out/linux-demo.png` also shows that widget.

**How the backend maps the device interface.** The renderer's GLSL goes
through `terminal/shaderx` to SPIR-V. Each draw pushes its uniform blocks
(copied into a per-frame host-visible ring) and its texture as push
descriptors. Render passes are dynamic rendering (Vulkan 1.3) and open
lazily, with a pending clear as the load op. Window output is a draw from the
scene texture onto the swapchain image (B8G8R8A8_UNORM when offered), and the
swapchain is rebuilt when the window size changes or present reports
`VK_ERROR_OUT_OF_DATE_KHR` or `VK_SUBOPTIMAL_KHR`. Vulkan clip space is y-down,
so every target draws top-down and `FlipY` is +1 everywhere.

**GL at build time only.** go-gl/glfw's C sources include `GL/gl.h` and its
cgo flags pass `-lGL` on Linux, so `libgl1-mesa-dev` stays in the image. No
symbol from it is referenced, and with `--as-needed` (Debian's default) the
binary does not link libGL. `inside.sh` checks the `NEEDED` entries.

**Debugging.** `CAUTION_VK_VALIDATION=1` loads the Khronos validation layer
(Debian: `vulkan-validationlayers`). Its messages go to stdout.

## Packaging

`cmd/linuxapp` is the counterpart to `cmd/appbundle`. There is no bundle
format on Linux, so it writes the XDG tree instead.

    go run ./cmd/linuxapp -pkg ./cmd/demo -name "Caution Demo"            # stage
    go run ./cmd/linuxapp -pkg ./cmd/demo -name "Caution Demo" -install   # into ~/.local

    bin/caution-demo
    share/applications/caution-demo.desktop
    share/doc/caution-demo/THIRD_PARTY_NOTICES.txt
    share/icons/hicolor/{16,24,32,48,64,128,256,512}x…/apps/caution-demo.png

It shares the generated app mark with the macOS packager
(`internal/appicon`), validates the entry with `desktop-file-validate`
when that tool is present, and runs on any OS. Writing files needs no Linux,
so `GOOS=linux GOARCH=amd64 go run ./cmd/linuxapp …` packages from a Mac.

The entry's `StartupWMClass` is what lets a desktop tie the running window to
the installed entry (icon, grouping). The terminal advertises it as `WM_CLASS`
from `terminal.Options.AppID` (`cmd/term -appid`, defaulting to a slug of the
title), and `verify.sh` asserts the two match by reading `xprop` off a live
window.

**App id on Wayland.** GLFW 3.3 never calls `xdg_toplevel.set_app_id`, so
under a native Wayland session the window carries no app id and a compositor
cannot match it to the entry. Under XWayland the X11 path applies and it
works.

## What the container can't check

- **Wayland.** Xvfb is an X server, so the Vulkan device's
  `VK_KHR_wayland_surface` path only compiles here.
- **Client-side decorations.** GLFW 3.3 does not draw them, so a window may
  come up undecorated under GNOME.
- **Hardware drivers.** lavapipe is a conformant Vulkan driver, but hardware
  drivers differ in memory types, swapchain formats and present modes. The
  backend needs Vulkan 1.3 and push descriptors (VK_KHR_push_descriptor or
  1.4 core).
- **HiDPI and fractional scaling.** DPR comes from the framebuffer/window size
  ratio.
- **Frame pacing and the swap skip.** The swapchain uses FIFO (vsync), and
  Xvfb has no real display refresh, so `MaxFPS` and the skip when nothing
  painted can't be measured.
- **Fullscreen and kiosk.** `-fullscreen`, `HideCursor` and `ExitOnInput`
  (the screensaver) all negotiate with the compositor.
- **Menu bar interaction.** Clicking, hover-switching and key equivalents need
  a real pointer under a real window manager.
- **Clipboard and IME.** GLFW routes clipboard through the compositor's data
  device. GLFW 3.3 offers no Wayland text input (`text-input-v3`), so the
  browser terminal is the IME answer, same as macOS.
- **Launcher integration.** Whether the installed entry shows up in the
  launcher with the right icon and groups the running window.

## Checking on a desktop

- `cmd/term -scene -shot out.png` is the smallest useful check: no server, no
  session, just the renderer. `cmd/term -scene` without `-shot` adds the
  swapchain. `vulkaninfo --summary` shows which driver the loader picked.
- `go run ./cmd/demo` then `cmd/term -url ws://localhost:8787/ws` exercises
  the whole stack, and `-shot` makes it headless.
- goldens needs a Chrome/Chromium binary (`-chrome` or `$CHROME_BIN`). It adds
  `--no-sandbox` only when running as root, which is the container case.
- The artifacts in `linux/out` are gitignored. Commit a PNG only if it is
  evidence for something.
