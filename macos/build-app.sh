#!/usr/bin/env bash
# Assemble ipcprobe.app (SwiftUI front end + bundled Go helper) and wrap it in a .dmg.
#
#   macos/build-app.sh [--version X.Y.Z] [--helper path/to/ipcprobe] [--no-dmg]
#
# --version  the version stamped into Info.plist (default: `git describe`, or "dev")
# --helper   a prebuilt universal `ipcprobe` binary to bundle (default: build one from ../ with the Go toolchain,
#            amd64 + arm64 merged with lipo — the same thing GoReleaser's universal_binaries does)
#
# Output: macos/dist/ipcprobe.app and macos/dist/ipcprobe_<version>_macOS.dmg
#
# The app is signed ad hoc (`codesign -s -`): that gives it a stable identity for the Local Network permission
# and lets it launch, but Gatekeeper still flags a download as unidentified. See macos/README.md.
set -euo pipefail

VERSION=""
HELPER=""
MAKE_DMG=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --helper)  HELPER="$2"; shift 2 ;;
    --no-dmg)  MAKE_DMG=0; shift ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done
# --helper is relative to where the script was invoked; resolve it before we cd into macos/.
if [[ -n "$HELPER" ]]; then
  [[ -f "$HELPER" ]] || { echo "--helper: $HELPER not found" >&2; exit 2; }
  HELPER="$(cd "$(dirname "$HELPER")" && pwd)/$(basename "$HELPER")"
fi

cd "$(dirname "$0")"
ROOT="$(cd .. && pwd)"
if [[ -z "$VERSION" ]]; then
  VERSION="$(git -C "$ROOT" describe --tags --always 2>/dev/null | sed 's/^v//' || true)"
  VERSION="${VERSION:-dev}"
fi

APP=dist/ipcprobe.app
rm -rf dist
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

echo "==> helper (Go)"
if [[ -z "$HELPER" ]]; then
  mkdir -p dist/helper
  LDFLAGS="-s -w -X main.version=$VERSION"
  ( cd "$ROOT" &&
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o macos/dist/helper/amd64 ./cmd/ipcprobe &&
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o macos/dist/helper/arm64 ./cmd/ipcprobe )
  lipo -create -output dist/helper/ipcprobe dist/helper/amd64 dist/helper/arm64
  HELPER=dist/helper/ipcprobe
fi
# Named ipcprobe-cli, not ipcprobe: the app's own executable is IPCProbe, and on a case-insensitive filesystem
# (APFS default) "ipcprobe" and "IPCProbe" are the same file — the two would overwrite each other.
cp "$HELPER" "$APP/Contents/MacOS/ipcprobe-cli"
chmod 755 "$APP/Contents/MacOS/ipcprobe-cli"

echo "==> app (Swift)"
# One build per architecture, merged with lipo. (`swift build --arch a --arch b` would do it in one go but needs
# full Xcode's xcbuild; per-arch --triple builds work with just the Command Line Tools.)
mkdir -p dist/app
NATIVE="$(uname -m)"; [[ "$NATIVE" == "x86_64" ]] || NATIVE=arm64
SLICES=()
for ARCH in arm64 x86_64; do
  if swift build -c release --product IPCProbe --triple "${ARCH}-apple-macosx" >/dev/null; then
    cp "$(swift build -c release --triple "${ARCH}-apple-macosx" --show-bin-path)/IPCProbe" "dist/app/$ARCH"
    SLICES+=("dist/app/$ARCH")
  else
    echo "warning: could not build the $ARCH slice of the app; continuing without it" >&2
  fi
done
if [[ ${#SLICES[@]} -eq 0 ]]; then
  swift build -c release --product IPCProbe
  cp "$(swift build -c release --show-bin-path)/IPCProbe" "dist/app/$NATIVE"
  SLICES=("dist/app/$NATIVE")
fi
lipo -create -output "$APP/Contents/MacOS/IPCProbe" "${SLICES[@]}"
lipo -info "$APP/Contents/MacOS/IPCProbe"

sed "s/__VERSION__/$VERSION/g" Resources/Info.plist > "$APP/Contents/Info.plist"
printf 'APPL????' > "$APP/Contents/PkgInfo"
if [[ -f Resources/AppIcon.icns ]]; then
  cp Resources/AppIcon.icns "$APP/Contents/Resources/AppIcon.icns"
  /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon" "$APP/Contents/Info.plist"
fi

echo "==> sign (ad hoc)"
# codesign refuses bundles carrying extended attributes (Finder info, quarantine flags from an unzip, ...).
xattr -cr "$APP"
# Inside-out, no --deep (Apple TN3127): sign nested code explicitly, then the bundle.
codesign --force --sign - --identifier io.github.alexandruispas659.ipcprobe.helper "$APP/Contents/MacOS/ipcprobe-cli"
codesign --force --sign - "$APP"
codesign --verify --verbose=2 "$APP"

if [[ $MAKE_DMG -eq 1 ]]; then
  echo "==> dmg"
  STAGE=dist/dmg
  mkdir -p "$STAGE"
  cp -R "$APP" "$STAGE/"
  ln -s /Applications "$STAGE/Applications"
  DMG="dist/ipcprobe_${VERSION}_macOS.dmg"
  hdiutil create -volname "ipcprobe" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
  rm -rf "$STAGE"
  echo "built $DMG"
fi
echo "built $APP"
