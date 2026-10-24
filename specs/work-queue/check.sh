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
    ""|WorkQueue|FairWorkQueue|ClaimScopedWorker|ClaimDeliveryHealth|ClaimDeliveryRecovery|QueueService|QueueLifecycle|DispatchReservationRecovery|IssueProjection|IssueClosureCancellation|QueueBootstrap|QueuePoolInstall|WorkerEvolution|WorkerDeploymentBoundary|WorkExpiration) ;;
    *)
        echo "TLC_MODEL_FILTER must be WorkQueue, FairWorkQueue, ClaimScopedWorker, ClaimDeliveryHealth, ClaimDeliveryRecovery, QueueService, QueueLifecycle, DispatchReservationRecovery, IssueProjection, IssueClosureCancellation, QueueBootstrap, QueuePoolInstall, WorkerEvolution, WorkerDeploymentBoundary, or WorkExpiration when set." >&2
        exit 1
        ;;
esac
RESULTS_DIR="${TLC_RESULTS_DIR:-$SPEC_DIR/../../.queue-validation-cache/tlc-$(date +%Y%m%d-%H%M%S)-$$}"
mkdir -p "$RESULTS_DIR/java"
RESULTS_DIR="$(cd "$RESULTS_DIR" && pwd)"
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
    "$JAVA_BIN" -Djava.io.tmpdir="$RESULTS_DIR/java" -XX:+UseParallelGC -Xmx1g -cp "$TLA2TOOLS_JAR" tlc2.TLC \
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
run_model WorkExpiration "" Invariant 0 WorkExpiration
run_model BrokenExpirationGrant GrantAuthority Invariant 12 WorkExpiration
run_model BrokenExpirationRelease ReservationAuthority Invariant 12 WorkExpiration
run_model BrokenExpirationClaim ClaimAuthority Invariant 12 WorkExpiration
run_model BrokenExpirationWatermark CommittedTimeMonotonicity Invariant 12 WorkExpiration
run_model BrokenExpirationCheckpoint ProjectionSoundness Invariant 12 WorkExpiration
run_model ExpirationRunningWitness NoRunningPastExpiry Invariant 12 WorkExpiration
run_model ExpirationCheckpointWitness NoCheckpointExpiry Invariant 12 WorkExpiration
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
run_model FairKeepOpen "" Invariant 0 FairWorkQueue
run_model BrokenKeepOpenGrant RetainedFinishSafety Invariant 12 FairWorkQueue
run_model BrokenKeepOpenResult RetainedFinishSafety Invariant 12 FairWorkQueue
run_model BrokenKeepOpenDelivery RetainedFinishSafety Invariant 12 FairWorkQueue
run_model BrokenKeepOpenRelease RunReleaseAuthority Invariant 12 FairWorkQueue
run_model BrokenKeepOpenOutcome RetainedFinishSafety Invariant 12 FairWorkQueue
run_model KeepOpenReclaimWitness NoRetainedReclaim Invariant 12 FairWorkQueue
run_model KeepOpenFinalWitness NoRetainedFinalCompletion Invariant 12 FairWorkQueue
run_model KeepOpenFailureWitness NoRetainedFailure Invariant 12 FairWorkQueue
run_model KeepOpenReservationWitness NoRetainedLiveReservation Invariant 12 FairWorkQueue
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
run_model BrokenProjection ProjectionSoundness Invariant 12 FairWorkQueue
run_model BatchedAssignmentWitness NoBatchedAssignment Invariant 12 FairWorkQueue
run_model PartialCompletionWitness NoPartialCompletion Invariant 12 FairWorkQueue
run_model DAGJoinWitness NoJoinClaim Invariant 12 FairWorkQueue
run_model ClaimScopeSingle "" Invariant 0 ClaimScopedWorker
run_model ClaimScopeMixed "" Invariant 0 ClaimScopedWorker
run_model BrokenMissingClaimScope OutputScope Invariant 12 ClaimScopedWorker
run_model BrokenForeignClaimScope OutputScope Invariant 12 ClaimScopedWorker
run_model BrokenLastOpenClaimScope OutputScope Invariant 12 ClaimScopedWorker
run_model BrokenCancelledClaimOutput EffectAuthorization Invariant 12 ClaimScopedWorker
run_model BrokenMixedDAGAdmission DAGAuthorization Invariant 12 ClaimScopedWorker
run_model SingleClaimScopeWitness NoAutomaticScope Invariant 12 ClaimScopedWorker
run_model MixedClaimDAGWitness NoMixedDAGProgress Invariant 12 ClaimScopedWorker
run_model DeliveryHealth "" Invariant 0 ClaimDeliveryHealth
run_model DeliveryHealthStaged "" Invariant 0 ClaimDeliveryHealth
run_model BrokenDeliveryFalseGreen ReportTruth Invariant 12 ClaimDeliveryHealth
run_model BrokenDeliveryEarlyStop IndependentSiblings Invariant 12 ClaimDeliveryHealth
run_model BrokenDeliveryReleaseResult ResultAuthority Invariant 12 ClaimDeliveryHealth
run_model BrokenDeliveryPrematureRelease ReleaseAuthority Invariant 12 ClaimDeliveryHealth
run_model DeliveryPendingWitness NoPendingSibling Invariant 12 ClaimDeliveryHealth
run_model DeliveryFailedWitness NoFailedSibling Invariant 12 ClaimDeliveryHealth
run_model DeliveryMixedWitness NoMixedSuccess Invariant 12 ClaimDeliveryHealth
run_model DeliveryReleasedWitness NoReleasedPending Invariant 12 ClaimDeliveryHealth
run_model DeliveryRecovery "" Invariant 0 ClaimDeliveryRecovery
run_model DeliveryRecoveryProgress "" Invariant 0 ClaimDeliveryRecovery
run_model BrokenRecoveryReleased EventualSettlement Temporal 13 ClaimDeliveryRecovery \
    "Temporal properties were violated"
run_model BrokenRecoveryUnfair EventualSettlement Temporal 13 ClaimDeliveryRecovery \
    "Temporal properties were violated"
run_model BrokenRecoveryPrincipal CleanupAuthority Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryDeadline FailureBudget Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryEvidence FailureAuthority Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryEffects UnknownEffects Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryCAS PublicationFence Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryArtifact ResultAuthority Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryMetadata NativeReadbackAgreement Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryForeignMetadata NativeReadbackAgreement Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryReplay NoReplay Invariant 12 ClaimDeliveryRecovery
run_model BrokenRecoveryRetained RetainedFence Invariant 12 ClaimDeliveryRecovery
run_model RecoveryReleasedWitness NoReleasedRecovery Invariant 12 ClaimDeliveryRecovery
run_model RecoveryEnrichedWitness NoEnrichedResult Invariant 12 ClaimDeliveryRecovery
run_model RecoveryRetainedWitness NoRetainedRecovery Invariant 12 ClaimDeliveryRecovery
run_model RecoveryUnavailableWitness NoUnavailablePending Invariant 12 ClaimDeliveryRecovery
run_model ServiceFixed "" Invariant 0 QueueService
run_model ServiceDynamic "" Invariant 0 QueueService
run_model BrokenServiceShare ServiceDeviation Invariant 12 QueueService
run_model StrictServiceStarvation EventualService Temporal 13 QueueService \
    "Temporal properties were violated"
run_model UnfairServiceStarvation EventualService Temporal 13 QueueService \
    "Temporal properties were violated"
run_model UnpackableServiceStarvation EventualService Temporal 13 QueueService \
    "Temporal properties were violated"
run_model LifecycleRecurring "" Invariant 0 QueueLifecycle
run_model LifecyclePacked "" Invariant 0 QueueLifecycle
run_model BrokenLifecyclePrefix FairPrefixPacking Invariant 12 QueueLifecycle
run_model BrokenLifecycleSender OneSender Invariant 12 QueueLifecycle
run_model BrokenLifecycleBinding BindingAuthority Invariant 12 QueueLifecycle
run_model BrokenLifecycleActivation BindingAuthority Invariant 12 QueueLifecycle
run_model BrokenLifecycleRerun AttemptOneEffects Invariant 12 QueueLifecycle
run_model BrokenLifecycleRelease ReservationAuthority Invariant 12 QueueLifecycle
run_model BrokenLifecycleConflictRelease ReservationAuthority Invariant 12 QueueLifecycle
run_model BrokenLifecycleControl ControlNotPreemption Invariant 12 QueueLifecycle
run_model BrokenLifecycleDelivery DeliveryExclusivity Invariant 12 QueueLifecycle
run_model BrokenLifecycleReplacement ReplacementSafety Invariant 12 QueueLifecycle
run_model BrokenLifecycleVerificationBudget DeliveryFailureBudget Invariant 12 QueueLifecycle
run_model LifecycleMixedDAGWitness NoMixedLifecycleWitness Invariant 12 QueueLifecycle
run_model LifecycleActivationWitness NoActivationRecoveryWitness Invariant 12 QueueLifecycle
run_model LifecycleConflictWitness NoConflictReservationWitness Invariant 12 QueueLifecycle
run_model LifecycleReleasedPendingWitness NoReleasedPendingDelivery Invariant 12 QueueLifecycle
run_model ReservationRecovery "" Invariant 0 DispatchReservationRecovery
run_model BrokenReservationEvidence PrelaunchEvidence Invariant 12 DispatchReservationRecovery
run_model BrokenReservationCAS StartFence Invariant 12 DispatchReservationRecovery
run_model BrokenReservationRefund NoRefund Invariant 12 DispatchReservationRecovery
run_model BrokenReservationResend OneSender Invariant 12 DispatchReservationRecovery
run_model ReservationSiblingWitness NoSiblingRecovery Invariant 12 DispatchReservationRecovery
run_model ReservationRetryWitness NoRetryRecovery Invariant 12 DispatchReservationRecovery
run_model ReservationRaceWitness NoPrelaunchRace Invariant 12 DispatchReservationRecovery
run_model IssueExisting "" Invariant 0 IssueProjection
run_model IssueCreated "" Invariant 0 IssueProjection
run_model IssueDefaultOpen "" Invariant 0 IssueProjection
run_model IssuePRStaysOpen "" Invariant 0 IssueProjection
run_model IssueKeepOpen "" Invariant 0 IssueProjection
run_model IssueKeepOpenWitness NoRetainedProjection Invariant 12 IssueProjection
run_model IssueBrokenKeepOpenClose ClosureAuthority Invariant 12 IssueProjection
run_model IssueBrokenUnowned AdmissionAuthority Invariant 12 IssueProjection
run_model IssueBrokenPayloadClose ClosureAuthority Invariant 12 IssueProjection
run_model IssueBrokenRetry SingleCreation Invariant 12 IssueProjection
run_model IssueClosureAvailable "" Invariant 0 IssueClosureCancellation
run_model IssueClosureActive "" Invariant 0 IssueClosureCancellation
run_model IssueClosureCompleted "" Invariant 0 IssueClosureCancellation
run_model IssueClosureCancelled "" Invariant 0 IssueClosureCancellation
run_model IssueClosureUnbound "" Invariant 0 IssueClosureCancellation
run_model IssueClosureStaleLabels "" Invariant 0 IssueClosureCancellation
run_model IssueClosureReopened "" Invariant 0 IssueClosureCancellation
run_model IssueClosureIdentityChanged "" Invariant 0 IssueClosureCancellation
run_model IssueClosureMissingQueue "" Invariant 0 IssueClosureCancellation
run_model BootstrapEmpty "" Invariant 0 QueueBootstrap
run_model BootstrapInitialized "" Invariant 0 QueueBootstrap
run_model BootstrapSameBranch "" Invariant 0 QueueBootstrap
run_model BrokenBootstrapConflict AbsenceAuthority Invariant 12 QueueBootstrap
run_model BrokenBootstrapPreparation PreparationAuthority Invariant 12 QueueBootstrap
run_model BrokenBootstrapBranch SeparateQueueBranch Invariant 12 QueueBootstrap
run_model BrokenBootstrapRoute PublicationAuthority Invariant 12 QueueBootstrap
run_model BrokenBootstrapAtomic AtomicGenesis Invariant 12 QueueBootstrap
run_model BootstrapDeferredWitness NoDeferredPreparation Invariant 12 QueueBootstrap
run_model BootstrapRetryWitness NoDeploymentRetry Invariant 12 QueueBootstrap
run_model BootstrapLostResponseWitness NoLostResponseRecovery Invariant 12 QueueBootstrap
run_model BootstrapLostResponseFailureWitness NoLostResponseFailure Invariant 12 QueueBootstrap
run_model BrokenBootstrapHost TrustedAWAuthority Invariant 12 QueueBootstrap
run_model BrokenBootstrapEnrollment NoEnrollmentGate Invariant 12 QueueBootstrap
run_model BootstrapTrustedAWWitness NoTrustedAWBootstrap Invariant 12 QueueBootstrap
run_model PoolInstall "" Invariant 0 QueuePoolInstall
run_model PoolInstallExpandedRepositoryAllowlist "" Invariant 0 QueuePoolInstall
run_model PoolInstallInvalidProposal "" Invariant 0 QueuePoolInstall
run_model PoolInstallInvalidPool "" Invariant 0 QueuePoolInstall
run_model PoolInstallInvalidRoute "" Invariant 0 QueuePoolInstall
run_model PoolInstallUnapprovedRoute "" Invariant 0 QueuePoolInstall
run_model PoolInstallUnauthorized "" Invariant 0 QueuePoolInstall
run_model PoolInstallProducerWitness NoProducerInstall Invariant 12 QueuePoolInstall
run_model WorkerEvolution "" Invariant 0 WorkerEvolution
run_model WorkerEvolutionPrincipal "" Invariant 0 WorkerEvolution
run_model BrokenEvolutionCAS DeploymentCAS Invariant 12 WorkerEvolution
run_model BrokenEvolutionRevision RevisionImmutability Invariant 12 WorkerEvolution
run_model BrokenEvolutionPin PinnedExecution Invariant 12 WorkerEvolution
run_model BrokenEvolutionContract ReadyGrants Invariant 12 WorkerEvolution
run_model BrokenEvolutionAvailability ReadyGrants Invariant 12 WorkerEvolution
run_model BrokenEvolutionGlobalPause LocalReadiness Invariant 12 WorkerEvolution
run_model BrokenEvolutionDebt DebtPreservation Invariant 12 WorkerEvolution
run_model BrokenEvolutionResult ResultPreservation Invariant 12 WorkerEvolution
run_model BrokenEvolutionReservation ReservationPreservation Invariant 12 WorkerEvolution
run_model BrokenEvolutionDispatch FrozenDispatch Invariant 12 WorkerEvolution
run_model BrokenEvolutionCredential FrozenCredential Invariant 12 WorkerEvolution
run_model BrokenEvolutionEconomics EconomicPolicy Invariant 12 WorkerEvolution
run_model EvolutionCompatibleWitness NoCompatibleReroute Invariant 12 WorkerEvolution
run_model EvolutionPinWitness NoPinnedOldRevision Invariant 12 WorkerEvolution
run_model EvolutionIncompatibleWitness NoLocalIncompatibility Invariant 12 WorkerEvolution
run_model EvolutionUnavailableWitness NoLocalUnavailability Invariant 12 WorkerEvolution
run_model EvolutionCompletionWitness NoFrozenCompletion Invariant 12 WorkerEvolution
run_model EvolutionRaceWitness NoDeploymentRace Invariant 12 WorkerEvolution
run_model WorkerDeploymentBoundary "" Invariant 0 WorkerDeploymentBoundary
run_model BrokenBoundaryAvailabilityCAS HistoricalAvailabilityCAS Invariant 12 WorkerDeploymentBoundary
run_model BrokenBoundaryAvailabilityRoute HistoricalRoutePreservation Invariant 12 WorkerDeploymentBoundary
run_model BrokenBoundaryWorker NoWorkerDeployment Invariant 12 WorkerDeploymentBoundary
run_model BrokenBoundaryApproval CallerApprovalAuthority Invariant 12 WorkerDeploymentBoundary
run_model BrokenBoundaryDebt DebtPreservation Invariant 12 WorkerDeploymentBoundary
run_model BoundaryPinnedAvailabilityWitness NoPinnedAvailabilityUpdate Invariant 12 WorkerDeploymentBoundary
run_model BoundaryUnapprovedWitness NoUnapprovedLocalPause Invariant 12 WorkerDeploymentBoundary
run_model BoundaryCompletionWitness NoFrozenCallerCompletion Invariant 12 WorkerDeploymentBoundary
if [ "$RUN_COUNT" -eq 0 ]; then
    echo "No configuration matches the requested TLC filters." >&2
    exit 1
fi
echo "Full TLC reports: $RESULTS_DIR"
