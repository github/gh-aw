#!/usr/bin/env bash
# EXPERIMENTAL: bounded TLC checks for the exact-effect authorization model.
set -euo pipefail

JAVA_BIN="${JAVA_BIN:-java}"
: "${TLA2TOOLS_JAR:?Set TLA2TOOLS_JAR to an official tla2tools.jar}"
if ! command -v "$JAVA_BIN" >/dev/null || [ ! -f "$TLA2TOOLS_JAR" ]; then
    echo "Java and an existing TLA2TOOLS_JAR are required." >&2
    exit 1
fi

SPEC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="${TLC_RESULTS_DIR:-${TMPDIR:-/tmp}/exact-effect-tlc-$(date +%Y%m%d-%H%M%S)-$$}"
mkdir -p "$RESULTS_DIR/java"
RESULTS_DIR="$(cd "$RESULTS_DIR" && pwd)"

# run_model CONFIG [VIOLATION]
# Without VIOLATION, the model must pass. With VIOLATION, TLC must report a
# counterexample for that invariant (exit status 12).
run_model() {
    local config="$1"
    local violation="${2:-}"
    if [ -n "${TLC_CONFIG_FILTER:-}" ] && [ "$config" != "$TLC_CONFIG_FILTER" ]; then
        return
    fi
    local output="$RESULTS_DIR/$config.log"
    local status=0
    "$JAVA_BIN" -Djava.io.tmpdir="$RESULTS_DIR/java" -XX:+UseParallelGC -Xmx2g -cp "$TLA2TOOLS_JAR" tlc2.TLC \
        -workers "${TLC_WORKERS:-2}" -seed 1 -fp 0 -deadlock -config "$SPEC_DIR/$config.cfg" \
        -metadir "$RESULTS_DIR/$config" "$SPEC_DIR/ExactEffectAuthorization.tla" \
        >"$output" 2>&1 || status=$?
    if [ -z "$violation" ]; then
        if [ "$status" -ne 0 ] || ! grep -q "Model checking completed. No error" "$output"; then
            cat "$output" >&2
            return 1
        fi
    else
        if [ "$status" -ne 12 ] || ! grep -Fq "Invariant $violation is violated" "$output"; then
            cat "$output" >&2
            echo "Expected a $violation counterexample, not success or a tooling failure." >&2
            return 1
        fi
    fi
    echo "$config: expected result"
}

# Hardened protocol, with and without an authority that requires resolved_effect.
run_model Hardened
run_model HardenedResolvedOnly
run_model HardenedStaged

# Witnesses: reachable behavior, including the documented residual race.
run_model WitnessCompletePlan NoCompletePlan
run_model WitnessPartialFailure NoPartialFailure
run_model WitnessResidualRace NoResidualRace

# Negative controls: removing one protection re-opens a known threat.
run_model BrokenDowngrade NoDowngrade
run_model BrokenForgedPromotion HonestBinding
run_model BrokenWireCheck SentEqualsPlan
run_model BrokenFreshness FreshPermission
run_model BrokenRetry SendOnce
run_model BrokenClaim SingleDecision
run_model BrokenDerivedSlot SentEqualsPlan
run_model BrokenRevalidation RevalidatedSend
run_model BrokenPartialSuccess HonestOutcome
run_model BrokenLease AtomicLease
