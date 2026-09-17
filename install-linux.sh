#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
PREFIX="${BOLTDM_PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
APP_DIR="$PREFIX/share/applications"
ICON_DIR="$PREFIX/share/icons/hicolor/scalable/apps"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/boltdm-install"

mkdir -p "$BIN_DIR" "$APP_DIR" "$ICON_DIR" "$CACHE_DIR"

find_binary() {
  local candidate
  for candidate in \
    "$ROOT/boltdm" \
    "$ROOT/dist/boltdm-linux-$(go env GOARCH 2>/dev/null || uname -m)/boltdm" \
    "$ROOT/dist/boltdm"; do
    if [[ -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

if BINARY="$(find_binary)"; then
  :
elif command -v go >/dev/null 2>&1 && [[ -f "$ROOT/go.mod" ]]; then
  echo "No prebuilt Linux binary found; building BoltDM from source..."
  BINARY="$CACHE_DIR/boltdm"
  CGO_ENABLED=0 GOOS=linux GOARCH="$(go env GOARCH)" go build -trimpath -ldflags='-s -w' -o "$BINARY" "$ROOT/cmd/boltdm"
else
  echo "Could not find a BoltDM Linux binary and Go is unavailable." >&2
  echo "Use a BoltDM Linux release archive or install Go 1.23+ and run ./build.sh first." >&2
  exit 1
fi

install -m 0755 "$BINARY" "$BIN_DIR/boltdm"

ICON_SOURCE=""
for candidate in "$ROOT/boltdm.svg" "$ROOT/packaging/linux/boltdm.svg"; do
  if [[ -f "$candidate" ]]; then
    ICON_SOURCE="$candidate"
    break
  fi
done
if [[ -n "$ICON_SOURCE" ]]; then
  install -m 0644 "$ICON_SOURCE" "$ICON_DIR/boltdm.svg"
fi

cat > "$APP_DIR/boltdm.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=BoltDM
Comment=Fast local download manager
Exec=$BIN_DIR/boltdm
Icon=boltdm
Terminal=false
Categories=Network;Utility;
StartupNotify=true
EOF
chmod 0644 "$APP_DIR/boltdm.desktop"

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -f -t "$PREFIX/share/icons/hicolor" >/dev/null 2>&1 || true
fi

if ! command -v xdg-open >/dev/null 2>&1; then
  echo "Warning: xdg-open is missing. Install xdg-utils so BoltDM can open the dashboard and folders." >&2
fi
if ! command -v aria2c >/dev/null 2>&1; then
  echo "Note: aria2c is optional but required for FTP, SFTP, torrent, magnet and Metalink transfers." >&2
fi
if ! command -v zenity >/dev/null 2>&1; then
  echo "Note: install zenity for the native folder picker; manual paths still work." >&2
fi

cat <<EOF
BoltDM installed.

Executable: $BIN_DIR/boltdm
Desktop entry: $APP_DIR/boltdm.desktop

If '$BIN_DIR' is not on PATH, add this to your shell profile:
  export PATH="$BIN_DIR:\$PATH"

Launch from your application menu or run:
  $BIN_DIR/boltdm
EOF
