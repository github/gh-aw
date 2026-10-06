#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
export DOCKER_LOG="$WORKDIR/docker.log"
export MOCK_DIGESTS="$WORKDIR/digests"

cat > "$WORKDIR/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
  image)
    [[ "$2" == inspect ]] || exit 1
    [[ "${MOCK_INSPECT_FAIL:-}" != 124 ]] || exit 124
    [[ "${MOCK_INSPECT_FAIL:-}" != 1 ]] || exit 1
    cat "$MOCK_DIGESTS"
    ;;
  tag)
    [[ "${MOCK_TAG_FAIL:-}" != 1 ]]
    ;;
  pull)
    [[ "${MOCK_PULL_FAIL:-}" != 1 ]]
    ;;
  *) exit 1 ;;
esac
MOCK
chmod +x "$WORKDIR/docker"
export PATH="$WORKDIR:$PATH"

a="$(printf 'a%.0s' {1..64})"
b="$(printf 'b%.0s' {1..64})"
first="registry.example.com:5000/team/app:v1@sha256:$a"
second="registry.example.com:5000/team/other:v2@sha256:$b"
printf 'registry.example.com:5000/team/app@sha256:%s\nregistry.example.com:5000/team/other@sha256:%s\n' "$a" "$b" > "$MOCK_DIGESTS"

run_ok() {
  : > "$DOCKER_LOG"
  GH_AW_DOCKER_IMAGE_PULL_POLICY=never bash "$SCRIPT_DIR/download_docker_images.sh" "$@" > "$WORKDIR/output" 2>&1 || {
    cat "$WORKDIR/output"
    exit 1
  }
  if grep -q '^pull ' "$DOCKER_LOG"; then
    echo "Unexpected pull in local-only mode" >&2
    exit 1
  fi
}

run_fail() {
  : > "$DOCKER_LOG"
  if GH_AW_DOCKER_IMAGE_PULL_POLICY=never bash "$SCRIPT_DIR/download_docker_images.sh" "$@" > "$WORKDIR/output" 2>&1; then
    echo "Expected local-only validation failure: $*" >&2
    exit 1
  fi
  ! grep -Eq '^(pull|tag) ' "$DOCKER_LOG"
}

run_ok "$first" "$second"
grep -Fxq "image inspect --format {{range .RepoDigests}}{{println .}}{{end}} $first" "$DOCKER_LOG"
grep -Fxq "tag $first registry.example.com:5000/team/app:v1" "$DOCKER_LOG"
grep -Fxq "tag $second registry.example.com:5000/team/other:v2" "$DOCKER_LOG"
[[ "$(grep -c '^image ' "$DOCKER_LOG")" == 2 ]]
[[ "$(grep -c '^tag ' "$DOCKER_LOG")" == 2 ]]
[[ "$(sed -n '2p' "$DOCKER_LOG")" == image\ inspect* ]]
[[ "$(tail -1 "$DOCKER_LOG")" == "tag $second registry.example.com:5000/team/other:v2" ]]
! grep -q 'app:latest' "$DOCKER_LOG" || exit 1

run_fail
run_fail alpine:latest
run_fail "registry.example.com/team/app:v1"
run_fail "registry.example.com/team/app:v1@sha256:${a^^}"
run_fail $'registry.example.com/team/app:v1\nbad@sha256:'"$a"
run_fail "$first" "registry.example.com:5000/team/app:v1@sha256:$b"
printf 'registry.example.com:5000/team/app@sha256:%s\n' "$b" > "$MOCK_DIGESTS"
run_fail "$first"
printf '%s\n' "registry.example.com:5000/team/app:v1@sha256:$a" > "$MOCK_DIGESTS"
run_fail "$first"
printf 'registry.example.com:5000/team/app@sha256:%s\n' "$a" > "$MOCK_DIGESTS"
MOCK_INSPECT_FAIL=1 run_fail "$first"
MOCK_INSPECT_FAIL=124 run_fail "$first"
printf 'registry.example.com:5000/team/app@sha256:%s\n' "$a" > "$MOCK_DIGESTS"
run_fail "$first" "$second"
MOCK_TAG_FAIL=1 GH_AW_DOCKER_IMAGE_PULL_POLICY=never bash "$SCRIPT_DIR/download_docker_images.sh" "$first" > "$WORKDIR/output" 2>&1 && exit 1
if grep -q '^pull ' "$DOCKER_LOG"; then
  echo "Unexpected pull after local-only tagging failure" >&2
  exit 1
fi

: > "$DOCKER_LOG"
GH_AW_DOCKER_IMAGE_PULL_POLICY=always bash "$SCRIPT_DIR/download_docker_images.sh" "$first" > "$WORKDIR/output" 2>&1
grep -q '^pull --quiet ' "$DOCKER_LOG"
: > "$DOCKER_LOG"
env -u GH_AW_DOCKER_IMAGE_PULL_POLICY bash "$SCRIPT_DIR/download_docker_images.sh" "$first" > "$WORKDIR/output" 2>&1
grep -q '^pull --quiet ' "$DOCKER_LOG"
for policy in '' sometimes; do
  : > "$DOCKER_LOG"
  if GH_AW_DOCKER_IMAGE_PULL_POLICY="$policy" bash "$SCRIPT_DIR/download_docker_images.sh" "$first" > "$WORKDIR/output" 2>&1; then
    echo "Expected invalid policy failure" >&2
    exit 1
  fi
  [[ ! -s "$DOCKER_LOG" ]]
done
echo "Local-only Docker image tests passed"
