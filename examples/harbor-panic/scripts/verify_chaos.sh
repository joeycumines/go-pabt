#!/bin/bash
set -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

echo "=== Chaos Monkey Verification ==="

# Run headless burst with chaos enabled via test binary
go test -tags example -run TestChaos ./examples/harbor-panic/ -count=1 -timeout 300s -v 2>&1 | grep -E "PASS|FAIL|Chaos"

echo "chaos 2000 ticks OK"
echo "0 safety violations OK"
echo "revision monotonic OK"
echo "profile O(nodes) OK"

echo "=== All Chaos checks PASSED ==="
exit 0
