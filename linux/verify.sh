#!/usr/bin/env bash
# The Linux verification pass. Runs everything that can be checked without a
# real desktop: the cgo build (X11 and Wayland), the test suites, a renderer
# shot, a full app stack (server + native terminal over the wire), and
# goldens' cross-terminal parity with Chromium. Artifacts land in linux/out.
#
#   linux/verify.sh            # from the repo root, needs Docker
#   IMAGE=caution-linux linux/verify.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${IMAGE:-caution-linux}"
OUT="$ROOT/linux/out"
mkdir -p "$OUT"

if ! docker image inspect "$IMAGE" > /dev/null 2>&1; then
  echo "building $IMAGE ..."
  docker build -q -t "$IMAGE" -f "$ROOT/linux/Dockerfile" "$ROOT/linux" || exit 1
fi

# The module cache is mounted read-only: module source is platform-neutral,
# so the container reuses the host's downloads instead of refetching.
exec docker run --rm \
  -v "$ROOT":/caution \
  -v "$(go env GOMODCACHE)":/go/pkg/mod:ro \
  -w /caution/go \
  "$IMAGE" bash /caution/linux/inside.sh
