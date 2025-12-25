#!/usr/bin/env bash
set -euo pipefail

# go_test_retry.sh
# ----------------
# Wraps `go test` with Windows-specific retry logic for the flaky
# `cmd.test.exe` deletion race. Non-Windows platforms simply execute
# `go test` once. Configure attempts/delay via:
#   GO_TEST_RETRY_ATTEMPTS (default: 3)
#   GO_TEST_RETRY_SLEEP   (default: 2 seconds)

attempts=${GO_TEST_RETRY_ATTEMPTS:-3}
sleep_secs=${GO_TEST_RETRY_SLEEP:-2}

detect_windows() {
  local os_name=${RUNNER_OS:-$(uname -s)}
  case "$os_name" in
    Windows|windows|MINGW*|MSYS*|CYGWIN*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_cache_lock_error() {
  local logfile=$1
  grep -qiE 'cmd\.test\.exe.*(access is denied|used by another process)' "$logfile"
}

if ! detect_windows; then
  exec go test "$@"
fi

if (( attempts < 1 )); then
  attempts=1
fi

for (( attempt = 1; attempt <= attempts; attempt++ )); do
  logfile=$(mktemp)
  if go test "$@" 2>&1 | tee "$logfile"; then
    rm -f "$logfile"
    exit 0
  fi
  status=${PIPESTATUS[0]}

  if (( attempt == attempts )) || ! is_cache_lock_error "$logfile"; then
    rm -f "$logfile"
    exit $status
  fi

  echo "Detected cmd.test.exe lock issue (attempt ${attempt}/${attempts}); retrying in ${sleep_secs}s..." >&2
  rm -f "$logfile"
  sleep "$sleep_secs"
done

