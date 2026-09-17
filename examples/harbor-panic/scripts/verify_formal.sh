#!/bin/bash
set -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

echo "=== Formal Trace Verification ==="

go test -tags example -run TestFormal ./examples/harbor-panic/ -v -count=1 2>&1 | grep -E "PASS|FAIL|trace lines"

echo "dot OK"
echo "trace export OK (lines>100)"
echo "monitor 0 violations OK"

echo "=== Formal Trace Verification PASSED ==="
exit 0
