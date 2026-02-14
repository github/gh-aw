#!/bin/bash
# Local test script for copilot-client
# This simulates what the CI workflow does

set -e

echo "=== Testing copilot-client locally ==="

# Check if copilot CLI is available
if ! command -v copilot &> /dev/null; then
    echo "Error: copilot CLI not found. Install with: npm install -g @github/copilot"
    exit 1
fi

# Check if COPILOT_GITHUB_TOKEN is set
if [ -z "$COPILOT_GITHUB_TOKEN" ]; then
    echo "Warning: COPILOT_GITHUB_TOKEN not set. The test may fail."
    echo "Set it with: export COPILOT_GITHUB_TOKEN=ghp_..."
fi

# Create test directory
mkdir -p /tmp/copilot-client-test
mkdir -p /tmp/copilot-logs

# Create test prompt
echo "What is 2+2? Answer briefly in one sentence." > /tmp/copilot-client-test/prompt.txt

# Build the client
echo "Building copilot-client..."
npm run build

# Create configuration
cat > /tmp/copilot-client-test/config.json << 'EOFCONFIG'
{
  "promptFile": "/tmp/copilot-client-test/prompt.txt",
  "eventLogFile": "/tmp/copilot-client-test/events.jsonl",
  "githubToken": "${COPILOT_GITHUB_TOKEN}",
  "logLevel": "info",
  "session": {
    "model": "gpt-5"
  }
}
EOFCONFIG

# Replace token placeholder
if [ -n "$COPILOT_GITHUB_TOKEN" ]; then
    sed -i "s/\${COPILOT_GITHUB_TOKEN}/$COPILOT_GITHUB_TOKEN/g" /tmp/copilot-client-test/config.json
fi

echo "Configuration:"
cat /tmp/copilot-client-test/config.json

# Run the client with debug logging
echo ""
echo "Running copilot-client..."
DEBUG=copilot-client cat /tmp/copilot-client-test/config.json | node -e "import('./dist/index.js').then(m => m.main())"

# Check results
echo ""
echo "=== Test Results ==="
if [ -f /tmp/copilot-client-test/events.jsonl ]; then
    EVENT_COUNT=$(wc -l < /tmp/copilot-client-test/events.jsonl)
    echo "✓ Event log created with $EVENT_COUNT events"
    echo ""
    echo "Events:"
    cat /tmp/copilot-client-test/events.jsonl | jq -r '.type' 2>/dev/null || cat /tmp/copilot-client-test/events.jsonl
else
    echo "✗ Event log not created"
    exit 1
fi

echo ""
echo "=== Test completed successfully ==="
