#!/usr/bin/env bash
# Download Docker images with retry logic and controlled parallelism
# Usage: download_docker_images.sh IMAGE1 [IMAGE2 ...]
#
# This script downloads multiple Docker images in parallel with controlled
# parallelism (max 4 concurrent downloads) to improve performance without
# overwhelming the system. Docker daemon supports concurrent pulls, which can
# provide significant speedup when downloading multiple images.
#
# Each image is pulled with retry logic (3 attempts with exponential backoff).
# The script fails if any image fails to download after all retry attempts.

set -euo pipefail

# Helper function to pull Docker images with retry logic
docker_pull_with_retry() {
  local image="$1"
  local max_attempts=3
  local attempt=1
  local wait_time=5
  
  while [ $attempt -le $max_attempts ]; do
    echo "Attempt $attempt of $max_attempts: Pulling $image..."
    timeout 5m docker pull --quiet "$image" 2>&1
    local exit_code=$?
    
    if [ $exit_code -eq 0 ]; then
      echo "Successfully pulled $image"
      return 0
    fi
    
    # Check if the command timed out (exit code 124 from timeout command)
    if [ $exit_code -eq 124 ]; then
      echo "docker pull timed out for $image"
      return 1
    fi
    
    if [ $attempt -lt $max_attempts ]; then
      echo "Failed to pull $image. Retrying in ${wait_time}s..."
      sleep $wait_time
      wait_time=$((wait_time * 2))  # Exponential backoff
    else
      echo "Failed to pull $image after $max_attempts attempts"
      return 1
    fi
    attempt=$((attempt + 1))
  done
}

# Export function so xargs can use it
export -f docker_pull_with_retry

# Pull images with controlled parallelism using xargs
echo "Starting download of ${#@} image(s) with max 4 concurrent downloads..."
printf '%s\n' "$@" | xargs -P 4 -I {} bash -c 'docker_pull_with_retry "$@"' _ {}

echo "All images downloaded successfully"
