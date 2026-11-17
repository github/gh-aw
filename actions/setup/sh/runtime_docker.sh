#!/usr/bin/env bash
set +o histexpand

gh_aw_container_name() {
  if [[ ! "${GH_AW_RUNTIME_ID:-}" =~ ^gh-aw-job-[a-zA-Z0-9]+$ ]]; then
    echo "::error::Setup must allocate a Docker-safe runtime ID first" >&2
    return 1
  fi
  printf '%s-%s\n' "$1" "$GH_AW_RUNTIME_ID"
}

# Inspect by exact name, then remove by immutable ID to avoid name-reuse races.
gh_aw_owned_container_id() {
  local name="$1" inspected
  inspected="$(docker inspect --type container --format '{{json .}}' "$name" 2>/dev/null)" || return 1
  GH_AW_INSPECTED_CONTAINER="$inspected" GH_AW_EXPECTED_CONTAINER="$name" node <<'NODE'
const container = JSON.parse(process.env.GH_AW_INSPECTED_CONTAINER);
if (container.Name !== `/${process.env.GH_AW_EXPECTED_CONTAINER}` ||
    container.Config?.Labels?.["gh-aw.runtime-id"] !== process.env.GH_AW_RUNTIME_ID ||
    !/^[a-f0-9]{64}$/.test(container.Id)) {
  console.error("::error::Refusing to stop a container without matching invocation ownership");
  process.exit(1);
}
process.stdout.write(container.Id);
NODE
}

gh_aw_remove_container() {
  local name="$1" container_id
  container_id="$(gh_aw_owned_container_id "$name")" || return 1
  docker rm -f "$container_id"
}
