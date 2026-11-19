#!/usr/bin/env bash
# EXPERIMENTAL: bounded TLC checks for the token broker threat model.
set -euo pipefail

JAVA_BIN="${JAVA_BIN:-java}"
: "${TLA2TOOLS_JAR:?Set TLA2TOOLS_JAR to an official tla2tools.jar}"
if ! command -v "$JAVA_BIN" >/dev/null || [ ! -f "$TLA2TOOLS_JAR" ]; then
    echo "Java and an existing TLA2TOOLS_JAR are required." >&2
    exit 1
fi

SPEC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="${TLC_RESULTS_DIR:-${TMPDIR:-/tmp}/token-broker-tlc-$(date +%Y%m%d-%H%M%S)-$$}"
mkdir -p "$RESULTS_DIR/java"
RESULTS_DIR="$(cd "$RESULTS_DIR" && pwd)"

# run_model CONFIG [VIOLATION] [KIND]
# Without VIOLATION, the model must pass. With VIOLATION, TLC must report a
# counterexample for it (status 12 for invariants, 13 for properties).
run_model() {
    local config="$1"
    local violation="${2:-}"
    local kind="${3:-Invariant}"
    if [ -n "${TLC_CONFIG_FILTER:-}" ] && [ "$config" != "$TLC_CONFIG_FILTER" ]; then
        return
    fi
    local expected_status=12
    [ "$kind" = "Temporal" ] && expected_status=13
    local output="$RESULTS_DIR/$config.log"
    local status=0
    "$JAVA_BIN" -Djava.io.tmpdir="$RESULTS_DIR/java" -XX:+UseParallelGC -Xmx2g -cp "$TLA2TOOLS_JAR" tlc2.TLC \
        -workers "${TLC_WORKERS:-2}" -seed 1 -fp 0 -deadlock -noGenerateSpecTE -config "$SPEC_DIR/$config.cfg" \
        -metadir "$RESULTS_DIR/$config" "$SPEC_DIR/TokenBrokerThreatModel.tla" \
        >"$output" 2>&1 || status=$?
    if [ -z "$violation" ]; then
        if [ "$status" -ne 0 ] || ! grep -q "Model checking completed. No error" "$output"; then
            cat "$output" >&2
            return 1
        fi
    else
        local message="Invariant $violation is violated"
        [ "$kind" = "Temporal" ] && message="Temporal property $violation was violated"
        if [ "$status" -ne "$expected_status" ] || ! grep -Fq "$message" "$output"; then
            cat "$output" >&2
            echo "Expected a $violation counterexample, not success or a tooling failure." >&2
            return 1
        fi
    fi
    echo "$config: expected result"
}

# Hardened profile: SV-01..SV-35 plus the candidate fixes.
run_model Hardened
run_model HardenedPersistentRunner

# Attack vectors TLC finds against the SV-01..SV-35 baseline.
run_model DiscoverPersistentRunner NoCredentialDisclosure
run_model DiscoverReusableCaller RepositoryBound
run_model DiscoverInvalidationRace InvalidationComplete
run_model DiscoverSkippedCleanup CompletionRevokes Temporal
run_model WitnessAgentAuthoredCI NoAgentAuthoredCI

# Negative controls: removing one requirement re-opens a known threat.
run_model BrokenEnvExclusion NoCredentialDisclosure
run_model BrokenHostStep NoCredentialDisclosure
run_model BrokenAudience NoCredentialDisclosure
run_model BrokenTenant ApprovedContextOnly
run_model BrokenRevision RepositoryBound
run_model BrokenPRTrust ApprovedContextOnly
run_model BrokenJobIdentity ReaderInAgentJob
run_model BrokenCloseCheck NoCredentialDisclosure
run_model BrokenRecordOrder InvalidationComplete
run_model BrokenRunnerCheck NoCredentialDisclosure
