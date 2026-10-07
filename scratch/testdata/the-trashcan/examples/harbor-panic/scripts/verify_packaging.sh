#!/bin/bash
set -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

echo "=== Packaging Verification ==="

# Build the harbor binary into scratch/
mkdir -p scratch
go build -tags example -o scratch/harbor-panic ./examples/harbor-panic

# Check 1: binary size >5M
SIZE=$(stat -f%z scratch/harbor-panic 2>/dev/null || stat -c%s scratch/harbor-panic 2>/dev/null || echo 0)
if [[ "$SIZE" -gt 5242880 ]]; then
  echo "binary size >5M OK (bytes=$SIZE)"
else
  echo "FAIL: scratch/harbor-panic size $SIZE <= 5M"
  exit 1
fi

# Check 2: no binary files in git diff
GIT_BINARIES=$(git diff --stat HEAD 2>/dev/null | grep -c 'Bin ' || true)
if [[ "$GIT_BINARIES" -eq 0 ]]; then
  echo "no binary files in git diff OK"
else
  echo "FAIL: $GIT_BINARIES binary files found in git diff"
  exit 1
fi

# Check 3: no stray harbor-panic binary FILES outside scratch/
STRAY=$(find . -maxdepth 2 -type f -name harbor-panic -not -path './scratch/*' -not -path './.git/*' 2>/dev/null | wc -l | tr -d ' ')
if [[ "$STRAY" -eq 0 ]]; then
  echo "no stray binaries outside scratch OK"
else
  echo "FAIL: $STRAY stray harbor-panic binaries found outside scratch/"
  find . -maxdepth 2 -type f -name harbor-panic -not -path './scratch/*' -not -path './.git/*'
  exit 1
fi

# Check 4: python pathlib stray check (files only, skip directories)
python3 -c "
import pathlib, sys
root = pathlib.Path('.')
stray = [p for p in root.glob('harbor-panic') if p.is_file() and 'scratch' not in str(p)]
stray += [p for p in root.glob('*/harbor-panic') if p.is_file() and 'scratch' not in str(p) and '.git' not in str(p)]
if stray:
    print(f'FAIL: stray binaries: {stray}', file=sys.stderr)
    sys.exit(1)
"

# Check 5: GOOS=linux cross-build succeeds
GOOS=linux GOARCH=amd64 go build -tags example -o /dev/null ./examples/harbor-panic
echo "GOOS=linux build OK"

# Check 6: gofmt clean
UNFMT=$(gofmt -l examples/harbor-panic/ 2>/dev/null | wc -l | tr -d ' ')
if [[ "$UNFMT" -gt 0 ]]; then
  echo "FAIL: gofmt found $UNFMT unformatted files"
  gofmt -l examples/harbor-panic/
  exit 1
fi
echo "gofmt clean OK"

# Clean up the built binary (it's scratch-only, not committed)
rm -f scratch/harbor-panic

echo "=== All Packaging checks PASSED ==="
exit 0
