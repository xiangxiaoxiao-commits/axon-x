#!/bin/bash
# Migrate axon knowledge graph from path-encoded slugs to named namespaces.
# Run once, then delete.
set -euo pipefail

DATA_DIR="$HOME/Library/Application Support/axon"
GC="$DATA_DIR/graphcache"
GR="$DATA_DIR/graphs"

echo "=== Migrating graphcache ==="

# 1. Merge -Users-xiangxiao-xx and -Users-xiangxiao-xx-axon into "axon"
if [ -d "$GC/-Users-xiangxiao-xx" ] || [ -d "$GC/-Users-xiangxiao-xx-axon" ]; then
  mkdir -p "$GC/axon"
  if [ -d "$GC/-Users-xiangxiao-xx" ]; then
    cp "$GC/-Users-xiangxiao-xx"/*.json "$GC/axon/" 2>/dev/null || true
    echo "  Copied -Users-xiangxiao-xx -> axon"
  fi
  if [ -d "$GC/-Users-xiangxiao-xx-axon" ]; then
    cp "$GC/-Users-xiangxiao-xx-axon"/*.json "$GC/axon/" 2>/dev/null || true
    echo "  Copied -Users-xiangxiao-xx-axon -> axon"
  fi
fi

# 2. -Users-xiangxiao-projects -> gaia (mixed gaia/gaiac/glite, will clean later)
if [ -d "$GC/-Users-xiangxiao-projects" ]; then
  mv "$GC/-Users-xiangxiao-projects" "$GC/gaia"
  echo "  Renamed -Users-xiangxiao-projects -> gaia"
fi

# 3. -Users-xiangxiao-projects-mono-glite -> glite
if [ -d "$GC/-Users-xiangxiao-projects-mono-glite" ]; then
  mv "$GC/-Users-xiangxiao-projects-mono-glite" "$GC/glite"
  echo "  Renamed -Users-xiangxiao-projects-mono-glite -> glite"
fi

# 4. -Users-xiangxiao-Desktop-xx-service-personal -> personal
if [ -d "$GC/-Users-xiangxiao-Desktop-xx-service-personal" ]; then
  mv "$GC/-Users-xiangxiao-Desktop-xx-service-personal" "$GC/personal"
  echo "  Renamed -Users-xiangxiao-Desktop-xx-service-personal -> personal"
fi

# 5. -Users-xiangxiao-Desktop-xx-service-personal-couple-space -> couple-space
if [ -d "$GC/-Users-xiangxiao-Desktop-xx-service-personal-couple-space" ]; then
  mv "$GC/-Users-xiangxiao-Desktop-xx-service-personal-couple-space" "$GC/couple-space"
  echo "  Renamed ...couple-space -> couple-space"
fi

# Clean up old dirs (only the ones we copied from, not moved)
rm -rf "$GC/-Users-xiangxiao-xx" "$GC/-Users-xiangxiao-xx-axon"
echo "  Removed old -Users-xiangxiao-xx and -Users-xiangxiao-xx-axon"

echo ""
echo "=== Migrating graphs ==="

# Rename graph JSON files to match new namespaces
if [ -f "$GR/-Users-xiangxiao-xx.json" ]; then
  cp "$GR/-Users-xiangxiao-xx.json" "$GR/axon.json"
  echo "  Copied -Users-xiangxiao-xx.json -> axon.json"
fi
if [ -f "$GR/-Users-xiangxiao-xx-axon.json" ]; then
  # axon.json already exists from above, this is supplementary
  rm -f "$GR/-Users-xiangxiao-xx-axon.json"
  echo "  Removed -Users-xiangxiao-xx-axon.json (merged into axon)"
fi
if [ -f "$GR/-Users-xiangxiao-projects.json" ]; then
  mv "$GR/-Users-xiangxiao-projects.json" "$GR/gaia.json"
  echo "  Renamed -Users-xiangxiao-projects.json -> gaia.json"
fi
if [ -f "$GR/-Users-xiangxiao-projects-mono-glite.json" ]; then
  mv "$GR/-Users-xiangxiao-projects-mono-glite.json" "$GR/glite.json"
  echo "  Renamed ...mono-glite.json -> glite.json"
fi
if [ -f "$GR/-Users-xiangxiao-Desktop-xx-service-personal.json" ]; then
  mv "$GR/-Users-xiangxiao-Desktop-xx-service-personal.json" "$GR/personal.json"
  echo "  Renamed ...personal.json -> personal.json"
fi
if [ -f "$GR/-Users-xiangxiao-Desktop-xx-service-personal-couple-space.json" ]; then
  mv "$GR/-Users-xiangxiao-Desktop-xx-service-personal-couple-space.json" "$GR/couple-space.json"
  echo "  Renamed ...couple-space.json -> couple-space.json"
fi

# Clean up old graph file
rm -f "$GR/-Users-xiangxiao-xx.json"

echo ""
echo "=== Done ==="
echo "Namespaces available:"
ls "$GC"
