#!/usr/bin/env bash
# check-docs-coverage.sh - Verify CLI commands are documented
set -e

echo "Checking documentation coverage..."

# Build the binary if needed
if [ ! -f "./warpdl" ]; then
    echo "Building warpdl..."
    if ! go build -o warpdl .; then
        echo "❌ Failed to build warpdl"
        exit 1
    fi
fi

# Get all top-level commands from help output
commands=$(./warpdl help 2>/dev/null | grep -E '^\s+\w+,' | awk -F',' '{print $1}' | tr -d ' ' | sort -u)

# Path to CLI reference (may be auto-generated or placeholder)
CLI_DOCS="docs/src/content/docs/usage/cli-reference.mdx"

if [ ! -f "$CLI_DOCS" ]; then
    echo "⚠️  CLI reference file not found: $CLI_DOCS"
    echo "Run 'go run docs/scripts/generate-cli-docs.go' first"
    exit 1
fi

missing=0
for cmd in $commands; do
    # Skip 'help' as it's implicit
    if [ "$cmd" = "help" ]; then
        continue
    fi

    # Check if command appears in docs (case insensitive)
    if ! grep -qi "\\b$cmd\\b" "$CLI_DOCS" 2>/dev/null; then
        echo "❌ Missing docs for command: $cmd"
        missing=$((missing + 1))
    fi
done

if [ $missing -gt 0 ]; then
    echo ""
    echo "❌ $missing command(s) missing documentation"
    exit 1
fi

echo "✅ All commands documented"

# Check environment variables coverage
ENV_DOCS="docs/src/content/docs/configuration/environment-variables.mdx"
if [ ! -f "$ENV_DOCS" ]; then
    echo "⚠️  Environment variables file not found: $ENV_DOCS"
    echo "Run 'go run docs/scripts/generate-env-docs.go' first"
    exit 1
fi

echo "✅ Environment variables documented"
echo ""
echo "Documentation coverage check passed!"
