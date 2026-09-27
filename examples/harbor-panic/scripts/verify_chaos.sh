#!/bin/bash
# Runs the fixture's chaos test. The assertions are the test's, not this script's:
# 2000 ticks, a monotonic revision, no safety violation, and a surviving forklift.
# The earlier version piped go test into grep, which meant a failing test still
# exited 0, and it then printed four success lines the test never measured.
set -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

echo "=== Chaos Monkey Verification: TestChaos ==="

if go test -tags example -run TestChaos ./examples/harbor-panic/ -count=1 -timeout 300s -v; then
  echo "TestChaos passed: 2000 ticks, revision monotonic, 0 safety violations, forklift survived."
else
  echo "TestChaos FAILED - see the output above." >&2
  exit 1
fi
