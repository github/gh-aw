#!/usr/bin/env bash
set -euo pipefail

JAVA_BIN="${JAVA_BIN:-java}"
: "${TLA2TOOLS_JAR:?Set TLA2TOOLS_JAR to an official tla2tools.jar}"
if ! command -v "$JAVA_BIN" >/dev/null || [ ! -f "$TLA2TOOLS_JAR" ]; then
    echo "Java and an existing TLA2TOOLS_JAR are required." >&2
    exit 1
fi

SPEC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="${TLC_RESULTS_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/dispatch-work-tlc.XXXXXX")}"
mkdir -p "$RESULTS_DIR"

run_model() {
    local config="$1"
    local invariant="${2:-}"
    local module="${3:-DispatchWorkCoordinator}"
    local output="$RESULTS_DIR/$config.log"
    local status=0
    "$JAVA_BIN" -XX:+UseParallelGC -Xmx1g -cp "$TLA2TOOLS_JAR" tlc2.TLC \
        -workers 2 -seed 1 -fp 0 -config "$SPEC_DIR/$config.cfg" \
        -metadir "$RESULTS_DIR/$config" "$SPEC_DIR/$module.tla" \
        >"$output" 2>&1 || status=$?
    if [ -z "$invariant" ]; then
        if [ "$status" -ne 0 ] || ! grep -q "Model checking completed. No error" "$output"; then
            cat "$output" >&2
            return 1
        fi
    elif [ "$status" -ne 12 ] || ! grep -q "Invariant $invariant is violated" "$output"; then
        cat "$output" >&2
        echo "Expected a $invariant counterexample, not success or a tooling failure." >&2
        return 1
    fi
    echo "$config: expected result"
    grep -E "^[0-9]+ states generated|^The depth of the complete|^Finished in" "$output"
}

if [ "$#" -eq 0 ]; then
    set -- DispatchWorkCoordinator Recovery Submission Selection \
        BrokenCAS BrokenTerminal BrokenSelection BrokenQueue
fi
for config in "$@"; do
    case "$config" in
        DispatchWorkCoordinator|Recovery|Submission) run_model "$config" ;;
        Selection) run_model Selection "" WorkQueueSelection ;;
        BrokenCAS) run_model BrokenCAS TerminalPersistence ;;
        BrokenTerminal) run_model BrokenTerminal TerminalFreeze ;;
        BrokenSelection) run_model BrokenSelection QueueStagingSoundness ;;
        BrokenQueue) run_model BrokenQueue QueueSelectionSoundness ;;
        *) echo "Unknown TLC configuration: $config" >&2; exit 2 ;;
    esac
done
echo "Full TLC reports: $RESULTS_DIR"
