#!/usr/bin/env bash
# Package the built Axon.app into a distributable .dmg.
# Prerequisite: run `wails build -clean` first so build/bin/Axon.app exists.
#
# Usage: scripts/make-dmg.sh [version]
#   version defaults to the CFBundleShortVersionString in the built app.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="$ROOT/build/bin/Axon.app"

[[ -d "$APP" ]] || { echo "Axon.app not found — run 'wails build -clean' first" >&2; exit 1; }

VERSION="${1:-$(/usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' "$APP/Contents/Info.plist")}"
DMG="$ROOT/build/bin/Axon-$VERSION.dmg"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

# Stage the app next to an /Applications symlink for drag-to-install.
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

rm -f "$DMG"
hdiutil create -volname "Axon" -srcfolder "$STAGE" -ov -format UDZO "$DMG"

echo "Built: $DMG"
