#!/bin/bash
# Runs the fixture's formal-trace test. The assertion is the test's: 200 steps, a
# trace exported every step, at least 100 lines, every line valid JSON with the
# required fields. The earlier version printed "dot OK" and "monitor 0 violations
# OK", neither of which TestFormal looks at.
set -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

echo "=== Formal Trace Verification: TestFormal ==="

if go test -tags example -run TestFormal ./examples/harbor-panic/ -count=1 -v; then
  echo "TestFormal passed: a trace exported every step, >= 100 lines, all valid JSON."
else
  echo "TestFormal FAILED - see the output above." >&2
  exit 1
fi
