#!/usr/bin/env bash

set -euo pipefail

skill_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
repo_root=$(CDPATH='' cd -- "$skill_dir/../../.." && pwd)

grep -Fq 'graders.<distinct-id>.script' "$skill_dir/SKILL.md"
grep -Fq '256 KiB (262,144 UTF-8 bytes, inclusive)' "$skill_dir/SKILL.md"
grep -Fq 'helpers.createSessionLookups(session)' "$skill_dir/SKILL.md"
grep -Fq 'Return a finite number, `null`, or an object with `value`' "$skill_dir/SKILL.md"
grep -Fq 'in an isolated downstream `graders` job' "$skill_dir/SKILL.md"
grep -Fq 'Replace `graders.operational-value.run` with an inline JavaScript `script`' "$skill_dir/SKILL.md"
grep -Fq 'ultimate operational condition' "$skill_dir/SKILL.md"
grep -Fq 'Select the furthest downstream rung that satisfies all three' "$skill_dir/SKILL.md"
grep -Fq 'Missing evidence for the selected rung returns `null`; it never causes runtime fallback to a weaker rung' "$skill_dir/SKILL.md"

if grep -Eq 'GH_AW_SESSION_FILE|GH_AW_GRADER_CONFIG|grader-shell-file-write|repository-owned Bash compatibility' "$skill_dir/SKILL.md"; then
    printf 'legacy operational-value shell guidance remains\n' >&2
    exit 1
fi

for removed in \
    "$skill_dir/scripts/operational-value-evaluator-path.sh" \
    "$skill_dir/scripts/verify-operational-value-evaluator.sh" \
    "$skill_dir/scripts/verify-operational-value-contract-change.sh" \
    "$repo_root/actions/setup/js/operational_value_grader.cjs" \
    "$repo_root/actions/setup/js/operational_value_worker.cjs"
do
    [[ ! -e $removed ]] || {
        printf 'legacy operational-value shell surface remains: %s\n' "$removed" >&2
        exit 1
    }
done

for guidance in \
    AGENTS.md \
    .github/aw/instructions.md \
    .github/aw/intent.md \
    .github/aw/create-agentic-workflow.md \
    .github/aw/update-agentic-workflow.md \
    .github/skills/developer-internals/SKILL.md
do
    grep -Fq 'graders.<distinct-id>.script' "$repo_root/$guidance"
done

node - "$repo_root" <<'NODE'
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const root = process.argv[2];
const text = fs.readFileSync(path.join(root, ".github/skills/operational-value-designer/SKILL.md"), "utf8");
const scripts = [...text.matchAll(/^    script: \|\n((?:      [^\n]*\n|\n)+)/gm)];
assert.equal(scripts.length, 1, "the documented custom grader must remain executable");
const script = scripts[0][1].replace(/^      /gm, "");
const { runCustomGraders } = require(path.join(root, "actions/setup/js/custom_graders.cjs"));
const config = { repository: "OWNER/REPO", issueNumber: 123 };
const event = data => ({ type: "safe_output.result", data, provenance: { source: "safe_output", job: "safe_outputs" } });
const result = { type: "add_comment", repo: "OWNER/REPO", number: 123, url: "https://github.com/OWNER/REPO/issues/123#issuecomment-1" };
for (const [name, session, value, configuration = config] of [
  ["attained", [event(result)], 1],
  ["missed", [event({ ...result, number: 456 })], 0],
  ["unavailable", [], null],
  ["malformed", [event({ ...result, number: "123" })], null],
  ["invalid-target", [event(result)], null, { ...config, repository: "" }],
]) {
  for (const id of ["comment-target-match", "operational-value"]) {
    const graders = [{ id, source: "inline", direction: "higher_is_better", threshold: 1, config: configuration, script }];
    const first = runCustomGraders(session, graders).output.results[0];
    const second = runCustomGraders(session, graders).output.results[0];
    assert.equal(first.value, value, `${name}:${id}`);
    assert.equal(first.source, "inline", `${name}:${id}:source`);
    assert.equal(first.status, value === null ? "unavailable" : value === 1 ? "pass" : "fail", `${name}:${id}:status`);
    assert.deepEqual(first, second, `${name}:${id}:determinism`);
  }
}
assert.throws(
  () => runCustomGraders([], [{ id: "operational-value", source: "inline", run: "./score.sh" }]),
  /JavaScript script/
);
NODE

printf 'operational-value-designer skill tests passed\n'
