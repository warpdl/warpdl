#!/usr/bin/env bash
set -euo pipefail

min_total=${COVERAGE_MIN:-80}
min_pkg=${COVERAGE_MIN_PER_PKG:-80}

profile=$(mktemp)
output=$(mktemp)
trap 'rm -f "$profile" "$output"' EXIT

go test ./... -coverprofile="$profile" -count=1 | tee "$output"

fail=0
awk -v min="$min_pkg" '
/\[no test files\]/ {print "missing tests:", $1; fail=1}
/coverage:/ {
  if ($0 ~ /no statements/) next
  line = $0
  sub(/.*coverage: /, "", line)
  sub(/%.*$/, "", line)
  cov = line + 0
  if (cov < min) {
    print "package coverage below threshold:", $1, cov"%"
    fail=1
  }
}
END { exit fail }
' "$output" || fail=1

total=$(go tool cover -func="$profile" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')
if awk -v total="$total" -v min="$min_total" 'BEGIN {exit !(total+0 < min)}'; then
  echo "total coverage below threshold: ${total}%"
  fail=1
fi

exit $fail
