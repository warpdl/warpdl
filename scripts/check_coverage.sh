#!/usr/bin/env bash
set -uo pipefail

min_total=${COVERAGE_MIN:-80}
min_pkg=${COVERAGE_MIN_PER_PKG:-80}

profile=$(mktemp)
output=$(mktemp)
trap 'rm -f "$profile" "$output"' EXIT

# Run tests and capture exit code
# On Windows, Go may fail to clean up build cache (known issue)
# so we check for actual test failures in the output instead
go test ./... -coverprofile="$profile" -count=1 2>&1 | tee "$output"
test_exit=${PIPESTATUS[0]}

# Check if any tests actually failed (not just cleanup errors)
if grep -q "^FAIL\s" "$output" || grep -q "^\-\-\- FAIL:" "$output"; then
  echo "Tests failed"
  exit 1
fi

fail=0
awk -v min="$min_pkg" '
/\[no test files\]/ {
  pkg = ($1 == "ok" || $1 == "?") ? $2 : $1
  print "missing tests:", pkg
  fail=1
}
/coverage:/ {
  if ($0 ~ /no statements/) next
  # Determine package name: $2 for lines starting with "ok", else $1
  pkg = ($1 == "ok") ? $2 : $1
  line = $0
  sub(/.*coverage: /, "", line)
  sub(/%.*$/, "", line)
  cov = line + 0
  if (cov < min) {
    print "package coverage below threshold:", pkg, cov"%"
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
