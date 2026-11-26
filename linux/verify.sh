#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${IMAGE:-caution-linux}"
OUT="$ROOT/linux/out"
mkdir -p "$OUT"

if ! docker image inspect "$IMAGE" > /dev/null 2>&1; then
  echo "building $IMAGE ..."
  docker build -q -t "$IMAGE" -f "$ROOT/linux/Dockerfile" "$ROOT/linux" || exit 1
fi

exec docker run --rm \
  -v "$ROOT":/caution \
  -v "$(go env GOMODCACHE)":/go/pkg/mod:ro \
  -v "${IMAGE}-gocache":/root/.cache/go-build \
  -w /caution \
  "$IMAGE" bash /caution/linux/inside.sh
