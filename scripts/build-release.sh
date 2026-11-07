#!/bin/sh
# Build the release archives into dist/: flipchat for macOS and Linux (amd64, arm64),
# the Python wheel and sdist, and SHA256SUMS.
#   scripts/build-release.sh v0.1.0
set -eu
version=${1:?usage: scripts/build-release.sh vX.Y.Z}
root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
rm -rf "$dist" && mkdir -p "$dist"

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  os=${target%/*} arch=${target#*/}
  name="flipchat_${os}_${arch}"
  mkdir -p "$dist/$name"
  (cd "$root/tui" && CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
    go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$dist/$name/flipchat" .)
  cp "$root/LICENSE" "$root/README.md" "$dist/$name/"
  tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
  rm -rf "${dist:?}/$name"
done

(cd "$root" && uv build -q --out-dir "$dist")

cd "$dist"
if command -v sha256sum >/dev/null; then sha256sum -- * >SHA256SUMS; else shasum -a 256 -- * >SHA256SUMS; fi
ls -l "$dist"
