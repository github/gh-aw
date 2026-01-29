#!/usr/bin/env bash
# Install dependencies in parallel to reduce sequential execution time
# Usage: install_parallel_setup.sh [--awf VERSION] [--copilot VERSION] [--claude VERSION] [--docker IMAGE1 IMAGE2 ...]
#
# This script parallelizes independent setup operations:
# - AWF binary installation (if --awf is specified)
# - Copilot CLI installation (if --copilot is specified)  
# - Claude Code CLI installation (if --claude is specified)
# - Docker image downloads (if --docker is specified)
#
# All operations run in parallel using background jobs, with proper error handling
# that preserves exit codes from failed jobs.

set -euo pipefail

# Parse arguments
AWF_VERSION=""
COPILOT_VERSION=""
CLAUDE_VERSION=""
DOCKER_IMAGES=()

while [[ $# -gt 0 ]]; do
  case $1 in
    --awf)
      AWF_VERSION="$2"
      shift 2
      ;;
    --copilot)
      COPILOT_VERSION="$2"
      shift 2
      ;;
    --claude)
      CLAUDE_VERSION="$2"
      shift 2
      ;;
    --docker)
      shift
      # Collect all remaining args as docker images
      while [[ $# -gt 0 ]] && [[ ! $1 =~ ^-- ]]; do
        DOCKER_IMAGES+=("$1")
        shift
      done
      ;;
    *)
      echo "ERROR: Unknown option: $1"
      echo "Usage: $0 [--awf VERSION] [--copilot VERSION] [--claude VERSION] [--docker IMAGE1 IMAGE2 ...]"
      exit 1
      ;;
  esac
done

# Track background job PIDs
PIDS=()
JOB_NAMES=()

# Error handling: collect exit codes from background jobs
EXIT_CODES=()

echo "Starting parallel setup operations..."

# Start AWF installation in background if requested
if [ -n "$AWF_VERSION" ]; then
  echo "Starting AWF binary installation (version: $AWF_VERSION)..."
  {
    bash /opt/gh-aw/actions/install_awf_binary.sh "$AWF_VERSION"
    exit $?
  } &
  PIDS+=($!)
  JOB_NAMES+=("AWF binary")
fi

# Start Copilot CLI installation in background if requested
if [ -n "$COPILOT_VERSION" ]; then
  echo "Starting Copilot CLI installation (version: $COPILOT_VERSION)..."
  {
    bash /opt/gh-aw/actions/install_copilot_cli.sh "$COPILOT_VERSION"
    exit $?
  } &
  PIDS+=($!)
  JOB_NAMES+=("Copilot CLI")
fi

# Start Claude Code CLI installation in background if requested
if [ -n "$CLAUDE_VERSION" ]; then
  echo "Starting Claude Code CLI installation (version: $CLAUDE_VERSION)..."
  {
    # Claude is installed via npm, so we use a temporary Node.js setup
    # Note: Node.js should already be set up before this script is called
    npm install -g "@anthropic-ai/claude-code@$CLAUDE_VERSION"
    claude-code --version
    exit $?
  } &
  PIDS+=($!)
  JOB_NAMES+=("Claude Code CLI")
fi

# Start Docker image downloads in background if requested
if [ ${#DOCKER_IMAGES[@]} -gt 0 ]; then
  echo "Starting Docker image downloads (${#DOCKER_IMAGES[@]} images)..."
  {
    bash /opt/gh-aw/actions/download_docker_images.sh "${DOCKER_IMAGES[@]}"
    exit $?
  } &
  PIDS+=($!)
  JOB_NAMES+=("Docker images")
fi

# Wait for all background jobs to complete and collect exit codes
echo "Waiting for ${#PIDS[@]} parallel operations to complete..."

FAILED_JOBS=()
for i in "${!PIDS[@]}"; do
  PID="${PIDS[$i]}"
  JOB_NAME="${JOB_NAMES[$i]}"
  
  # Wait for specific PID and capture its exit code
  if wait "$PID"; then
    echo "✓ ${JOB_NAME} completed successfully"
    EXIT_CODES+=("0")
  else
    EXIT_CODE=$?
    echo "✗ ${JOB_NAME} failed with exit code ${EXIT_CODE}"
    EXIT_CODES+=("${EXIT_CODE}")
    FAILED_JOBS+=("${JOB_NAME}")
  fi
done

# Report results
if [ ${#FAILED_JOBS[@]} -eq 0 ]; then
  echo "✓ All ${#PIDS[@]} parallel setup operations completed successfully"
  exit 0
else
  echo "✗ ${#FAILED_JOBS[@]} of ${#PIDS[@]} operations failed:"
  for JOB in "${FAILED_JOBS[@]}"; do
    echo "  - ${JOB}"
  done
  exit 1
fi
