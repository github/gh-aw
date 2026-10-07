#!/usr/bin/env bash
set +o histexpand

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
#
# When images include a digest pin (e.g. image:tag@sha256:abc), the script
# ensures the tag alias (image:tag) is created after pulling so that tools
# referencing images by tag (such as AWF with --skip-pull) can find them.
#
# The script also aliases the image as "image:latest" so that any downstream
# tool that references the image via the mutable ":latest" tag (regardless of
# which versioned tag was actually pulled) can still resolve it locally under
# --pull-never/--skip-pull semantics. This guards against tag mismatches
# between the version-pinned tag written here and a ":latest" reference used
# elsewhere (see gh-aw#50681).

set -euo pipefail

case "${GH_AW_DOCKER_IMAGE_PULL_POLICY-always}" in
  always) ;;
  never)
    if (( $# == 0 )); then
      echo "Local-only image validation requires at least one image" >&2
      exit 1
    fi

    declare -A alias_digests=()
    declare -a aliases=()
    declare -a expected_digests=()
    for image in "$@"; do
      if [[ ! "$image" =~ ^([a-z0-9][a-z0-9.-]*(:[0-9]+)?/([a-z0-9]+([._-][a-z0-9]+)*/)*[a-z0-9]+([._-][a-z0-9]+)*)(:([A-Za-z0-9_][A-Za-z0-9_.-]*))?@sha256:([a-f0-9]{64})$ ]]; then
        echo "Local-only mode requires a fully qualified repository[:tag]@sha256:<64 lowercase hex> reference: $image" >&2
        exit 1
      fi
      repository="${BASH_REMATCH[1]}"
      tag="${BASH_REMATCH[7]}"
      if [[ -z "$tag" ]]; then
        echo "Local-only mode requires an explicit tag before the digest: $image" >&2
        exit 1
      fi
      digest="${BASH_REMATCH[8]}"
      registry="${repository%%/*}"
      if [[ "$registry" != *.* && "$registry" != *:* && "$registry" != localhost ]]; then
        echo "Local-only mode requires a registry-qualified image: $image" >&2
        exit 1
      fi
      alias="${repository}:${tag}"
      if [[ -v alias_digests["$alias"] && "${alias_digests[$alias]}" != "$digest" ]]; then
        echo "Conflicting digests requested for alias $alias" >&2
        exit 1
      fi
      alias_digests["$alias"]="$digest"
      aliases+=("$alias")
      expected_digests+=("${repository}@sha256:${digest}")
    done

    # Validate every exact repository digest before modifying any local tag.
    index=0
    for image in "$@"; do
      expected="${expected_digests[$index]}"
      if ! digests=$(timeout 30s docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$image"); then
        echo "Local-only image inspection failed: $image" >&2
        exit 1
      fi
      found=false
      while IFS= read -r entry; do
        if [[ "$entry" == "$expected" ]]; then
          found=true
          break
        fi
      done <<< "$digests"
      if [[ "$found" != true ]]; then
        echo "Local-only image is missing exact RepoDigest $expected" >&2
        exit 1
      fi
      index=$((index + 1))
    done

    index=0
    for image in "$@"; do
      if ! timeout 30s docker tag "$image" "${aliases[$index]}"; then
        echo "Local-only image tagging failed: $image" >&2
        exit 1
      fi
      index=$((index + 1))
    done
    echo "All local images validated and tagged successfully"
    exit 0
    ;;
  *)
    echo "Invalid GH_AW_DOCKER_IMAGE_PULL_POLICY (expected always or never)" >&2
    exit 1
    ;;
esac

# Helper function to pull Docker images with retry logic
docker_pull_with_retry() {
  local image="$1"
  local max_attempts=3
  local wait_time=5
  
  for attempt in $(seq 1 $max_attempts); do
    echo "Attempt $attempt of $max_attempts: Pulling $image..."
    
    if timeout 5m docker pull --quiet "$image" 2>&1; then
      echo "Successfully pulled $image"

      # When pulling with a digest pin, Docker may not create a digest-free
      # alias automatically. Preserve the original base reference and add back
      # its implicit ":latest" or explicit tag alias so downstream tools can
      # resolve the image locally under --pull-never/--skip-pull semantics.
      local tag_ref="$image"
      local base_ref="$image"
      if [[ "$image" == *"@sha256:"* ]]; then
        base_ref="${image%%@sha256:*}"
        tag_ref="$base_ref"
        if [[ "$base_ref" == *":"* ]]; then
          echo "Tagging digest-pinned image as $base_ref"
          docker tag "$image" "$base_ref"
        else
          local latest_ref="${base_ref}:latest"
          echo "Tagging digest-pinned image as $latest_ref"
          docker tag "$image" "$latest_ref"
          tag_ref="$latest_ref"
        fi
      fi

      # Only AWF images need a mutable ":latest" alias for local compose
      # stacks, and only when the requested reference was version-pinned. This
      # avoids races when unrelated repositories are pulled concurrently with
      # multiple distinct tags.
      if [[ "$base_ref" == ghcr.io/github/gh-aw-* && "$tag_ref" == *":"* ]]; then
        local repo_ref="${tag_ref%%:*}"
        local tag_part="${tag_ref##*:}"
        if [[ "$tag_part" != "latest" ]]; then
          local latest_ref="${repo_ref}:latest"
          echo "Aliasing $tag_ref as $latest_ref"
          docker tag "$tag_ref" "$latest_ref"
        fi
      fi

      return 0
    fi
    
    local exit_code=$?
    
    # Timeout produces exit code 124
    if [ $exit_code -eq 124 ]; then
      echo "docker pull timed out for $image after 5 minutes"
      return 1
    fi
    
    # Retry with exponential backoff unless this was the last attempt
    if [ "$attempt" -lt "$max_attempts" ]; then
      echo "Failed to pull $image. Retrying in ${wait_time}s..."
      sleep $wait_time
      wait_time=$((wait_time * 2))
    else
      echo "Failed to pull $image after $max_attempts attempts"
      return 1
    fi
  done
}

# Export function so xargs can use it
export -f docker_pull_with_retry

# Pull images with controlled parallelism using xargs
echo "Starting download of ${#@} image(s) with max 4 concurrent downloads..."
printf '%s\n' "$@" | xargs -P 4 -I {} bash -c 'docker_pull_with_retry "$@"' _ {}

echo "All images downloaded successfully"
