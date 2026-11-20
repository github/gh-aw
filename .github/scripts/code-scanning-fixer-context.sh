#!/usr/bin/env bash
set -euo pipefail

AGENT_DIR=$1
CACHE_FILE=$2
mkdir -p "$AGENT_DIR"
CACHE_RECORDS="$AGENT_DIR/cache-records.json"
CACHE_DATA="$AGENT_DIR/cache-data.json"
ALERTS_FILE="$AGENT_DIR/open-alerts.json"
PULLS_FILE="$AGENT_DIR/open-fix-pulls.json"
PULL_FILES="$AGENT_DIR/pull-files.json"
PULL_RECORDS="$AGENT_DIR/pull-records.jsonl"
SELECTION_FILE="$AGENT_DIR/alert-selection.json"

fetch_error() {
  printf '%s\n' "$1" >&2
  jq -n --arg reason "$1" '{status: "fetch_error", reason: $reason}' > "$SELECTION_FILE"
}

if [ -f "$CACHE_FILE" ]; then
  jq -Rn '
    def valid_record:
      type == "object"
      and (((.alert_number | type) == "number") or ((.alert_number | type) == "string"));
    [inputs | try fromjson catch null] as $records
    | [$records[] | select(valid_record)] as $valid
    | {records: $valid, invalid_count: (($records | length) - ($valid | length))}
  ' "$CACHE_FILE" > "$CACHE_DATA"
  jq '.records' "$CACHE_DATA" > "$CACHE_RECORDS"
  INVALID_CACHE_RECORDS=$(jq -r '.invalid_count' "$CACHE_DATA")
  if [ "$INVALID_CACHE_RECORDS" -gt 0 ]; then
    printf 'Ignored %s invalid cached alert record(s).\n' "$INVALID_CACHE_RECORDS" >&2
  fi
else
  printf '[]\n' > "$CACHE_RECORDS"
fi

if ! gh api --paginate "repos/${GITHUB_REPOSITORY}/code-scanning/alerts?state=open&per_page=100" | jq -s 'add // []' > "$ALERTS_FILE"; then
  fetch_error "Could not fetch code scanning alerts."
  exit 0
fi
if ! gh api --paginate "repos/${GITHUB_REPOSITORY}/pulls?state=open&per_page=100" | jq -s 'add // []' > "$PULLS_FILE"; then
  fetch_error "Could not fetch open pull requests for deduplication."
  exit 0
fi

: > "$PULL_RECORDS"
while IFS= read -r PR_NUMBER; do
  if ! gh api --paginate "repos/${GITHUB_REPOSITORY}/pulls/${PR_NUMBER}/files?per_page=100" | jq -se '
    add // []
    | if length >= 3000 then error("GitHub pull request file listing may be truncated at 3000 files")
      elif all(.[]; (.filename | type) == "string" and (.filename | length) > 0) then
        [.[] | .filename, (.previous_filename // empty)] | unique
      else error("Invalid pull request file listing")
      end
  ' > "$PULL_FILES"; then
    fetch_error "Could not completely inspect changed files for pull request #${PR_NUMBER}; no remediation is safe."
    exit 0
  fi
  jq -c --argjson number "$PR_NUMBER" --slurpfile files "$PULL_FILES" '
    .[] | select(.number == $number) | . + {files: $files[0]}
  ' "$PULLS_FILE" >> "$PULL_RECORDS"
done < <(jq -r '.[].number' "$PULLS_FILE")
jq -s '.' "$PULL_RECORDS" > "$PULLS_FILE"

NOW=$(date -u +%s)
jq -n --argjson now "$NOW" --arg repo "$GITHUB_REPOSITORY" \
  --slurpfile cached "$CACHE_RECORDS" --slurpfile alerts "$ALERTS_FILE" \
  --slurpfile pulls "$PULLS_FILE" \
  -f .github/scripts/code-scanning-fixer-select.jq > "$SELECTION_FILE"
