#!/usr/bin/env python3
"""Dependency-free checks of the bounded-model runner, not model proofs."""

import os
import re
import shutil
import subprocess
import unittest
import uuid
from pathlib import Path

SPEC = Path(__file__).resolve().parent
ROOT = SPEC.parent.parent


class ModelRunnerTests(unittest.TestCase):
    def setUp(self):
        self.directory = ROOT / ".queue-validation-cache" / "model-runner-tests" / uuid.uuid4().hex
        self.directory.mkdir(parents=True)
        self.java = self.directory / "java"
        self.java.write_text(
            "#!/usr/bin/env bash\n"
            "printf '%s\\n' \"$FAKE_TLC_OUTPUT\"\n"
            "printf 'Finished in 00s\\n'\n"
            "exit \"$FAKE_TLC_STATUS\"\n"
        )
        self.java.chmod(0o755)
        self.jar = self.directory / "tla2tools.jar"
        self.jar.touch()

    def tearDown(self):
        shutil.rmtree(self.directory)

    def run_model(self, config, output, status=0, model="WorkerEvolution", default_results=False):
        env = {**os.environ, "JAVA_BIN": str(self.java), "TLA2TOOLS_JAR": str(self.jar),
               "TLC_MODEL_FILTER": model, "TLC_CONFIG_FILTER": config,
               "FAKE_TLC_OUTPUT": output, "FAKE_TLC_STATUS": str(status)}
        env.pop("TLC_RESULTS_DIR", None)
        if not default_results:
            env["TLC_RESULTS_DIR"] = str(self.directory / "results")
        return subprocess.run(["bash", str(SPEC / "check.sh")], cwd=ROOT, env=env,
                              capture_output=True, text=True, timeout=10)

    def test_positive_requires_success_status_and_exhaustion_message(self):
        accepted = self.run_model("WorkerEvolution", "Model checking completed. No error")
        self.assertEqual(accepted.returncode, 0, accepted.stderr)
        for output, status in (("Finished in 1s", 0),
                               ("Model checking completed. No error", 1)):
            with self.subTest(output=output, status=status):
                self.assertNotEqual(self.run_model("WorkerEvolution", output, status).returncode, 0)

    def test_negative_requires_exact_named_diagnostic_and_status(self):
        message = "Invariant FrozenDispatch is violated"
        accepted = self.run_model("BrokenEvolutionDispatch", message, 12)
        self.assertEqual(accepted.returncode, 0, accepted.stderr)
        for output, status in ((message, 0), (message, 151),
                               ("Invariant TypeOK is violated", 12),
                               ("Parsing failed", 12)):
            with self.subTest(output=output, status=status):
                rejected = self.run_model("BrokenEvolutionDispatch", output, status)
                self.assertNotEqual(rejected.returncode, 0)
                self.assertIn("Expected a FrozenDispatch counterexample", rejected.stderr)

    def test_witness_does_not_accept_safety_failure(self):
        rejected = self.run_model("EvolutionCompletionWitness", "Invariant Safety is violated", 12)
        self.assertNotEqual(rejected.returncode, 0)
        accepted = self.run_model("EvolutionCompletionWitness", "Invariant NoFrozenCompletion is violated", 12)
        self.assertEqual(accepted.returncode, 0, accepted.stderr)

    def test_reservation_recovery_configs_require_exact_verdicts(self):
        model = "DispatchReservationRecovery"
        accepted = self.run_model("ReservationRecovery", "Model checking completed. No error", model=model)
        self.assertEqual(accepted.returncode, 0, accepted.stderr)
        for config, invariant in (("BrokenReservationEvidence", "PrelaunchEvidence"),
                                  ("BrokenReservationCAS", "StartFence"),
                                  ("BrokenReservationRefund", "NoRefund"),
                                  ("BrokenReservationResend", "OneSender"),
                                  ("ReservationSiblingWitness", "NoSiblingRecovery"),
                                  ("ReservationRetryWitness", "NoRetryRecovery"),
                                  ("ReservationRaceWitness", "NoPrelaunchRace")):
            with self.subTest(config=config):
                result = self.run_model(config, f"Invariant {invariant} is violated", 12, model=model)
                self.assertEqual(result.returncode, 0, result.stderr)
                rejected = self.run_model(config, "Invariant Safety is violated", 12, model=model)
                self.assertNotEqual(rejected.returncode, 0)

    def test_unmatched_config_cannot_report_success(self):
        result = self.run_model("missing-config", "Model checking completed. No error")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("No configuration matches", result.stderr)

    def test_delivery_health_configs_require_exact_verdicts(self):
        model = "ClaimDeliveryHealth"
        for config in ("DeliveryHealth", "DeliveryHealthStaged"):
            result = self.run_model(config, "Model checking completed. No error", model=model)
            self.assertEqual(result.returncode, 0, result.stderr)
        for config, invariant in (
            ("BrokenDeliveryFalseGreen", "ReportTruth"),
            ("BrokenDeliveryEarlyStop", "IndependentSiblings"),
            ("BrokenDeliveryReleaseResult", "ResultAuthority"),
            ("BrokenDeliveryPrematureRelease", "ReleaseAuthority"),
            ("DeliveryPendingWitness", "NoPendingSibling"),
            ("DeliveryFailedWitness", "NoFailedSibling"),
            ("DeliveryMixedWitness", "NoMixedSuccess"),
            ("DeliveryReleasedWitness", "NoReleasedPending"),
        ):
            with self.subTest(config=config):
                result = self.run_model(config, f"Invariant {invariant} is violated", 12, model=model)
                self.assertEqual(result.returncode, 0, result.stderr)
                rejected = self.run_model(config, "Invariant Safety is violated", 12, model=model)
                self.assertNotEqual(rejected.returncode, 0)
        result = self.run_model("LifecycleReleasedPendingWitness",
                                "Invariant NoReleasedPendingDelivery is violated", 12,
                                model="QueueLifecycle")
        self.assertEqual(result.returncode, 0, result.stderr)
        rejected = self.run_model("LifecycleReleasedPendingWitness",
                                  "Invariant ResultSoundness is violated", 12,
                                  model="QueueLifecycle")
        self.assertNotEqual(rejected.returncode, 0)

    def test_unknown_model_is_rejected(self):
        result = self.run_model("WorkerEvolution", "", model="missing-model")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("TLC_MODEL_FILTER must be", result.stderr)

    def test_expiration_configs_require_exact_verdicts(self):
        model = "WorkExpiration"
        accepted = self.run_model("WorkExpiration", "Model checking completed. No error", model=model)
        self.assertEqual(accepted.returncode, 0, accepted.stderr)
        for config, invariant in (
            ("BrokenExpirationGrant", "GrantAuthority"),
            ("BrokenExpirationRelease", "ReservationAuthority"),
            ("BrokenExpirationClaim", "ClaimAuthority"),
            ("BrokenExpirationWatermark", "CommittedTimeMonotonicity"),
            ("BrokenExpirationCheckpoint", "ProjectionSoundness"),
            ("ExpirationRunningWitness", "NoRunningPastExpiry"),
            ("ExpirationCheckpointWitness", "NoCheckpointExpiry"),
        ):
            with self.subTest(config=config):
                accepted = self.run_model(config, f"Invariant {invariant} is violated", 12, model=model)
                self.assertEqual(accepted.returncode, 0, accepted.stderr)
                rejected = self.run_model(config, "Invariant TypeOK is violated", 12, model=model)
                self.assertNotEqual(rejected.returncode, 0)

    def test_retirement_configs_require_exact_verdicts(self):
        model = "ObsoleteWorkRetirement"
        for config in ("Retirement", "RetirementSingle"):
            accepted = self.run_model(config, "Model checking completed. No error", model=model)
            self.assertEqual(accepted.returncode, 0, accepted.stderr)
        for config, invariant in (
            ("BrokenRetirementExpiry", "SelectionExpiration"),
            ("BrokenRetirementPublication", "CancellationAuthority"),
            ("BrokenRetirementPin", "CancellationAuthority"),
            ("BrokenRetirementOwnership", "CancellationAuthority"),
            ("BrokenRetirementBarrier", "CancellationAuthority"),
            ("BrokenRetirementAvailability", "CancellationAuthority"),
            ("BrokenRetirementLegacy", "LegacyReplayCompatibility"),
            ("BrokenRetirementCheckpoint", "CheckpointMetadata"),
            ("BrokenRetirementTerminal", "TerminalMetadata"),
            ("BrokenRetirementDebt", "ProtectedState"),
            ("BrokenRetirementReservation", "ProtectedState"),
            ("BrokenRetirementResult", "ProtectedState"),
            ("BrokenRetirementMaintenance", "RecoveryIsolation"),
            ("RetirementCheckedWitness", "NoCheckedRetirement"),
            ("RetirementLegacyWitness", "NoLegacyCheckpoint"),
            ("RetirementRecoveryWitness", "NoRetirementRecovery"),
            ("RetirementExpirationWitness", "NoElapsedPrefix"),
        ):
            with self.subTest(config=config):
                accepted = self.run_model(config, f"Invariant {invariant} is violated", 12, model=model)
                self.assertEqual(accepted.returncode, 0, accepted.stderr)
                for output, status in ((f"Invariant {invariant} is violated", 0),
                                       ("Invariant TypeOK is violated", 12),
                                       ("Parsing failed", 151)):
                    rejected = self.run_model(config, output, status, model=model)
                    self.assertNotEqual(rejected.returncode, 0)

    def test_delivery_recovery_configs_require_exact_verdicts(self):
        model = "ClaimDeliveryRecovery"
        for config in ("DeliveryRecovery", "DeliveryRecoveryProgress"):
            result = self.run_model(config, "Model checking completed. No error", model=model)
            self.assertEqual(result.returncode, 0, result.stderr)
        for config, invariant in (
            ("BrokenRecoveryPrincipal", "CleanupAuthority"),
            ("BrokenRecoveryDeadline", "FailureBudget"),
            ("BrokenRecoveryEvidence", "FailureAuthority"),
            ("BrokenRecoveryEffects", "UnknownEffects"),
            ("BrokenRecoveryCAS", "PublicationFence"),
            ("BrokenRecoveryArtifact", "ResultAuthority"),
            ("BrokenRecoveryMetadata", "NativeReadbackAgreement"),
            ("BrokenRecoveryForeignMetadata", "NativeReadbackAgreement"),
            ("BrokenRecoveryReplay", "NoReplay"),
            ("BrokenRecoveryRetained", "RetainedFence"),
            ("RecoveryReleasedWitness", "NoReleasedRecovery"),
            ("RecoveryEnrichedWitness", "NoEnrichedResult"),
            ("RecoveryRetainedWitness", "NoRetainedRecovery"),
            ("RecoveryUnavailableWitness", "NoUnavailablePending"),
        ):
            with self.subTest(config=config):
                accepted = self.run_model(config, f"Invariant {invariant} is violated", 12, model=model)
                self.assertEqual(accepted.returncode, 0, accepted.stderr)
                rejected = self.run_model(config, "Invariant Safety is violated", 12, model=model)
                self.assertNotEqual(rejected.returncode, 0)

    def test_released_recovery_control_requires_temporal_counterexample(self):
        model = "ClaimDeliveryRecovery"
        message = "Temporal properties were violated"
        for config in ("BrokenRecoveryReleased", "BrokenRecoveryUnfair"):
            accepted = self.run_model(config, message, 13, model=model)
            self.assertEqual(accepted.returncode, 0, accepted.stderr)
            for output, status in ((message, 0), (message, 12),
                                   ("Invariant Safety is violated", 12), ("Parsing failed", 151)):
                with self.subTest(config=config, output=output, status=status):
                    rejected = self.run_model(config, output, status, model=model)
                    self.assertNotEqual(rejected.returncode, 0)

    def test_default_evidence_and_java_extraction_stay_repository_local(self):
        result = self.run_model("WorkerEvolution", "Model checking completed. No error", default_results=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        path = Path(result.stdout.split("Full TLC reports: ", 1)[1].strip())
        try:
            self.assertEqual(path.parent, ROOT / ".queue-validation-cache")
            self.assertTrue((path / "java").is_dir())
        finally:
            shutil.rmtree(path)
        for name in ("check.sh", "traces.sh"):
            source = (SPEC / name).read_text()
            self.assertNotIn("mktemp", source)
            self.assertNotIn("/tmp", source)
            self.assertIn('-Djava.io.tmpdir="$RESULTS_DIR/java"', source)

    def test_all_new_configs_are_registered_with_real_invariants(self):
        runner = (SPEC / "check.sh").read_text()
        for model, pattern in (("ClaimDeliveryHealth", r"(?:Delivery|BrokenDelivery).*\.cfg"),
                               ("ObsoleteWorkRetirement", r"(?:Retirement.*|BrokenRetirement.*)\.cfg"),
                               ("WorkExpiration", r"(?:WorkExpiration|BrokenExpiration.*|Expiration.*Witness)\.cfg"),
                               ("ClaimDeliveryRecovery", r"(?:DeliveryRecovery.*|BrokenRecovery.*|Recovery(?:Released|Enriched|Retained|Unavailable)Witness)\.cfg"),
                               ("QueueLifecycle", r"LifecycleReleasedPendingWitness\.cfg"),
                               ("DispatchReservationRecovery", r"(?:Reservation|BrokenReservation).*\.cfg"),
                               ("WorkerEvolution", r"(?:WorkerEvolution|BrokenEvolution|Evolution).*\.cfg"),
                               ("WorkerDeploymentBoundary", r"(?:WorkerDeploymentBoundary|BrokenBoundary|Boundary).*\.cfg"),
                               ("QueueBootstrap", r"(?:BrokenBootstrapHost|BrokenBootstrapEnrollment|BootstrapTrustedAW).*\.cfg")):
            source = (SPEC / f"{model}.tla").read_text()
            if model == "WorkerDeploymentBoundary":
                source += "\n" + (SPEC / "WorkerEvolution.tla").read_text()
            configurations = [path for path in SPEC.glob("*.cfg") if re.fullmatch(pattern, path.name)]
            if model == "ClaimDeliveryHealth":
                configurations = [path for path in configurations if not path.stem.startswith("DeliveryRecovery")]
            self.assertTrue(configurations)
            for path in configurations:
                with self.subTest(config=path.stem):
                    registration = re.search(rf"^run_model {path.stem} (.+)$", runner, re.MULTILINE)
                    self.assertIsNotNone(registration)
                    self.assertEqual(registration.group(1).split()[3], model)
                    match = re.search(r"^INVARIANTS? (.+)$", path.read_text(), re.MULTILINE)
                    self.assertIsNotNone(match)
                    for invariant in match.group(1).split():
                        self.assertRegex(source, rf"(?m)^{invariant} ==")
                    for prop in re.findall(r"^PROPERTY (.+)$", path.read_text(), re.MULTILINE):
                        self.assertRegex(source, rf"(?m)^{prop} ==")


if __name__ == "__main__":
    unittest.main()
