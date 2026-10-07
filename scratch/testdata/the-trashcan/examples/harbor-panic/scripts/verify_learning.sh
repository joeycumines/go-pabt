#!/bin/bash
# Runs the fixture's no-good learning test. The only assertion is the test's:
# 500 steps produce fewer than 5 cycles. The earlier version also printed
# "dead-ends cached OK" and "recovery OK"; TestNoGood logs the dead-end count but
# does not assert it, so those lines were claims about nothing.
set -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

echo "=== No-Good Learning Verification: TestNoGood ==="

if go test -tags example -run TestNoGood ./examples/harbor-panic/ -count=1 -v; then
  echo "TestNoGood passed: 500 steps, fewer than 5 cycles. Its own output reports the dead-end count."
else
  echo "TestNoGood FAILED - see the output above." >&2
  exit 1
fi
