#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT=$(mktemp)
FIXTURE_ROOT=$(mktemp -d)
trap 'rm -f "$OUTPUT"; rm -rf "$FIXTURE_ROOT"' EXIT

# Exercise TYPE-020 against the live handler and fixtures with weakened file-count and duplicate-filename guards.
(
    fixture="$FIXTURE_ROOT/type020"
    mkdir -p "$fixture/actions/setup/js" "$fixture/pkg/workflow"
    cp "$REPO_ROOT/actions/setup/js/merge_pull_request.cjs" "$fixture/actions/setup/js/merge_pull_request.cjs"
    printf "must be 'blocked', 'allowed'\n" >"$fixture/pkg/workflow/merge_pull_request.go"
    printf 'AddDefault("protected_files_policy", "blocked")\n' >"$fixture/pkg/workflow/safe_outputs_handler_registry_pull_requests.go"
    cd "$fixture"

    log_high() { echo "HIGH: $1"; }
    log_critical() { echo "CRITICAL: $1"; }
    log_pass() { echo "PASS: $1"; }
    eval "$(sed -n '/^check_merge_pull_request_protected_files() {/,/^}/p' "$SCRIPT_DIR/check-safe-outputs-conformance.sh")"

    check_merge_pull_request_protected_files >"$OUTPUT"
    if ! grep -q '^PASS: TYPE-020:' "$OUTPUT" || grep -q '^HIGH:' "$OUTPUT"; then
        echo "FAIL: TYPE-020 must pass when both changed-file safeguards are present"
        cat "$OUTPUT"
        exit 1
    fi

    sed 's/pr\.changed_files > 3000/pr.changed_files > 4000/' actions/setup/js/merge_pull_request.cjs >"$OUTPUT"
    cp "$OUTPUT" actions/setup/js/merge_pull_request.cjs
    check_merge_pull_request_protected_files >"$OUTPUT"
    if ! grep -q "HIGH: TYPE-020: Changed-file counts above GitHub's 3000-file listing limit are not rejected" "$OUTPUT" ||
        grep -q '^PASS:' "$OUTPUT"; then
        echo "FAIL: TYPE-020 must reject a weakened changed-file upper bound"
        cat "$OUTPUT"
        exit 1
    fi

    cp "$REPO_ROOT/actions/setup/js/merge_pull_request.cjs" actions/setup/js/merge_pull_request.cjs
    grep -vF 'new Set(files.map(file => file.filename)).size !== files.length' actions/setup/js/merge_pull_request.cjs >"$OUTPUT"
    cp "$OUTPUT" actions/setup/js/merge_pull_request.cjs
    check_merge_pull_request_protected_files >"$OUTPUT"
    if ! grep -q 'HIGH: TYPE-020: Incomplete or duplicate changed-file evidence is not rejected' "$OUTPUT" ||
        grep -q '^PASS:' "$OUTPUT"; then
        echo "FAIL: TYPE-020 must reject removed duplicate-filename validation"
        cat "$OUTPUT"
        exit 1
    fi
)

# Exercise SEC-005 independently so unsafe production handlers must still be reported.
(
    cd "$FIXTURE_ROOT"
    mkdir -p actions/setup/js
    for name in adapter_checks adapter_test adapter.test adapter_checks_helper unsafe_handler; do
        printf 'const adapter = { "target-repo": "owner/repo" };\n' >"actions/setup/js/$name.cjs"
    done
    printf '// @safe-outputs-exempt SEC-005: fixed scope validated upstream\nconst targetRepo = "owner/repo";\n' >actions/setup/js/exempt_handler.cjs
    printf 'const target_repo = "owner/repo";\nvalidateTargetRepo(target_repo);\n' >actions/setup/js/allowed_handler.cjs

    for name in unified_session_payload unified_session_render; do
        cp "$REPO_ROOT/actions/setup/js/$name.cjs" "actions/setup/js/$name.cjs"
        sed '/@safe-outputs-exempt.*SEC-005/d' "$REPO_ROOT/actions/setup/js/$name.cjs" >"actions/setup/js/${name}_unexempted.cjs"
    done
    printf 'const targetRepo = "owner/repo";\noctokit.rest.issues.create({ owner: targetRepo.split("/")[0], repo: targetRepo.split("/")[1], title: "unsafe" });\n' >actions/setup/js/unsafe_handler.cjs

    log_high() { echo "HIGH: $1"; }
    log_pass() { echo "PASS: $1"; }
    eval "$(sed -n '/^check_cross_repo() {/,/^}/p' "$SCRIPT_DIR/check-safe-outputs-conformance.sh")"
    check_cross_repo >"$OUTPUT"

    if [[ $(grep -c '^HIGH:' "$OUTPUT") -ne 4 ]] ||
        ! grep -q 'adapter_checks_helper.cjs supports target-repo' "$OUTPUT" ||
        ! grep -q 'unsafe_handler.cjs supports target-repo' "$OUTPUT" ||
        ! grep -q 'unified_session_payload_unexempted.cjs supports target-repo' "$OUTPUT" ||
        ! grep -q 'unified_session_render_unexempted.cjs supports target-repo' "$OUTPUT"; then
        echo "FAIL: SEC-005 must skip only tests, allowlisted handlers, and documented exemptions"
        cat "$OUTPUT"
        exit 1
    fi
    if grep -q '^PASS:' "$OUTPUT"; then
        echo "FAIL: SEC-005 must not pass with unsafe production handlers"
        exit 1
    fi

    rm actions/setup/js/adapter_checks_helper.cjs actions/setup/js/unsafe_handler.cjs \
        actions/setup/js/unified_session_payload_unexempted.cjs actions/setup/js/unified_session_render_unexempted.cjs
    check_cross_repo >"$OUTPUT"
    if ! grep -q '^PASS: SEC-005: All cross-repo handlers validate allowlists' "$OUTPUT" ||
        grep -q '^HIGH:' "$OUTPUT"; then
        echo "FAIL: SEC-005 must pass for fixtures and authorized production handlers"
        cat "$OUTPUT"
        exit 1
    fi
)

# Exercise REQ-002 with a long type and an incomplete type followed by a complete sibling.
(
    cd "$FIXTURE_ROOT"
    spec="docs/src/content/docs/specs/safe-outputs-specification.md"
    mkdir -p "$(dirname "$spec")"
    {
        printf '#### Type: long_type\n\n**MCP Tool Schema**:\n'
        for _ in $(seq 1 250); do printf 'filler\n'; done
        printf '**Operational Semantics**:\n**Configuration Parameters**:\n**Security Requirements**:\n**Required Permissions**:\n\n'
    } >"$spec"

    log_medium() { echo "MEDIUM: $1"; }
    log_pass() { echo "PASS: $1"; }
    eval "$(sed -n '/^check_type_completeness() {/,/^}/p' "$SCRIPT_DIR/check-safe-outputs-conformance.sh")"
    check_type_completeness >"$OUTPUT"
    if ! grep -q '^PASS: REQ-002: All safe output types have complete documentation' "$OUTPUT" || grep -q '^MEDIUM:' "$OUTPUT"; then
        echo "FAIL: REQ-002 must count required sections across the whole type block"
        cat "$OUTPUT"
        exit 1
    fi

    {
        printf '\n#### Type: short_type\n\n**MCP Tool Schema**:\n**Operational Semantics**:\n'
        printf '\n#### Type: short_type_sibling\n\n**MCP Tool Schema**:\n**Operational Semantics**:\n'
        printf '**Configuration Parameters**:\n**Security Requirements**:\n**Required Permissions**:\n\n### Next Section\n'
    } >>"$spec"
    check_type_completeness >"$OUTPUT"
    if ! grep -q "^MEDIUM: REQ-002: Type 'short_type' has only 2/5 required sections" "$OUTPUT" || grep -q '^PASS:' "$OUTPUT"; then
        echo "FAIL: REQ-002 must report incomplete types and must not pass"
        cat "$OUTPUT"
        exit 1
    fi
)

# Exercise WTD-002 through the getDetectionWarningBlocks caution chain, and reject a fallback whose caution template lost its label.
(
    cd "$FIXTURE_ROOT"
    rm -rf actions
    mkdir -p actions/setup/js actions/setup/md
    printf 'const { getDetectionWarningBlocks } = require("./messages_run_status.cjs");\nconst conclusion = process.env.GH_AW_DETECTION_CONCLUSION;\nawait githubClient.rest.pulls.create({});\nmodule.exports = { getDetectionWarningBlocks, conclusion };\n' >actions/setup/js/push_to_pull_request_branch.cjs
    printf 'const template = "threat_detection_caution.md";\nmodule.exports = { template };\n' >actions/setup/js/messages_run_status.cjs
    printf '> [!CAUTION]\n> agentic threat detected\n> <!-- gh-aw-threat-detected -->\n' >actions/setup/md/threat_detection_caution.md

    log_high() { echo "HIGH: $1"; }
    log_medium() { echo "MEDIUM: $1"; }
    log_pass() { echo "PASS: $1"; }
    eval "$(sed -n '/^check_wtd_convertible_fallback() {/,/^}/p' "$SCRIPT_DIR/check-safe-outputs-conformance.sh")"
    check_wtd_convertible_fallback >"$OUTPUT"
    if ! grep -q '^PASS: WTD-002: push_to_pull_request_branch has convertible fallback' "$OUTPUT" || grep -q '^HIGH:' "$OUTPUT"; then
        echo "FAIL: WTD-002 must accept the getDetectionWarningBlocks caution chain"
        cat "$OUTPUT"
        exit 1
    fi

    printf '> [!CAUTION]\n> threat detected\n' >actions/setup/md/threat_detection_caution.md
    check_wtd_convertible_fallback >"$OUTPUT"
    if ! grep -q "^HIGH: WTD-002: push_to_pull_request_branch fallback missing 'agentic threat detected' text" "$OUTPUT" || grep -q '^PASS:' "$OUTPUT"; then
        echo "FAIL: WTD-002 must reject a fallback whose caution template lacks the threat label"
        cat "$OUTPUT"
        exit 1
    fi
)

# Exercise SEC-014 against negative fixtures for missing documentation, staged ordering, protocol limits, and redirects.
(
    fixture="$FIXTURE_ROOT/sec014"
    mkdir -p "$fixture/docs/src/content/docs/specs" "$fixture/pkg/workflow" "$fixture/actions/setup/js"
    printf '#### Safe-output invocation authorization\n' >"$fixture/docs/src/content/docs/specs/safe-outputs-specification.md"
    : >"$fixture/docs/src/content/docs/specs/safe-output-authorization.tsp"
    cp "$REPO_ROOT/pkg/workflow/safe_outputs_authorization.go" "$fixture/pkg/workflow/safe_outputs_authorization.go"
    cp "$REPO_ROOT/actions/setup/js/safe_output_authorization.cjs" "$fixture/actions/setup/js/safe_output_authorization.cjs"
    cp "$REPO_ROOT/actions/setup/js/safe_output_authorization_gate.cjs" "$fixture/actions/setup/js/safe_output_authorization_gate.cjs"
    cp "$REPO_ROOT/actions/setup/js/service_correlation.cjs" "$fixture/actions/setup/js/service_correlation.cjs"
    log_critical() { echo "CRITICAL: $1"; }
    log_high() { echo "HIGH: $1"; }
    log_medium() { echo "MEDIUM: $1"; }
    log_pass() { echo "PASS: $1"; }
    eval "$(sed -n '/^check_safe_output_invocation_authorization() {/,/^}/p' "$SCRIPT_DIR/check-safe-outputs-conformance.sh")"
    run_sec014() (
        cd "$fixture"
        check_safe_output_invocation_authorization >"$OUTPUT"
    )
    assert_sec014_failure() {
        local expected="$1"
        run_sec014
        if ! grep -qF "$expected" "$OUTPUT" || grep -q '^PASS: SEC-014:' "$OUTPUT"; then
            echo "FAIL: SEC-014 must report '$expected'"
            cat "$OUTPUT"
            exit 1
        fi
    }

    run_sec014
    if ! grep -q '^PASS: SEC-014:' "$OUTPUT" || grep -qE '^(CRITICAL|HIGH|MEDIUM):' "$OUTPUT"; then
        echo "FAIL: SEC-014 must pass with the authorization requirements present"
        cat "$OUTPUT"
        exit 1
    fi

    rm "$fixture/docs/src/content/docs/specs/safe-output-authorization.tsp"
    sed 's/u\.Scheme != "https"/u.Scheme != "http"/' "$fixture/pkg/workflow/safe_outputs_authorization.go" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/pkg/workflow/safe_outputs_authorization.go"
    assert_sec014_failure "CRITICAL: SEC-014: Compiler does not reject non-HTTPS authorization endpoints"

    cp "$REPO_ROOT/pkg/workflow/safe_outputs_authorization.go" "$fixture/pkg/workflow/safe_outputs_authorization.go"
    cp "$REPO_ROOT/actions/setup/js/safe_output_authorization.cjs" "$fixture/actions/setup/js/safe_output_authorization.cjs"
    cp "$REPO_ROOT/actions/setup/js/safe_output_authorization_gate.cjs" "$fixture/actions/setup/js/safe_output_authorization_gate.cjs"
    touch "$fixture/docs/src/content/docs/specs/safe-output-authorization.tsp"
    python3 - "$fixture/actions/setup/js/safe_output_authorization.cjs" "$fixture/actions/setup/js/safe_output_authorization_gate.cjs" <<'PY'
import re
import sys

client_path, gate_path = sys.argv[1:]
client = open(client_path, encoding="utf-8").read()
gate = open(gate_path, encoding="utf-8").read()
client_guard = re.search(r"(?m)^  if \(staged === true\) \{\n.*?^  \}\n", client, re.S)
gate_guard = re.search(r"(?m)^  if \(staged\) \{\n.*?^  \}\n", gate, re.S)
if not client_guard or not gate_guard:
    raise SystemExit("FAIL: staged guards not found for ordering fixtures")
client = client[:client_guard.start()] + client[client_guard.end():]
client_anchor = "    const token = await acquireToken({ core, env, config, fetchImpl, deadline, now, wallNow: currentUTC });\n"
if client_anchor not in client:
    raise SystemExit("FAIL: client credential request anchor not found")
client = client.replace(client_anchor, client_anchor + client_guard.group(), 1)
gate = gate[:gate_guard.start()] + gate[gate_guard.end():]
gate_anchor = '  const claim = claimHandle ? options.claimIdentity || require("./work_queue_claim_scope.cjs").claimIdentity(claimHandle) : null;\n'
if gate_anchor not in gate:
    raise SystemExit("FAIL: gate Claim loading anchor not found")
gate = gate.replace(gate_anchor, gate_anchor + gate_guard.group(), 1)
open(client_path, "w", encoding="utf-8").write(client)
open(gate_path, "w", encoding="utf-8").write(gate)
PY
    assert_sec014_failure "CRITICAL: SEC-014: Authorization client does not short-circuit staged mode"
    assert_sec014_failure "CRITICAL: SEC-014: Authorization gate does not short-circuit staged mode"

    for bound in 'MAX_REQUEST_BYTES:65536' 'MAX_RESPONSE_BYTES:8192' 'HTTP_TIMEOUT_MS:10000' 'TOTAL_TIMEOUT_MS:20000' 'MIN_VALIDITY_MS:2000' 'MAX_VALIDITY_MS:30000'; do
        IFS=':' read -r name value <<< "$bound"
        cp "$REPO_ROOT/actions/setup/js/safe_output_authorization.cjs" "$fixture/actions/setup/js/safe_output_authorization.cjs"
        sed "s/const ${name} = ${value};/const ${name} = ${value}0;/" "$fixture/actions/setup/js/safe_output_authorization.cjs" >"$OUTPUT"
        cp "$OUTPUT" "$fixture/actions/setup/js/safe_output_authorization.cjs"
        assert_sec014_failure "HIGH: SEC-014: Authorization client missing specified bound: ${name} = ${value}"
    done

    cp "$REPO_ROOT/actions/setup/js/safe_output_authorization.cjs" "$fixture/actions/setup/js/safe_output_authorization.cjs"
    sed '/fetchImpl(url, { method: "GET"/s/, redirect: "manual"//' "$fixture/actions/setup/js/safe_output_authorization.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/safe_output_authorization.cjs"
    assert_sec014_failure "CRITICAL: SEC-014: OIDC credential request does not disable automatic redirects"

    cp "$REPO_ROOT/actions/setup/js/safe_output_authorization.cjs" "$fixture/actions/setup/js/safe_output_authorization.cjs"
    sed '/fetchImpl(config.endpoint, {/,/});/s/redirect: "manual",//' "$fixture/actions/setup/js/safe_output_authorization.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/safe_output_authorization.cjs"
    assert_sec014_failure "CRITICAL: SEC-014: Authorization decision request does not disable automatic redirects"
)

# Exercise IMP-006 against weakened target branches and submit-review execution paths.
(
    fixture="$FIXTURE_ROOT/imp006"
    mkdir -p "$fixture/docs/src/content/docs/specs" "$fixture/actions/setup/js"
    {
        for type in create_pull_request_review_comment submit_pull_request_review reply_to_pull_request_review_comment; do
            case "$type" in
                create_pull_request_review_comment) prefix=CRC ;;
                submit_pull_request_review) prefix=SPR ;;
                reply_to_pull_request_review_comment) prefix=RRC ;;
            esac
            printf '#### Type: %s\n**Target Authorization**\n' "$type"
            for requirement in 001 002 003 004; do printf '**%s-%s**\n' "$prefix" "$requirement"; done
            printf '\n'
        done
    } >"$fixture/docs/src/content/docs/specs/safe-outputs-specification.md"
    cp "$REPO_ROOT/actions/setup/js/create_pr_review_comment.cjs" "$fixture/actions/setup/js/create_pr_review_comment.cjs"
    cp "$REPO_ROOT/actions/setup/js/submit_pr_review.cjs" "$fixture/actions/setup/js/submit_pr_review.cjs"
    cp "$REPO_ROOT/actions/setup/js/reply_to_pr_review_comment.cjs" "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs"
    log_high() { echo "HIGH: $1"; }
    log_pass() { echo "PASS: $1"; }
    eval "$(sed -n '/^check_pr_review_target_authorization() {/,/^}/p' "$SCRIPT_DIR/check-safe-outputs-conformance.sh")"
    run_imp006() (
        cd "$fixture"
        check_pr_review_target_authorization >"$OUTPUT"
    )
    assert_imp006_failure() {
        local expected="$1"
        run_imp006
        if ! grep -qF "$expected" "$OUTPUT" || grep -q '^PASS: IMP-006:' "$OUTPUT"; then
            echo "FAIL: IMP-006 must report '$expected'"
            cat "$OUTPUT"
            exit 1
        fi
    }

    run_imp006
    if ! grep -q '^PASS: IMP-006:' "$OUTPUT" || grep -q '^HIGH:' "$OUTPUT"; then
        echo "FAIL: IMP-006 must pass with the review target requirements present"
        cat "$OUTPUT"
        exit 1
    fi

    sed 's/const targetConfig = config\.target || "triggering";/const targetConfig = "*";/' "$REPO_ROOT/actions/setup/js/submit_pr_review.cjs" >"$fixture/actions/setup/js/submit_pr_review.cjs"
    assert_imp006_failure 'does not default an omitted target to "triggering" (SPR-001)'

    cp "$REPO_ROOT/actions/setup/js/submit_pr_review.cjs" "$fixture/actions/setup/js/submit_pr_review.cjs"
    sed '/async function handleWithRegistry/,/async function handleWithLegacyBuffer/s/      targetConfig,/      targetConfig: "*",/' "$fixture/actions/setup/js/submit_pr_review.cjs" >"$OUTPUT"
    sed '/async function handleWithLegacyBuffer/,/async function setReviewContextOnBuffer/s/        targetConfig,/        targetConfig: "*",/' "$OUTPUT" >"$fixture/actions/setup/js/submit_pr_review.cjs"
    assert_imp006_failure 'does not resolve and propagate the configured target in the registry path'
    assert_imp006_failure 'does not resolve and propagate the configured target in the legacy path'

    for path in registry legacy; do
        cp "$REPO_ROOT/actions/setup/js/submit_pr_review.cjs" "$fixture/actions/setup/js/submit_pr_review.cjs"
        if [ "$path" = registry ]; then
            sed '/async function handleWithRegistry/,/async function handleWithLegacyBuffer/s/const targetResult = resolveTarget({/const targetResult = null;/' "$fixture/actions/setup/js/submit_pr_review.cjs" >"$OUTPUT"
        else
            sed '/async function handleWithLegacyBuffer/,/async function setReviewContextOnBuffer/s/const targetResult = resolveTarget({/const targetResult = null;/' "$fixture/actions/setup/js/submit_pr_review.cjs" >"$OUTPUT"
        fi
        cp "$OUTPUT" "$fixture/actions/setup/js/submit_pr_review.cjs"
        if [ "$path" = registry ]; then
            assert_imp006_failure 'does not resolve and propagate the configured target in the registry path'
        else
            assert_imp006_failure 'does not resolve and propagate the configured target in the legacy path'
        fi
    done

    cp "$REPO_ROOT/actions/setup/js/submit_pr_review.cjs" "$fixture/actions/setup/js/submit_pr_review.cjs"
    sed 's/const prNum = targetResult\.number;/const prNum = message.pull_request_number;/' "$fixture/actions/setup/js/submit_pr_review.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/submit_pr_review.cjs"
    assert_imp006_failure 'does not resolve and propagate the configured target in the registry path'
    assert_imp006_failure 'does not resolve and propagate the configured target in the legacy path'

    cp "$REPO_ROOT/actions/setup/js/create_pr_review_comment.cjs" "$fixture/actions/setup/js/create_pr_review_comment.cjs"
    sed '/\/\/ Default behavior: use triggering PR/,/if (!pullRequestNumber)/s/pullRequestNumber = effectivePayload\.pull_request\.number;/pullRequestNumber = commentItem.pull_request_number;/' "$fixture/actions/setup/js/create_pr_review_comment.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/create_pr_review_comment.cjs"
    assert_imp006_failure 'does not bind triggering targets to the triggering PR (CRC-001-003)'

    cp "$REPO_ROOT/actions/setup/js/create_pr_review_comment.cjs" "$fixture/actions/setup/js/create_pr_review_comment.cjs"
    sed '/} else if (commentTarget && commentTarget !== "triggering")/,/\/\/ Default behavior: use triggering PR/s/parseInt(commentTarget, 10)/parseInt(commentItem.pull_request_number, 10)/' "$fixture/actions/setup/js/create_pr_review_comment.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/create_pr_review_comment.cjs"
    assert_imp006_failure 'does not bind fixed targets to configured target (CRC-002-003)'

    cp "$REPO_ROOT/actions/setup/js/create_pr_review_comment.cjs" "$fixture/actions/setup/js/create_pr_review_comment.cjs"
    sed '/if (commentTarget === "\*")/,/} else if (commentTarget && commentTarget !== "triggering")/s/if (commentItem\.pull_request_number) {/if (true) {/' "$fixture/actions/setup/js/create_pr_review_comment.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/create_pr_review_comment.cjs"
    assert_imp006_failure 'does not require pull_request_number for wildcard targets (CRC-004)'

    cp "$REPO_ROOT/actions/setup/js/reply_to_pr_review_comment.cjs" "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs"
    sed '/if (replyTarget === "triggering")/,/} else if (replyTarget === "\*")/s/targetPRNumber = triggeringPRNumber;/targetPRNumber = item.pull_request_number;/' "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs"
    assert_imp006_failure 'does not bind triggering targets to the triggering PR (RRC-001-003)'

    cp "$REPO_ROOT/actions/setup/js/reply_to_pr_review_comment.cjs" "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs"
    sed '/\/\/ Explicit PR number in target/,/\/\/ Apply required-labels/s/parseInt(replyTarget, 10)/parseInt(item.pull_request_number, 10)/' "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs"
    assert_imp006_failure 'does not bind fixed targets to configured target (RRC-002-003)'

    cp "$REPO_ROOT/actions/setup/js/reply_to_pr_review_comment.cjs" "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs"
    sed '/} else if (replyTarget === "\*")/,/\/\/ Explicit PR number in target/s/if (!targetPRNumber) {/if (true) {/' "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs" >"$OUTPUT"
    cp "$OUTPUT" "$fixture/actions/setup/js/reply_to_pr_review_comment.cjs"
    assert_imp006_failure 'does not require pull_request_number for wildcard targets (RRC-004)'
)

bash "$SCRIPT_DIR/check-safe-outputs-transport-integrity_test.sh"

(cd "$REPO_ROOT" && bash "$SCRIPT_DIR/check-safe-outputs-conformance.sh" 2>&1 | sed -E $'s/\033\\[[0-9;]*[[:alpha:]]//g' >"$OUTPUT") || true

if ! grep -q 'SEC-005: All cross-repo handlers validate allowlists' "$OUTPUT" ||
    grep -q '\[HIGH\].*SEC-005:' "$OUTPUT"; then
    echo "FAIL: Expected no SEC-005 cross-repository authorization gaps"
    grep 'SEC-005:' "$OUTPUT" || true
    exit 1
fi

for finding in TYPE-016 MCE-001 USE-001 USE-003 WTD-002; do
    if grep -E "\\[(HIGH|MEDIUM|LOW)\\] $finding:" "$OUTPUT" >/dev/null; then
        echo "FAIL: Expected no $finding safe-output conformance findings"
        grep -E "\\[(HIGH|MEDIUM|LOW)\\] $finding:" "$OUTPUT"
        exit 1
    fi
done

mapfile -t findings < <(grep "IMP-004: Safe output config property is missing" "$OUTPUT" || true)
mapfile -t target_findings < <(grep "IMP-005: Safe output target authorization conformance gap" "$OUTPUT" || true)

if [[ ${#findings[@]} -ne 0 ]]; then
    echo "FAIL: Expected no safe-output config schema gaps"
    printf '  %s\n' "${findings[@]}"
    exit 1
fi

if [[ ${#target_findings[@]} -ne 0 ]]; then
    echo "FAIL: Expected no safe-output target authorization conformance gaps"
    printf '  %s\n' "${target_findings[@]}"
    exit 1
fi

if ! grep -q "IMP-004: All safe output config properties are declared in the schema" "$OUTPUT"; then
    echo "FAIL: Expected IMP-004 complete schema coverage result"
    exit 1
fi

if ! grep -q "IMP-005: Safe output target authorization is specified, tested, and enforced" "$OUTPUT"; then
    echo "FAIL: Expected IMP-005 target authorization coverage result"
    exit 1
fi

echo "PASS: SEC-005 enforces cross-repository authorization, IMP-004 resolves referenced safe-output schemas, and IMP-005 enforces target authorization coverage"
