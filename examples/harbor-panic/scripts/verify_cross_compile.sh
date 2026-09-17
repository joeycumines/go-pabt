#!/bin/bash
set -e
echo "=== Cross-Compilation Verification ==="

FAIL=0

for pair in "linux/amd64" "windows/amd64" "darwin/arm64"; do
  GOOS="${pair%%/*}"
  GOARCH="${pair##*/}"
  if GOOS="$GOOS" GOARCH="$GOARCH" go build -tags example ./examples/harbor-panic 2>/dev/null; then
    echo "$GOOS/$GOARCH OK"
  else
    echo "$GOOS/$GOARCH FAIL"
    FAIL=1
  fi
done

rm -f harbor-panic harbor-panic.exe

if [[ "$FAIL" -eq 0 ]]; then
  echo "cross-compile: all targets OK"
  exit 0
else
  echo "cross-compile: FAILED"
  exit 1
fi
