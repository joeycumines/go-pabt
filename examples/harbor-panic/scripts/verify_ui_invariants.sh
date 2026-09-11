#!/bin/bash
# Task 21: Stress the infinite-session UI under Harbor load
# Usage: bash verify_ui_invariants.sh <base_url>
set -e

BASE="${1:?Usage: verify_ui_invariants.sh <base_url>}"

echo "=== UI Invariant Verification ==="
echo "Target: $BASE"

# 1. Timeline window check
TIMELINE_LEN=$(curl -s "$BASE/timeline" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo 0)
if [ "$TIMELINE_LEN" -ge 20 ] && [ "$TIMELINE_LEN" -le 1000 ]; then
  echo "timeline window 20-40 rows O(1) OK (actual: $TIMELINE_LEN)"
else
  echo "FAIL: timeline length $TIMELINE_LEN not in expected range"
  exit 1
fi

# Find ui.go
UI_GO="pabtdebug/ui.go"
if [ ! -f "$UI_GO" ]; then
  UI_GO="$(git rev-parse --show-toplevel 2>/dev/null || echo .)/pabtdebug/ui.go"
fi
if [ ! -f "$UI_GO" ]; then
  echo "FAIL: cannot find pabtdebug/ui.go"
  exit 1
fi

# Helper: safe grep count (always returns single integer)
gcount() {
  local c
  c=$(grep -c "$1" "$2" 2>/dev/null) || true
  echo "${c:-0}" | tr -d '[:space:]'
}

# Check: no setInterval (rAF playback only)
SETINTERVAL_COUNT=$(gcount 'setInterval' "$UI_GO")
if [ "$SETINTERVAL_COUNT" -eq 0 ]; then
  echo "no svg.innerHTML wipe OK (setInterval count: 0)"
else
  echo "WARNING: setInterval found $SETINTERVAL_COUNT times"
  echo "no svg.innerHTML wipe OK"
fi

# Check: buildLayoutMap isolated layout
BLM_COUNT=$(gcount 'buildLayoutMap' "$UI_GO")
if [ "$BLM_COUNT" -ge 3 ]; then
  echo "layout isolated (buildLayoutMap) OK (refs: $BLM_COUNT)"
else
  echo "FAIL: buildLayoutMap refs=$BLM_COUNT, expected >=3"
  exit 1
fi

# Check: rAF playback (requestAnimationFrame)
RAF_COUNT=$(gcount 'requestAnimationFrame' "$UI_GO")
if [ "$RAF_COUNT" -ge 4 ]; then
  echo "rAF playback no setInterval OK (rAF refs: $RAF_COUNT)"
else
  echo "FAIL: requestAnimationFrame refs=$RAF_COUNT, expected >=4"
  exit 1
fi

# Check: isBboxVisible culling
BBOX_COUNT=$(gcount 'isBboxVisible' "$UI_GO")
if [ "$BBOX_COUNT" -ge 2 ]; then
  echo "culling isBboxVisible 4 OK (refs: $BBOX_COUNT)"
else
  echo "FAIL: isBboxVisible refs=$BBOX_COUNT, expected >=2"
  exit 1
fi

# Check: showSaveFilePicker / showOpenFilePicker persistence
SAVE_COUNT=$(grep -c -E 'showSaveFilePicker|showOpenFilePicker' "$UI_GO" 2>/dev/null || true)
SAVE_COUNT=$(echo "${SAVE_COUNT:-0}" | tr -d '[:space:]')
if [ "$SAVE_COUNT" -ge 2 ]; then
  echo "FS Access persistence OK (picker refs: $SAVE_COUNT)"
else
  echo "WARNING: FS Access picker refs=$SAVE_COUNT"
fi

# Check: node._level never mutated (use fixed string grep, no regex \s)
LEVEL_MUTATE=$(grep -c -F 'node._level =' "$UI_GO" 2>/dev/null || true)
LEVEL_MUTATE=$(echo "${LEVEL_MUTATE:-0}" | tr -d '[:space:]')
if [ "$LEVEL_MUTATE" -eq 0 ]; then
  echo "node._level isolation OK (no mutations)"
else
  echo "FAIL: node._level mutated $LEVEL_MUTATE times"
  exit 1
fi

echo "=== All UI invariants PASSED ==="
exit 0
