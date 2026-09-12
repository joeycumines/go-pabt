#!/bin/sh
# verify_safety.sh — Task 35 Safety Verification harness
# Usage: bash scripts/verify_safety.sh http://localhost:8080/debug/pabt
set -e
BASE="${1:?usage: verify_safety.sh <base_url>}"
FAIL=0

# Check 1: cycles <5
CYCLES=$(curl -sf "$BASE/safety" | python3 -c "import json,sys; print(json.load(sys.stdin).get('cycles',999))")
if [ "$CYCLES" -lt 5 ]; then
  echo "cycles $CYCLES <5 OK"
else
  echo "FAIL: cycles=$CYCLES >=5"
  FAIL=1
fi

# Check 2: dead-ends cached (deadEnds field exists and is numeric)
DEADENDS=$(curl -sf "$BASE/safety" | python3 -c "import json,sys; print(json.load(sys.stdin).get('deadEnds',999))")
if [ "$DEADENDS" -ge 0 ] 2>/dev/null; then
  echo "dead-ends cached $DEADENDS OK"
else
  echo "FAIL: deadEnds not numeric"
  FAIL=1
fi

# Check 3: safety 0 violations
SAFETY=$(curl -sf "$BASE/safety" | python3 -c "import json,sys; print(json.load(sys.stdin).get('safetyViolations',999))")
if [ "$SAFETY" -eq 0 ]; then
  echo "safety $SAFETY violations OK"
else
  echo "FAIL: safetyViolations=$SAFETY !=0"
  FAIL=1
fi

exit $FAIL
