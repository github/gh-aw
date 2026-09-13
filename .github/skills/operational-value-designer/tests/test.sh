#!/usr/bin/env bash

set -euo pipefail

skill_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
repo_root=$(CDPATH='' cd -- "$skill_dir/../../.." && pwd)
work_dir=$(mktemp -d "$repo_root/.operational-value-designer-test.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

evaluator_path=$($skill_dir/scripts/operational-value-evaluator-path.sh daily-file-diet)
[[ $evaluator_path == .github/graders/daily-file-diet-operational-value.sh ]]
if "$skill_dir/scripts/operational-value-evaluator-path.sh" ../escape >/dev/null 2>&1; then
    printf 'invalid workflow name was accepted\n' >&2
    exit 1
fi

valid_evaluator="$work_dir/valid.sh"
cat > "$valid_evaluator" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

[[ $# -eq 0 ]]
request=$(cat)
printf '%s\n' "$request" | jq -e '
    .schemaVersion == 1
    and .run.id == "1"
    and .run.attempt == 1
    and .run.repository == "owner/repo"
    and .run.workflow == "Verification workflow"
    and .run.ref == "refs/heads/main"
    and .run.sha == "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    and .run.eventName == "workflow_dispatch"
    and (.event | type == "object")
    and .config.verification == true
' >/dev/null
printf 'verification diagnostic\n' >&2
printf '%s\n' '[{"id":"correct-triage","value":1},{"id":"label-confidence","value":null}]'
EOF
chmod +x "$valid_evaluator"

"$skill_dir/scripts/verify-operational-value-evaluator.sh" "$valid_evaluator" >/dev/null

make_evaluator() {
    local name=$1
    local evaluator_output=$2
    local generated_evaluator="$work_dir/$name.sh"
    cat > "$generated_evaluator" <<EOF
#!/usr/bin/env bash
set -euo pipefail
cat <<'JSON'
$evaluator_output
JSON
EOF
    chmod +x "$generated_evaluator"
    printf '%s\n' "$generated_evaluator"
}

assert_rejected() {
    local name=$1
    local evaluator_output=$2
    local generated_evaluator
    generated_evaluator=$(make_evaluator "$name" "$evaluator_output")
    if "$skill_dir/scripts/verify-operational-value-evaluator.sh" "$generated_evaluator" >/dev/null 2>&1; then
        printf 'invalid evaluator output was accepted: %s\n' "$name" >&2
        exit 1
    fi
}

assert_rejected empty-array '[]'
assert_rejected top-level-object '{"id":"score","value":1}'
assert_rejected duplicate-ids '[{"id":"same","value":1},{"id":"same","value":0}]'
assert_rejected empty-id '[{"id":"   ","value":1}]'
assert_rejected negative-value '[{"id":"score","value":-0.01}]'
assert_rejected out-of-range '[{"id":"score","value":1.01}]'
assert_rejected boolean-value '[{"id":"score","value":true}]'
assert_rejected string-value '[{"id":"score","value":"1"}]'
assert_rejected extra-field '[{"id":"score","value":1,"message":"not allowed"}]'
assert_rejected multiple-documents $'[{"id":"first","value":1}]\n[{"id":"second","value":0}]'
assert_rejected invalid-json 'not json'

failed_evaluator="$work_dir/failed.sh"
cat > "$failed_evaluator" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "$failed_evaluator"
if "$skill_dir/scripts/verify-operational-value-evaluator.sh" "$failed_evaluator" >/dev/null 2>&1; then
    printf 'unsuccessful evaluator was accepted\n' >&2
    exit 1
fi

oversized_evaluator="$work_dir/oversized.sh"
cat > "$oversized_evaluator" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '[{"id":"score","value":1}]'
dd if=/dev/zero bs=1048576 count=1 2>/dev/null | tr '\0' ' '
EOF
chmod +x "$oversized_evaluator"
if "$skill_dir/scripts/verify-operational-value-evaluator.sh" "$oversized_evaluator" >/dev/null 2>&1; then
    printf 'oversized evaluator output was accepted\n' >&2
    exit 1
fi

printf 'operational-value-designer skill tests passed\n'