#!/bin/bash
set -e
echo "=== Cross-Compilation Verification ==="

FAIL=0

for pair in "linux/amd64" "windows/amd64" "darwin/arm64"; do
  GOOS="${pair%%/*}"
  GOARCH="${pair##*/}"
  # Each build lands in scratch/, never the repo root: a build that fails partway
  # through this loop would otherwise leave a binary in the working tree.
  mkdir -p scratch
  out="scratch/harbor-panic-$GOOS-$GOARCH"
  [ "$GOOS" = "windows" ] && out="$out.exe"
  if GOOS="$GOOS" GOARCH="$GOARCH" go build -tags example -o "$out" ./examples/harbor-panic 2>/dev/null; then
    echo "$GOOS/$GOARCH OK"
  else
    echo "$GOOS/$GOARCH FAIL"
    FAIL=1
  fi
  rm -f "$out"
done

if [[ "$FAIL" -eq 0 ]]; then
  echo "cross-compile: all targets OK"
  exit 0
else
  echo "cross-compile: FAILED"
  exit 1
fi
