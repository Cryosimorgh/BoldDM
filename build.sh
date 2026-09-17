#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

TARGET_OS="${BOLTDM_TARGET_OS:-$(go env GOOS)}"
TARGET_ARCH="${BOLTDM_TARGET_ARCH:-$(go env GOARCH)}"

mkdir -p dist

if [[ "${BOLTDM_SKIP_CHECKS:-0}" != "1" ]]; then
  go test ./...
  go vet ./...
fi

if [[ "$TARGET_OS" == "linux" ]]; then
  package="boltdm-linux-${TARGET_ARCH}"
  outdir="dist/${package}"
  rm -rf "$outdir"
  mkdir -p "$outdir"

  CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_ARCH" go build -trimpath -ldflags='-s -w' -o "$outdir/boltdm" ./cmd/boltdm
  CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_ARCH" go build -trimpath -ldflags='-s -w' -o "$outdir/boltdm-updater" ./cmd/updater
  cp -f install-linux.sh "$outdir/install-linux.sh"
  cp -f packaging/linux/boltdm.svg "$outdir/boltdm.svg"
  cp -f LICENSE "$outdir/LICENSE"
  chmod +x "$outdir/boltdm" "$outdir/boltdm-updater" "$outdir/install-linux.sh"

  archive="dist/BoltDM-linux-${TARGET_ARCH}.tar.gz"
  tar -czf "$archive" -C dist "$package"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$archive" > "${archive}.sha256"
  else
    shasum -a 256 "$archive" > "${archive}.sha256"
  fi
  echo "Built $archive"
else
  output="dist/boltdm-${TARGET_OS}-${TARGET_ARCH}"
  CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -trimpath -ldflags='-s -w' -o "$output" ./cmd/boltdm
  echo "Built $output"
fi
