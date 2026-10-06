#!/usr/bin/env bash
set -euo pipefail

JAVA_BIN="${JAVA_BIN:-java}"
: "${TLA2TOOLS_JAR:?Set TLA2TOOLS_JAR to an official tla2tools.jar}"
if ! command -v "$JAVA_BIN" >/dev/null || [ ! -f "$TLA2TOOLS_JAR" ]; then
    echo "Java and an existing TLA2TOOLS_JAR are required." >&2
    exit 1
fi

SPEC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
case "${TLC_MODEL_FILTER:-}" in
    ""|WorkQueue|FairWorkQueue) ;;
    *)
        echo "TLC_MODEL_FILTER must be WorkQueue or FairWorkQueue when set." >&2
        exit 1
        ;;
esac
RESULTS_DIR="${TLC_RESULTS_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/work-queue-tlc.XXXXXX")}"
mkdir -p "$RESULTS_DIR"
RUN_COUNT=0

run_model() {
    local config="$1"
    local violation="${2:-}"
    local kind="${3:-Invariant}"
    local expected_status="${4:-12}"
    local module="${5:-WorkQueue}"
    local expected_message="${6:-$kind $violation is violated}"
    if [ -n "${TLC_MODEL_FILTER:-}" ] && [ "$module" != "$TLC_MODEL_FILTER" ]; then
        return
    fi
    if [ -n "${TLC_CONFIG_FILTER:-}" ] && [ "$config" != "$TLC_CONFIG_FILTER" ]; then
        return
    fi
    RUN_COUNT=$((RUN_COUNT + 1))
    local output="$RESULTS_DIR/$config.log"
    local status=0
    "$JAVA_BIN" -XX:+UseParallelGC -Xmx1g -cp "$TLA2TOOLS_JAR" tlc2.TLC \
        -workers 2 -seed 1 -fp 0 -config "$SPEC_DIR/$config.cfg" \
        -metadir "$RESULTS_DIR/$config" "$SPEC_DIR/$module.tla" \
        >"$output" 2>&1 || status=$?
    if [ -z "$violation" ]; then
        if [ "$status" -ne 0 ] || ! grep -q "Model checking completed. No error" "$output"; then
            cat "$output" >&2
            return 1
        fi
    elif [ "$status" -ne "$expected_status" ] || ! grep -Fq "$expected_message" "$output"; then
        cat "$output" >&2
        echo "Expected a $violation counterexample, not success or a tooling failure." >&2
        return 1
    fi
    echo "$config: expected result"
    grep -E "states generated|depth of the complete|Finished in" "$output"
}

run_model WorkQueue
run_model Recovery
run_model QueueOrdering
run_model BrokenCAS TerminalPersistence
run_model BrokenTerminal TerminalFreeze
run_model BrokenQueueSelection QueueSelection "Action property" 13
run_model WeakOrderingWitness NoOutOfOrderClaim
run_model FairBatch "" Invariant 0 FairWorkQueue
run_model FairThreeClaim "" Invariant 0 FairWorkQueue
run_model FairPriority "" Invariant 0 FairWorkQueue
run_model FairStrict "" Invariant 0 FairWorkQueue
run_model FairDAGChain "" Invariant 0 FairWorkQueue
run_model FairDAGForward "" Invariant 0 FairWorkQueue
run_model FairDAGFork "" Invariant 0 FairWorkQueue
run_model FairDAGGitHub "" Invariant 0 FairWorkQueue
run_model FairGitHubDependencies "" Invariant 0 FairWorkQueue
run_model BrokenBatchSelection DecisionValidity Invariant 12 FairWorkQueue
run_model BrokenClaimEffects EffectAuthorization Invariant 12 FairWorkQueue
run_model BrokenAssignmentHandle ClaimClosureAuthority Invariant 12 FairWorkQueue
run_model BrokenBatchCAS TerminalPersistence Invariant 12 FairWorkQueue
run_model BrokenBatchRelease RunReleaseAuthority Invariant 12 FairWorkQueue
run_model BrokenDAGDependency DependencyAuthorization Invariant 12 FairWorkQueue
run_model BrokenDAGResult ResultEffectSoundness Invariant 12 FairWorkQueue
run_model BrokenDAGCycle DAGValidity Invariant 151 FairWorkQueue \
    "The invariant of DAGValidity is equal to FALSE"
run_model BrokenExternalDependency ExternalAuthorization Invariant 12 FairWorkQueue
run_model BrokenPRClosedAsMerged ExternalTruth Invariant 12 FairWorkQueue
run_model BatchedAssignmentWitness NoBatchedAssignment Invariant 12 FairWorkQueue
run_model PartialCompletionWitness NoPartialCompletion Invariant 12 FairWorkQueue
run_model DAGJoinWitness NoJoinClaim Invariant 12 FairWorkQueue
if [ "$RUN_COUNT" -eq 0 ]; then
    echo "No configuration matches the requested TLC filters." >&2
    exit 1
fi
echo "Full TLC reports: $RESULTS_DIR"
