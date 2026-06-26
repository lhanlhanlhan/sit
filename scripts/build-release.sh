#!/usr/bin/env bash
# Cross-compile the `sit` binary for the supported platform matrix.
# Usage: scripts/build-release.sh [version]
# Output: dist/sit_<os>_<arch>[.exe]
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="dist"
LDFLAGS="-s -w -X main.Version=${VERSION}"

# Pin toolchain to avoid auto-download of a newer Go pulling incompatible deps.
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.23.0}"
export CGO_ENABLED=0   # pure-Go (modernc.org/sqlite), fully static cross-compile

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

rm -rf "$OUT"
mkdir -p "$OUT"

for p in "${PLATFORMS[@]}"; do
  os="${p%/*}"
  arch="${p#*/}"
  bin="$OUT/sit_${os}_${arch}"
  echo "==> building $bin (version=$VERSION)"
  GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" -o "$bin" ./cmd/sit
done

echo "==> done. artifacts in $OUT/"
ls -lh "$OUT"
