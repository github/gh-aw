#!/usr/bin/env bash
set -euo pipefail

JAVA_BIN="${JAVA_BIN:-java}"
: "${TLA2TOOLS_JAR:?Set TLA2TOOLS_JAR to an official tla2tools.jar}"
if ! command -v "$JAVA_BIN" >/dev/null || [ ! -f "$TLA2TOOLS_JAR" ]; then
    echo "Java and an existing TLA2TOOLS_JAR are required." >&2
    exit 1
fi

TRACE_DEPTH="${TLC_TRACE_DEPTH:-16}"
TRACE_COUNT="${TLC_TRACE_COUNT:-3}"
if [[ ! "$TRACE_DEPTH" =~ ^[1-9][0-9]*$ || ! "$TRACE_COUNT" =~ ^[1-9][0-9]*$ ]]; then
    echo "TLC_TRACE_DEPTH and TLC_TRACE_COUNT must be positive integers." >&2
    exit 1
fi

SPEC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="${TLC_RESULTS_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/work-queue-traces.XXXXXX")}"
mkdir -p "$RESULTS_DIR"
RESULTS_DIR="$(cd "$RESULTS_DIR" && pwd)"

"$JAVA_BIN" -XX:+UseParallelGC -Xmx1g -cp "$TLA2TOOLS_JAR" tlc2.TLC \
    -workers 1 -seed 1 -fp 0 -simulate "file=$RESULTS_DIR/simulation,num=$TRACE_COUNT" \
    -depth "$TRACE_DEPTH" -config "$SPEC_DIR/WorkQueue.cfg" \
    -metadir "$RESULTS_DIR/simulation-meta" "$SPEC_DIR/WorkQueue.tla" \
    >"$RESULTS_DIR/simulation.log" 2>&1 || {
    cat "$RESULTS_DIR/simulation.log" >&2
    exit 1
}

for entry in "CompetingClaimsWitness NoCompetingClaims" \
             "RecoveryWitness NoRecoveredOrphan" \
             "ExternalEffectWitness NoExternalEffect" \
             "WeakOrderingWitness NoOutOfOrderClaim"; do
    read -r config invariant <<<"$entry"
    status=0
    "$JAVA_BIN" -XX:+UseParallelGC -Xmx1g -cp "$TLA2TOOLS_JAR" tlc2.TLC \
        -workers 1 -seed 1 -fp 0 -config "$SPEC_DIR/$config.cfg" \
        -metadir "$RESULTS_DIR/$config" "$SPEC_DIR/WorkQueue.tla" \
        >"$RESULTS_DIR/$config.log" 2>&1 || status=$?
    if [ "$status" -ne 12 ] || ! grep -q "Invariant $invariant is violated" "$RESULTS_DIR/$config.log"; then
        cat "$RESULTS_DIR/$config.log" >&2
        echo "Expected a $invariant counterexample, not success or a tooling failure." >&2
        exit 1
    fi
    echo "$config: expected $invariant counterexample"
done

echo "Simulation traces (at most $TRACE_DEPTH states, $TRACE_COUNT traces): $RESULTS_DIR/simulation_*"
echo "Textual counterexample reports: $RESULTS_DIR/*Witness.log"
