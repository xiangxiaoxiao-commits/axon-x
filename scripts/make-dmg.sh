#!/usr/bin/env bash
# Package the built Axon-x.app into a distributable .dmg, bundling axon-mcp.
# Prerequisite: run `wails build -clean` first so build/bin/Axon-x.app exists.
#
# Usage: scripts/make-dmg.sh [version]
#   version defaults to the CFBundleShortVersionString in the built app.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="$ROOT/build/bin/Axon-x.app"

[[ -d "$APP" ]] || { echo "Axon-x.app not found — run 'wails build -clean' first" >&2; exit 1; }

# Ship the MCP server inside the bundle, next to the main binary, so the app's
# one-click "接入 Claude Code" can locate it via os.Executable(). Build it fresh
# so the shipped copy matches this source tree.
echo "Building axon-mcp into the app bundle…"
go build -o "$APP/Contents/MacOS/axon-mcp" "$ROOT/cmd/axon-mcp"
# Re-sign ad-hoc: adding a file invalidates the bundle's existing signature.
codesign --force --deep --sign - "$APP" 2>/dev/null || true

VERSION="${1:-$(/usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' "$APP/Contents/Info.plist")}"
DMG="$ROOT/build/bin/Axon-x-$VERSION.dmg"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

# Stage the app next to an /Applications symlink for drag-to-install.
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

rm -f "$DMG"
hdiutil create -volname "Axon-x" -srcfolder "$STAGE" -ov -format UDZO "$DMG"

echo "Built: $DMG"
