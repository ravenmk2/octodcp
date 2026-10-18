#!/usr/bin/env bash
# scripts/build.sh — local multi-platform build
set -euo pipefail

APP=octodcp
DIST=dist
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS="-s -w -X main.version=${VERSION}"

platforms=(
  linux/amd64 linux/arm64
  darwin/amd64 darwin/arm64
  windows/amd64 windows/arm64
)


# embed scenario: build the frontend (or write a web/dist placeholder until
# the frontend exists); see scripts/build-web.sh
bash scripts/build-web.sh


# --install: build for the current platform only and install to ~/.local/bin (CLI-style projects)
if [[ "${1:-}" == "--install" ]]; then
  os=$(go env GOOS)
  bin=$APP
  [[ $os == windows ]] && bin=${APP}.exe
  mkdir -p "${HOME}/.local/bin"
  CGO_ENABLED=0 go build -ldflags "$LDFLAGS" -o "${HOME}/.local/bin/${bin}" "./cmd/${APP}"
  echo "installed ${bin} to ~/.local/bin"
  exit 0
fi

mkdir -p "$DIST"
for p in "${platforms[@]}"; do
  os=${p%/*}
  arch=${p#*/}
  out="${DIST}/${APP}_${os}_${arch}"
  [[ $os == windows ]] && out="${out}.exe"
  echo "building ${os}/${arch} -> ${out}"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -ldflags "$LDFLAGS" -o "$out" "./cmd/${APP}"
done
