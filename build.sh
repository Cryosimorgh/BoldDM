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

  if [[ "${BOLTDM_BUNDLE_YTDLP:-0}" == "1" ]]; then
    case "$TARGET_ARCH" in
      amd64) ytdlp_url="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux" ;;
      arm64) ytdlp_url="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux_aarch64" ;;
      *) echo "No bundled yt-dlp binary configured for Linux/$TARGET_ARCH" >&2; exit 1 ;;
    esac
    if command -v curl >/dev/null 2>&1; then
      curl --fail --location --retry 3 --retry-delay 2 "$ytdlp_url" -o "$outdir/yt-dlp"
    elif command -v wget >/dev/null 2>&1; then
      wget -O "$outdir/yt-dlp" "$ytdlp_url"
    else
      echo "curl or wget is required to bundle yt-dlp" >&2
      exit 1
    fi
  fi

  chmod +x "$outdir/boltdm" "$outdir/boltdm-updater" "$outdir/install-linux.sh"
  if [[ -f "$outdir/yt-dlp" ]]; then
    chmod +x "$outdir/yt-dlp"
  fi

  archive="dist/BoltDM-linux-${TARGET_ARCH}.tar.gz"
  tar -czf "$archive" -C dist "$package"
  archive_name="$(basename "$archive")"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$(dirname "$archive")" && sha256sum "$archive_name") > "${archive}.sha256"
  else
    (cd "$(dirname "$archive")" && shasum -a 256 "$archive_name") > "${archive}.sha256"
  fi
  echo "Built $archive"
else
  output="dist/boltdm-${TARGET_OS}-${TARGET_ARCH}"
  CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -trimpath -ldflags='-s -w' -o "$output" ./cmd/boltdm
  echo "Built $output"
fi
