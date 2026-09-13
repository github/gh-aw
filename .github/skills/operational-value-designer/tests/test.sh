#!/usr/bin/env bash

set -euo pipefail

skill_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
repo_root=$(CDPATH='' cd -- "$skill_dir/../../.." && pwd)
work_dir=$(mktemp -d "$repo_root/.operational-value-designer-test.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

evaluator_path_from_skill=$("$skill_dir/scripts/operational-value-evaluator-path.sh" daily-file-diet)
[[ $evaluator_path_from_skill == .github/graders/daily-file-diet-operational-value.sh ]]
if "$skill_dir/scripts/operational-value-evaluator-path.sh" ../escape >/dev/null 2>&1; then
    printf 'invalid workflow name was accepted\n' >&2
    exit 1
fi

evaluator_path="$work_dir/operational-value.sh"
cat > "$evaluator_path" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

request=$(cat)
printf '%s\n' "$request" | jq -e '
    .schemaVersion == 1
    and .run.id == "1"
    and .run.repository == "owner/repo"
    and .run.eventName == "workflow_dispatch"
    and .event == {}
    and .config.verification == true
' >/dev/null

cat <<'JSON'
[
  {"id":"issue-resolution","value":1},
  {"id":"repository-health","value":null}
]
JSON
EOF
chmod +x "$evaluator_path"

"$skill_dir/scripts/verify-operational-value-evaluator.sh" "$evaluator_path" >/dev/null

invalid_evaluator_path="$work_dir/invalid-operational-value.sh"
cat > "$invalid_evaluator_path" <<'EOF'
#!/usr/bin/env bash
cat >/dev/null
printf '[{"id":"duplicate","value":1},{"id":"duplicate","value":0}]\n'
EOF
chmod +x "$invalid_evaluator_path"
if "$skill_dir/scripts/verify-operational-value-evaluator.sh" "$invalid_evaluator_path" >/dev/null 2>&1; then
    printf 'invalid evaluator output was accepted\n' >&2
    exit 1
fi

daily_evaluator="$repo_root/.github/graders/daily-file-diet-operational-value.sh"
daily_work_dir="$work_dir/daily-file-diet"
daily_output_path="$work_dir/agent_output.json"
mkdir -p "$daily_work_dir/pkg"

run_daily_evaluator() {
    (cd "$daily_work_dir" && printf '%s\n' '{"schemaVersion":1,"run":{"id":"1"},"event":{},"config":{}}' \
        | GH_AW_AGENT_OUTPUT="$daily_output_path" "$daily_evaluator")
}

awk 'BEGIN { for (i = 0; i < 800; i++) print "package pkg" }' > "$daily_work_dir/pkg/large.go"
printf '%s\n' '{"items":[{"type":"create_issue","title":"Refactor pkg/large.go","body":"Split pkg/large.go into focused files."}]}' > "$daily_output_path"
[[ $(run_daily_evaluator) == '[{"id":"large-file-triage-output","value":1}]' ]]

printf '%s\n' '{"items":[]}' > "$daily_output_path"
[[ $(run_daily_evaluator) == '[{"id":"large-file-triage-output","value":0}]' ]]

printf '%s\n' '{"items":[{"type":"create_issue","title":"Refactor another file","body":"Split pkg/other.go."}]}' > "$daily_output_path"
[[ $(run_daily_evaluator) == '[{"id":"large-file-triage-output","value":0}]' ]]

awk 'BEGIN { for (i = 0; i < 799; i++) print "package pkg" }' > "$daily_work_dir/pkg/large.go"
printf '%s\n' '{"items":[{"type":"noop"}]}' > "$daily_output_path"
[[ $(run_daily_evaluator) == '[{"id":"large-file-triage-output","value":1}]' ]]

printf '%s\n' '{"items":[]}' > "$daily_output_path"
[[ $(run_daily_evaluator) == '[{"id":"large-file-triage-output","value":0}]' ]]

rm -f "$daily_output_path"
[[ $(run_daily_evaluator) == '[{"id":"large-file-triage-output","value":null}]' ]]

printf 'operational-value-designer skill tests passed\n'