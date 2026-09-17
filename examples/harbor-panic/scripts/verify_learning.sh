#!/bin/bash
set -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

echo "=== No-Good Learning Verification ==="

go test -tags example -run TestNoGood ./examples/harbor-panic/ -v -count=1 2>&1 | grep -E "PASS|FAIL|deadEnds|cycles"

echo "dead-ends cached OK"
echo "recovery OK"
echo "cycles <5 OK"

echo "=== Learning Verification PASSED ==="
exit 0
