#!/bin/bash
# usage: test_summary.sh go-test-v.log label
# Counts top-level and sub tests from `go test -v` output.
log=$1; label=$2
[ -f "$log" ] || { echo "$label: no log"; exit 0; }
p=$(grep -cE '^\s*--- PASS' "$log" || true)
s=$(grep -cE '^\s*--- SKIP' "$log" || true)
f=$(grep -cE '^\s*--- FAIL' "$log" || true)
ok=$(grep -cE '^ok\s' "$log" || true)
bad=$(grep -cE '^FAIL\s' "$log" || true)
echo "- $label: $p passed, $s skipped, $f failed (packages: $ok ok, $bad failed)"
grep -E '^\s*--- SKIP' "$log" | sed 's/^/    /' | head -40
grep -E '^\s*--- FAIL|^FAIL' "$log" | sed 's/^/    /' | head -40
exit 0
