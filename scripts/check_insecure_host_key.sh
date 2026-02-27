#!/usr/bin/env bash
# CI lint gate: ensures InsecureIgnoreHostKey is never used in non-test Go files.
# ssh.InsecureIgnoreHostKey() silently accepts all host keys, defeating TOFU security.
# Only test files (_test.go) are allowed to use it.
set -euo pipefail

matches=$(grep -rn "InsecureIgnoreHostKey" --include="*.go" . | grep -v "_test.go" || true)
if [ -n "$matches" ]; then
    echo "ERROR: InsecureIgnoreHostKey found in non-test files:"
    echo "$matches"
    echo ""
    echo "Use newTOFUHostKeyCallback() instead of ssh.InsecureIgnoreHostKey()"
    exit 1
fi
echo "OK: No InsecureIgnoreHostKey in non-test files"
exit 0
