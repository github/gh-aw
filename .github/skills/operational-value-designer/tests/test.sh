#!/usr/bin/env bash

set -euo pipefail

skill_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
repo_root=$(CDPATH='' cd -- "$skill_dir/../../.." && pwd)
work_dir=$(mktemp -d "$repo_root/.operational-value-designer-test.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

for text in \
    'graders.operational-value.script' \
    'safe_output.result' \
    'githubnext/gh-cao' \
    'schema-example-coverage-v1'
do
    grep -Fq "$text" "$skill_dir/SKILL.md"
done

for text in \
    'nonempty ordered array of unique `{id,value}`' \
    'The first metric supplies top-level `operational-value.value`' \
    'never ignore or translate shell automatically' \
    '256 KiB'
do
    grep -Fq "$text" "$skill_dir/references/run-scoped.md"
done

for guidance in \
    AGENTS.md \
    .github/aw/instructions.md \
    .github/aw/intent.md \
    .github/aw/create-agentic-workflow.md \
    .github/aw/update-agentic-workflow.md \
    .github/skills/developer-internals/SKILL.md
do
    grep -Fq 'graders.operational-value.script' "$repo_root/$guidance"
done

node - "$skill_dir/references/run-scoped.md" "$work_dir/evaluator.js" <<'NODE'
const assert = require("node:assert/strict");
const fs = require("node:fs");
const [skill, output] = process.argv.slice(2);
const text = fs.readFileSync(skill, "utf8");
const match = text.match(/    script: \|\n((?:      [^\n]*\n)+)/);
assert.ok(match, "documented operational-value script is missing");
fs.writeFileSync(output, match[1].replace(/^      /gm, ""));
NODE

"$skill_dir/scripts/verify-operational-value-evaluator.sh" \
    "$work_dir/evaluator.js" "$skill_dir/tests/fixtures.json" >/dev/null

assert_rejected() {
    local name=$1 source=$2
    printf '%s\n' "$source" > "$work_dir/$name.js"
    if "$skill_dir/scripts/verify-operational-value-evaluator.sh" \
        "$work_dir/$name.js" "$skill_dir/tests/fixtures.json" >/dev/null 2>&1; then
        printf 'invalid operational-value JavaScript was accepted: %s\n' "$name" >&2
        exit 1
    fi
}

assert_rejected scalar 'return 1'
assert_rejected empty-array 'return []'
assert_rejected duplicate-ids 'return [{id:"same",value:1},{id:"same",value:0}]'
assert_rejected non-finite 'return [{id:"primary",value:NaN},{id:"recorded-comment-count",value:1}]'
assert_rejected extra-field 'return [{id:"primary",value:1,extra:true},{id:"recorded-comment-count",value:1}]'
assert_rejected promise 'return Promise.resolve([{id:"primary",value:1}])'

test ! -e "$repo_root/.github/graders/daily-file-diet-operational-value.sh"
test ! -e "$repo_root/.github/graders/daily-file-diet-operational-value.fixtures.json"

printf 'operational-value-designer skill tests passed\n'
