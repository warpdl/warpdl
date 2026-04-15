#!/bin/bash

# Run jscpd (JavaScript Copy/Paste Detector) for duplicate code detection
# This script is used locally and in CI to detect code duplication

set -e

# Check if jscpd is installed
if ! command -v jscpd &> /dev/null; then
    echo "jscpd is not installed. Installing..."
    if command -v npm &> /dev/null; then
        npm install -g jscpd
    else
        echo "Error: npm is required to install jscpd"
        exit 1
    fi
fi

run_jscpd() {
    local targets=("$@")
    echo "Running duplicate code detection..."
    jscpd --exitCode 1 "${targets[@]}"
}

# In PR CI, scan only changed Go files against the PR base branch.
# This keeps the check actionable and avoids failing on legacy duplication.
if [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" && -n "${GITHUB_BASE_REF:-}" ]]; then
    git fetch --no-tags --depth=1 origin "${GITHUB_BASE_REF}" >/dev/null 2>&1 || true
    changed_go_files=()
    while IFS= read -r file; do
        [[ -n "$file" ]] && changed_go_files+=("$file")
    done < <(git diff --name-only --diff-filter=ACMR "origin/${GITHUB_BASE_REF}...HEAD" -- '*.go')

    if [[ ${#changed_go_files[@]} -eq 0 ]]; then
        echo "No changed Go files in this PR; skipping duplicate detection."
        exit 0
    fi

    echo "Scanning ${#changed_go_files[@]} changed Go file(s) in this PR..."
    run_jscpd "${changed_go_files[@]}"
    exit 0
fi

# If paths are provided, scan only those paths; otherwise scan the full repo.
if [[ $# -gt 0 ]]; then
    run_jscpd "$@"
else
    run_jscpd .
fi
