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

hash_file() {
    if command -v sha256sum >/dev/null; then
        sha256sum "$1"
    elif command -v shasum >/dev/null; then
        shasum -a 256 "$1"
    else
        echo "sha256sum or shasum is required to bind traces to sources." >&2
        return 1
    fi
}

capture_sources() {
    for model in WorkQueue FairWorkQueue ClaimScopedWorker QueueService QueueLifecycle QueueEvolution; do
        hash_file "$SPEC_DIR/$model.tla"
    done
    for config in WorkQueue FairBatch FairDAGGitHub ClaimScopeMixed ServiceDynamic LifecyclePacked EvolutionRolling \
                  CompetingClaimsWitness RecoveryWitness ExternalEffectWitness WeakOrderingWitness \
                  PartialCompletionWitness MixedClaimDAGWitness LifecycleMixedDAGWitness \
                  LifecycleActivationWitness LifecycleConflictWitness \
                  EvolutionOverlapWitness EvolutionPinnedWitness EvolutionLocalWitness \
                  EvolutionDeliveryWitness EvolutionCASWitness EvolutionCapacityWitness; do
        hash_file "$SPEC_DIR/$config.cfg"
    done
    hash_file "$SPEC_DIR/check.sh"
    hash_file "$SPEC_DIR/traces.sh"
    hash_file "$TLA2TOOLS_JAR"
}

capture_sources >"$RESULTS_DIR/sources-before.sha256"
"$JAVA_BIN" -version >"$RESULTS_DIR/java-version.log" 2>&1
printf 'workers=1 seed=1 fp=0 depth=%s traces=%s\n' "$TRACE_DEPTH" "$TRACE_COUNT" >"$RESULTS_DIR/simulation-settings.txt"

for entry in "WorkQueue WorkQueue simulation" \
             "FairWorkQueue FairBatch FairBatch" \
             "FairWorkQueue FairDAGGitHub FairDAGGitHub" \
             "ClaimScopedWorker ClaimScopeMixed ClaimScopeMixed" \
             "QueueService ServiceDynamic ServiceDynamic" \
             "QueueLifecycle LifecyclePacked LifecyclePacked" \
             "QueueEvolution EvolutionRolling EvolutionRolling"; do
    read -r model config prefix <<<"$entry"
    "$JAVA_BIN" -XX:+UseParallelGC -Xmx1g -cp "$TLA2TOOLS_JAR" tlc2.TLC \
        -workers 1 -seed 1 -fp 0 -simulate "file=$RESULTS_DIR/$prefix,num=$TRACE_COUNT" \
        -depth "$TRACE_DEPTH" -config "$SPEC_DIR/$config.cfg" \
        -metadir "$RESULTS_DIR/$prefix-meta" "$SPEC_DIR/$model.tla" \
        >"$RESULTS_DIR/$prefix.log" 2>&1 || {
        cat "$RESULTS_DIR/$prefix.log" >&2
        exit 1
    }
    if ! grep -q "Finished in" "$RESULTS_DIR/$prefix.log" || \
       ! compgen -G "$RESULTS_DIR/${prefix}_*" >/dev/null; then
        cat "$RESULTS_DIR/$prefix.log" >&2
        echo "Expected completed simulation and emitted traces for $config." >&2
        exit 1
    fi
    echo "$config: seeded simulation traces"
done

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

for entry in "FairWorkQueue PartialCompletionWitness" \
             "ClaimScopedWorker MixedClaimDAGWitness" \
             "QueueLifecycle LifecycleMixedDAGWitness" \
             "QueueLifecycle LifecycleActivationWitness" \
             "QueueLifecycle LifecycleConflictWitness" \
             "QueueEvolution EvolutionOverlapWitness" \
             "QueueEvolution EvolutionPinnedWitness" \
             "QueueEvolution EvolutionLocalWitness" \
             "QueueEvolution EvolutionDeliveryWitness" \
             "QueueEvolution EvolutionCASWitness" \
             "QueueEvolution EvolutionCapacityWitness"; do
    read -r model config <<<"$entry"
    TLC_MODEL_FILTER="$model" TLC_CONFIG_FILTER="$config" \
        TLC_RESULTS_DIR="$RESULTS_DIR/current-witnesses" \
        bash "$SPEC_DIR/check.sh"
done

capture_sources >"$RESULTS_DIR/sources-after.sha256"
if ! cmp -s "$RESULTS_DIR/sources-before.sha256" "$RESULTS_DIR/sources-after.sha256"; then
    echo "Trace sources changed during execution; evidence is not source-stable." >&2
    exit 1
fi

echo "Historical simulation traces (at most $TRACE_DEPTH states, $TRACE_COUNT traces): $RESULTS_DIR/simulation_*"
echo "Current abstraction traces: $RESULTS_DIR/{FairBatch,FairDAGGitHub,ClaimScopeMixed,ServiceDynamic,LifecyclePacked,EvolutionRolling}_*"
echo "Textual counterexample reports: $RESULTS_DIR/*Witness.log"
echo "Current guarded witness reports: $RESULTS_DIR/current-witnesses/*Witness.log"
echo "Source/tool hashes and execution settings: $RESULTS_DIR"
