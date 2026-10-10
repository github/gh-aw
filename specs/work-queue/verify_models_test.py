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

    def test_unmatched_config_cannot_report_success(self):
        result = self.run_model("missing-config", "Model checking completed. No error")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("No configuration matches", result.stderr)

    def test_unknown_model_is_rejected(self):
        result = self.run_model("WorkerEvolution", "", model="missing-model")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("TLC_MODEL_FILTER must be", result.stderr)

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
        for model, pattern in (("WorkerEvolution", r"(?:WorkerEvolution|BrokenEvolution|Evolution).*\.cfg"),
                               ("WorkerDeploymentBoundary", r"(?:WorkerDeploymentBoundary|BrokenBoundary|Boundary).*\.cfg"),
                               ("QueueBootstrap", r"(?:BrokenBootstrapHost|BrokenBootstrapEnrollment|BootstrapTrustedAW).*\.cfg")):
            source = (SPEC / f"{model}.tla").read_text()
            if model == "WorkerDeploymentBoundary":
                source += "\n" + (SPEC / "WorkerEvolution.tla").read_text()
            configurations = [path for path in SPEC.glob("*.cfg") if re.fullmatch(pattern, path.name)]
            self.assertTrue(configurations)
            for path in configurations:
                with self.subTest(config=path.stem):
                    registration = re.search(rf"^run_model {path.stem} (.+)$", runner, re.MULTILINE)
                    self.assertIsNotNone(registration)
                    self.assertTrue(registration.group(1).endswith(model))
                    match = re.search(r"^INVARIANTS? (.+)$", path.read_text(), re.MULTILINE)
                    self.assertIsNotNone(match)
                    for invariant in match.group(1).split():
                        self.assertRegex(source, rf"(?m)^{invariant} ==")


if __name__ == "__main__":
    unittest.main()
