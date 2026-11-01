#!/bin/bash
set +o histexpand

# Validate that all expression placeholders have been properly substituted
# This script checks that the prompt file doesn't contain any unreplaced placeholders

set -e

PROMPT_FILE="${GH_AW_PROMPT:-/tmp/gh-aw/aw-prompts/prompt.txt}"

if [ ! -f "$PROMPT_FILE" ]; then
    echo "❌ Error: Prompt file not found at $PROMPT_FILE"
    exit 1
fi

echo "🔍 Validating prompt placeholders..."

# Check for unreplaced environment variable placeholders (format: __GH_AW_*__)
# Strip inline code spans (`...`) before checking, since backtick-quoted occurrences
# are documentation/code examples (e.g., in PR descriptions) and not actual placeholders.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LITERAL_HELPER="$SCRIPT_DIR/prompt_literal_values.cjs"
if [ ! -f "$LITERAL_HELPER" ]; then
    LITERAL_HELPER="$SCRIPT_DIR/../js/prompt_literal_values.cjs"
fi
AUTHOR_PROMPT=$(node "$LITERAL_HELPER" "$PROMPT_FILE")
STRIPPED_PROMPT=$(printf '%s\n' "$AUTHOR_PROMPT" | sed 's/`[^`]*`//g')
# Only compiler-declared late bindings may remain until the agent job.
read -r -a DEFERRED_PLACEHOLDERS <<< "${GH_AW_DEFERRED_PLACEHOLDERS:-}"
for placeholder in "${DEFERRED_PLACEHOLDERS[@]}"; do
    STRIPPED_PROMPT=${STRIPPED_PROMPT//"$placeholder"/}
done
if printf '%s\n' "$STRIPPED_PROMPT" | grep -q "__GH_AW_"; then
    echo "❌ Error: Found unreplaced placeholders in prompt file:"
    echo ""
    printf '%s\n' "$AUTHOR_PROMPT" | grep -n "__GH_AW_" | grep -v '`[^`]*__GH_AW_' | head -20
    echo ""
    echo "These placeholders should have been replaced with their actual values."
    echo "This indicates a problem with the placeholder substitution step."
    exit 1
fi

# Check for unreplaced GitHub expression syntax (format: ${{ ... }})
# Note: We allow ${{ }} in certain contexts like handlebars templates, 
# but not in the actual prompt content that should have been substituted
if printf '%s\n' "$AUTHOR_PROMPT" | grep -q '\${{[^}]*}}'; then
    # Count occurrences
    COUNT=$(printf '%s\n' "$AUTHOR_PROMPT" | grep -o '\${{[^}]*}}' | wc -l)
    
    # Show a sample of the problematic expressions
    echo "⚠️  Warning: Found $COUNT potential unreplaced GitHub expressions in prompt:"
    echo ""
    printf '%s\n' "$AUTHOR_PROMPT" | grep -n '\${{[^}]*}}' | head -10
    echo ""
    echo "Note: Some expressions may be intentional (e.g., in handlebars templates)."
    echo "Please verify these are expected."
fi

# Count total lines and characters for informational purposes
LINE_COUNT=$(wc -l < "$PROMPT_FILE")
CHAR_COUNT=$(wc -c < "$PROMPT_FILE")
WORD_COUNT=$(wc -w < "$PROMPT_FILE")

echo "✅ Placeholder validation complete"
echo "📊 Prompt statistics:"
echo "   - Lines: $LINE_COUNT"
echo "   - Characters: $CHAR_COUNT"
echo "   - Words: $WORD_COUNT"
