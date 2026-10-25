import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import sys
import unittest
from unittest.mock import patch

import check
import check_transport


class TransportRunnerTest(unittest.TestCase):
    def test_untrusted_runtime_is_rejected_before_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            jar = Path(directory) / "tla2tools.jar"
            jar.write_bytes(b"untrusted runtime")
            with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                 patch.object(check_transport.subprocess, "run") as run:
                with self.assertRaisesRegex(RuntimeError, "pinned SHA-256"):
                    check_transport.main()
                run.assert_not_called()

    def test_all_profiles_faults_and_witnesses_run_and_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            jar = Path(directory) / "tla2tools.jar"
            jar.write_bytes(b"local runtime fixture")
            digest = hashlib.sha256(jar.read_bytes()).hexdigest()
            configurations = []
            destinations = []
            modules = []

            def run(command, cwd, **options):
                configurations.append((cwd / "model.cfg").read_text())
                destinations.append(cwd)
                modules.append(command[-1])
                self.assertEqual(options["timeout"], 120)
                self.assertIn("-workers", command)
                self.assertTrue((cwd / command[-1]).is_file())
                return check_transport.subprocess.CompletedProcess(command, 0, "", "")

            with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                 patch.object(check_transport, "JAR_SHA256", digest), \
                 patch.object(check_transport.subprocess, "run", side_effect=run), \
                 patch.object(check_transport, "check_result",
                              return_value={"distinct": 1}) as result, \
                 patch("builtins.print"):
                check_transport.main()
            expected = [None, *check_transport.FAULTS.values(), *check_transport.WITNESSES]
            git_cases = list(check_transport.git_cases())
            self.assertEqual([call.args[2] for call in result.call_args_list],
                             expected * 2 + [case[-1] for case in git_cases])
            for transport in ["patch", "bundle"]:
                self.assertEqual(
                    sum(f'Transport = "{transport}"' in config for config in configurations),
                    len(expected),
                )
            git_configurations = configurations[len(expected) * 2:]
            secure_profiles = {(allow, shallow) for name, allow, shallow, _, _ in git_cases
                               if name.startswith("secure-")}
            self.assertEqual(secure_profiles, {(False, False), (False, True),
                                               (True, False), (True, True)})
            self.assertEqual(modules, ["SafeOutputTransport.tla"] * (len(expected) * 2)
                             + ["SafeOutputGit.tla"] * len(git_cases))
            for config, (name, allow, shallow, fault, invariant) in zip(git_configurations,
                                                                      git_cases):
                self.assertIn(f'Fault = "{fault}"', config)
                self.assertIn(f"AllowFullHistoryFetch = {str(allow).upper()}", config)
                self.assertIn(f"Shallow = {str(shallow).upper()}", config)
                self.assertIn("ComparisonBudget = 4", config)
                self.assertIn("HistoryLimit = 100", config)
                self.assertIn("    ProtectedPublication\n", config)
                if name in check_transport.GIT_WITNESSES:
                    self.assertIn(f"\nINVARIANT {invariant}\n", config)
                else:
                    self.assertNotIn("\nINVARIANT ", config)
            self.assertTrue(all(not destination.exists() for destination in destinations))

    def test_model_selection(self):
        for model in ["transport", "git"]:
            with self.subTest(model=model), tempfile.TemporaryDirectory() as directory:
                jar = Path(directory) / "tla2tools.jar"
                jar.write_bytes(b"local runtime fixture")
                with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                     patch.object(check_transport, "JAR_SHA256",
                                  hashlib.sha256(jar.read_bytes()).hexdigest()), \
                     patch.object(check_transport, "run_case") as run:
                    check_transport.main(model)
                expected = "SafeOutputGit" if model == "git" else "SafeOutputTransport"
                self.assertGreater(run.call_count, 0)
                self.assertTrue(all(call.args[2] == expected for call in run.call_args_list))

    def test_git_tool_failure_is_surfaced_and_cleaned_up(self):
        with tempfile.TemporaryDirectory() as directory:
            jar = Path(directory) / "tla2tools.jar"
            jar.write_bytes(b"local runtime fixture")
            destinations = []

            def run(command, cwd, **options):
                destinations.append(cwd)
                return subprocess.CompletedProcess(command, 1, "Error parsing module", "")

            with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                 patch.object(check_transport, "JAR_SHA256",
                              hashlib.sha256(jar.read_bytes()).hexdigest()), \
                 patch.object(check_transport.subprocess, "run", side_effect=run):
                with self.assertRaisesRegex(RuntimeError, "git-secure.*TLC/tooling failure"):
                    check_transport.main("git")
            self.assertEqual(len(destinations), 1)
            self.assertFalse(destinations[0].exists())


class RuntimePinTest(unittest.TestCase):
    def test_release_pin_matches_workflow_and_documentation(self):
        expected = "d15655eeaff4b3f7b3d65c46b838bc0f35d131b2ef4b1e473689b62c98d4726f"
        self.assertEqual(check.JAR_SHA256, expected)
        workflow = (check.ROOT.parents[1] / ".github/workflows/daily-workflow-security-model.md").read_text()
        self.assertIn("https://github.com/tlaplus/tlaplus/releases/download/v1.8.0/tla2tools.jar",
                      workflow)
        match = re.search(
            r"printf '%s  %s\\n' \\\n\s+([a-f0-9]{64}) \\\n"
            r'\s+"\$tools/tla2tools\.jar" \| sha256sum --check --strict',
            workflow,
        )
        self.assertIsNotNone(match)
        self.assertEqual(match.group(1), expected)
        lock = (check.ROOT.parents[1] / ".github/workflows/daily-workflow-security-model.lock.yml").read_text()
        self.assertIn(expected, lock)
        self.assertIn(expected, (check.ROOT / "README.md").read_text())

    def test_missing_or_mismatched_jar_fails_before_execution(self):
        for present in [False, True]:
            with self.subTest(present=present), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                jar = root / "tla2tools.jar"
                if present:
                    jar.write_bytes(b"not the pinned TLA+ runtime")
                results = root / "results"
                with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                     patch.object(sys, "argv", ["check.py", "--results", str(results)]), \
                     patch.object(check, "run_case") as run_case, \
                     patch("sys.stderr"):
                    with self.assertRaises(SystemExit) as error:
                        check.main()
                self.assertEqual(error.exception.code, 2)
                run_case.assert_not_called()
                self.assertFalse(results.exists())

    def test_matching_jar_reaches_model_checker(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            jar = root / "tla2tools.jar"
            jar.write_bytes(b"local runtime fixture")
            results = root / "results"
            digest = hashlib.sha256(jar.read_bytes()).hexdigest()
            with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                 patch.object(sys, "argv", ["check.py", "--results", str(results)]), \
                 patch.object(check, "JAR_SHA256", digest), \
                 patch.object(check, "run_case", side_effect=RuntimeError("fixture boundary")) as run_case:
                with self.assertRaisesRegex(RuntimeError, "fixture boundary"):
                    check.main()
            run_case.assert_called_once()
            self.assertEqual(run_case.call_args.args[1], jar.resolve())
            self.assertTrue(results.is_dir())


class ResultContractTest(unittest.TestCase):
    def test_success_requires_exhaustion(self):
        text = ("Model checking completed. No error has been found.\n"
                "10 states generated, 8 distinct states found")
        self.assertEqual(check.check_result(0, text), {"generated": 10, "distinct": 8})
        for status, output in [(1, text), (0, "Simulation finished")]:
            with self.assertRaises(RuntimeError):
                check.check_result(status, output)

    def test_counterexample_requires_exact_invariant_and_status(self):
        text = ("Error: Invariant DetectionGate is violated.\n"
                "10 states generated, 8 distinct states found")
        self.assertEqual(check.check_result(12, text, "DetectionGate")["distinct"], 8)
        for status, invariant in [(0, "DetectionGate"), (1, "DetectionGate"),
                                  (12, "TypeOK")]:
            with self.assertRaises(RuntimeError):
                check.check_result(status, text, invariant)

    def test_tool_failure_is_not_a_counterexample(self):
        with self.assertRaises(RuntimeError):
            check.check_result(12, "Error parsing module", "DetectionGate")

    def test_trace_shape(self):
        raw = {"counterexample": {"state": [[1, {"s": {"lastEvent": "Init"}}],
                                            [2, {"s": {"lastEvent": "Execute"}}]]}}
        trace = check.normalize_trace(raw)
        self.assertEqual(trace["states"][1]["s"]["lastEvent"], "Execute")
        for invalid in [{}, {"counterexample": {"state": []}},
                        {"counterexample": {"state": [[1, {}], [2, {}]]}}]:
            with self.assertRaises(RuntimeError):
                check.normalize_trace(invalid)

    def test_concretization_is_explicitly_synthetic(self):
        for fault, invariant in check.MUTATIONS.items():
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                destination = Path(directory)
                check.concretize(fault, invariant, destination, {"states": [{}, {}]})
                report = json.loads((destination / "counterexample.json").read_text())
                self.assertFalse(report["confirmed_product_bug"])
                source = (destination / "source.md").read_text()
                self.assertIn("NOT a confirmed product vulnerability", source)
                if fault == "agent-write":
                    self.assertIn("contents: write", source)
                if fault == "retained-push-token":
                    self.assertIn("create-pull-request:", source)

    def test_examples_match_model_and_include_privileged_git(self):
        examples = json.loads((check.ROOT / "examples.json").read_text())
        self.assertEqual(examples["model_sha256"],
                         hashlib.sha256((check.ROOT / "CompiledWorkflow.tla").read_bytes()).hexdigest())
        self.assertFalse(examples["confirmed_product_bug"])
        push = next(example for example in examples["examples"] if example["case"] == "privileged-push")
        self.assertTrue(any(operation["job"] == "safe_outputs" and operation["op"] == "push"
                            for operation in push["final_state"]["operations"]))
        retained = next(example for example in examples["examples"]
                        if example["case"] == "failure-token-retention")
        self.assertLess(retained["events"].index("Fail:safe_outputs"),
                        retained["events"].index("AppTokenPost"))
        self.assertEqual(retained["final_state"]["status"]["safe_outputs"], "failure")
        self.assertEqual(retained["final_state"]["appTokenPost"], "complete")
        self.assertIn("app-write", retained["final_state"]["live"])

    def test_compiled_corpus_contract(self):
        for outcome in ["success", "violation", "tool-failure", "malformed-policy"]:
            with self.subTest(outcome=outcome), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / "sample.lock.yml").write_text("jobs: {}\n")
                report = {"conforms": outcome == "success",
                          "detection_policy": {"mode": "enabled"}}
                if outcome == "malformed-policy":
                    report["detection_policy"] = None
                output = json.dumps(report)
                code = 0 if outcome == "success" else 1
                if outcome == "tool-failure":
                    output, code = "not-json", 2
                script = root / "verifier"
                script.write_text(f"#!{sys.executable}\nimport sys\nprint({output!r})\nsys.exit({code})\n")
                script.chmod(0o700)
                if outcome == "success":
                    check.check_compiled_workflows(script, root, root)
                else:
                    with self.assertRaises(RuntimeError):
                        check.check_compiled_workflows(script, root, root)
                saved = json.loads((root / "compiled-workflows.json").read_text())
                self.assertEqual(saved["failed"], 0 if outcome == "success" else 1)


class RuntimeCheckoutModelTest(unittest.TestCase):
    def test_controls_are_declared_and_check_configured_invariants(self):
        model = (check.ROOT / "RuntimeRepositoryCheckout.tla").read_text()
        config = (check.ROOT / "RuntimeRepositoryCheckout.cfg").read_text()
        for fault, invariant in check.RUNTIME_CHECKOUT_MUTATIONS.items():
            with self.subTest(fault=fault):
                self.assertIn(f'"{fault}"', model)
                self.assertRegex(model, rf"(?m)^{invariant}\s*==")
                self.assertRegex(config, rf"(?m)^\s+{invariant}$")
        for invariant in check.RUNTIME_CHECKOUT_WITNESSES.values():
            self.assertRegex(model, rf"(?m)^{invariant}\s*==")
            self.assertNotRegex(config, rf"(?m)^\s+{invariant}$")

    def test_runtime_counterexamples_are_separate_from_source_mutations(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            raw = {"counterexample": {"state": [
                [1, {"s": {"lastEvent": "Init"}}],
                [2, {"s": {"lastEvent": "Compile"}}],
            ]}}

            def tlc(command, **kwargs):
                Path(command[command.index("-dumpTrace") + 2]).write_text(json.dumps(raw))
                text = ("Invariant PolicyProvenance is violated.\n"
                        "10 states generated, 8 distinct states found")
                return subprocess.CompletedProcess(command, 12, text, "")

            with patch.object(check.subprocess, "run", side_effect=tlc):
                report = check.run_case("java", root / "fixture.jar", root, "runtime-policy",
                                        fault="untrusted-policy", invariant="PolicyProvenance",
                                        model="RuntimeRepositoryCheckout")
            saved = root / "runtime-policy"
            self.assertTrue((saved / "RuntimeRepositoryCheckout.tla").is_file())
            self.assertFalse((saved / "CompiledWorkflow.tla").exists())
            self.assertFalse((saved / "source.md").exists())
            self.assertIn('Fault = "untrusted-policy"', (saved / "model.cfg").read_text())
            classification = json.loads((saved / "counterexample.json").read_text())
            self.assertFalse(classification["confirmed_product_bug"])
            self.assertEqual(classification["model"], "RuntimeRepositoryCheckout")
            self.assertEqual(report["model"], "RuntimeRepositoryCheckout")
            self.assertNotIn("checkout_mode", report)

    def test_main_includes_additional_model_baselines_controls_and_witnesses(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            jar = root / "fixture.jar"
            jar.write_bytes(b"runtime fixture")
            results = root / "results"

            def run_case(*args, model="CompiledWorkflow", **kwargs):
                return {"case": args[3], "model": model,
                        "model_sha256": hashlib.sha256(
                            (check.ROOT / f"{model}.tla").read_bytes()).hexdigest()}

            with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                 patch.object(sys, "argv", ["check.py", "--results", str(results)]), \
                 patch.object(check, "JAR_SHA256", hashlib.sha256(jar.read_bytes()).hexdigest()), \
                 patch.object(check, "run_case", side_effect=run_case), patch("sys.stdout"):
                check.main()
            report = json.loads((results / "results.json").read_text())
            self.assertEqual(report["additional_models"],
                             ["RuntimeRepositoryCheckout", "StatusOnlyJob", "MCPCompile",
                              "ActivationAdmission", "AdmissionConclusion"])
            cases = {case["case"] for case in report["cases"]
                     if case["model"] == "RuntimeRepositoryCheckout"}
            self.assertEqual(cases, {"secure-runtime-checkout"} |
                             {f"runtime-checkout-{fault}" for fault in check.RUNTIME_CHECKOUT_MUTATIONS} |
                             {f"runtime-checkout-{name}" for name in check.RUNTIME_CHECKOUT_WITNESSES})
            mcp_cases = {case["case"] for case in report["cases"]
                         if case["model"] == "MCPCompile"}
            self.assertEqual(mcp_cases, {"secure-mcp-compile"} |
                             {f"mcp-compile-{fault}" for fault in check.MCP_COMPILE_MUTATIONS} |
                             {f"mcp-compile-{name}" for name in check.MCP_COMPILE_WITNESSES})
            self.assertEqual(sum(case["model"] == "ActivationAdmission"
                                 for case in report["cases"]),
                             4 + len(check.ADMISSION_MUTATIONS) + len(check.ADMISSION_WITNESSES))
            self.assertEqual(sum(case["model"] == "AdmissionConclusion"
                                 for case in report["cases"]),
                             2 + len(check.CONCLUSION_MUTATIONS) + len(check.CONCLUSION_WITNESSES))


class MCPCompileModelTest(unittest.TestCase):
    def test_controls_are_declared_and_check_configured_invariants(self):
        model = (check.ROOT / "MCPCompile.tla").read_text()
        config = (check.ROOT / "MCPCompile.cfg").read_text()
        for fault, invariant in check.MCP_COMPILE_MUTATIONS.items():
            with self.subTest(fault=fault):
                self.assertIn(f'"{fault}"', model)
                self.assertRegex(model, rf"(?m)^{invariant}\s*==")
                self.assertRegex(config, rf"(?m)^\s+{invariant}$")
        for invariant in check.MCP_COMPILE_WITNESSES.values():
            self.assertRegex(model, rf"(?m)^{invariant}\s*==")
            self.assertNotRegex(config, rf"(?m)^\s+{invariant}$")

    def test_runner_checks_baseline_controls_and_witnesses(self):
        with patch.object(check, "run_case", return_value={}) as run_case:
            cases = check.check_mcp_compile("java", Path("fixture.jar"), Path("results"))
        expected = {"secure-mcp-compile": ("none", None, False)}
        expected.update({f"mcp-compile-{fault}": (fault, invariant, False)
                         for fault, invariant in check.MCP_COMPILE_MUTATIONS.items()})
        expected.update({f"mcp-compile-{name}": ("none", invariant, True)
                         for name, invariant in check.MCP_COMPILE_WITNESSES.items()})
        observed = {}
        for call in run_case.call_args_list:
            self.assertEqual(call.kwargs["model"], "MCPCompile")
            observed[call.args[3]] = (
                call.args[4] if len(call.args) > 4 else "none",
                call.kwargs.get("invariant"), call.kwargs.get("witness", False))
        self.assertEqual(observed, expected)
        self.assertEqual(len(cases), len(expected))


class AdmissionModelTest(unittest.TestCase):
    def test_controls_and_witnesses_are_declared(self):
        model = (check.ROOT / "ActivationAdmission.tla").read_text()
        config = (check.ROOT / "ActivationAdmission.cfg").read_text()
        for fault, invariant in check.ADMISSION_MUTATIONS.items():
            with self.subTest(fault=fault):
                self.assertIn(f'"{fault}"', model)
                self.assertRegex(model, rf"(?m)^{invariant}\s*==")
                self.assertRegex(config, rf"(?m)^\s+{invariant}$")
        for invariant in check.ADMISSION_WITNESSES.values():
            self.assertRegex(model, rf"(?m)^{invariant}\s*==")
            self.assertNotRegex(config, rf"(?m)^\s+{invariant}$")
        self.assertIn("PROPERTY Termination", config)

    def test_runner_checks_profiles_controls_and_witnesses(self):
        with patch.object(check, "run_case", return_value={}) as run_case:
            cases = check.check_activation_admission("java", Path("jar"), Path("results"))
        calls = run_case.call_args_list
        self.assertEqual({(call.kwargs["outage_policy"], call.kwargs["otel_enabled"])
                          for call in calls[:4]},
                         {("ignore", True), ("ignore", False), ("block", True), ("block", False)})
        controls = calls[4:4 + len(check.ADMISSION_MUTATIONS)]
        self.assertEqual({call.args[4]: call.kwargs["invariant"] for call in controls},
                         check.ADMISSION_MUTATIONS)
        for call in controls:
            for key, value in check.ADMISSION_MUTATION_SETTINGS.get(call.args[4], {}).items():
                self.assertEqual(call.kwargs[key], value)
        witnesses = calls[4 + len(check.ADMISSION_MUTATIONS):]
        self.assertEqual({call.args[3]: call.kwargs["invariant"] for call in witnesses},
                         {f"admission-{name}": invariant
                          for name, invariant in check.ADMISSION_WITNESSES.items()})
        self.assertTrue(all(call.kwargs["witness"] for call in witnesses))
        self.assertTrue(all(call.kwargs["model"] == "ActivationAdmission" for call in calls))
        self.assertEqual(len(cases), 4 + len(controls) + len(witnesses))

    def test_profile_reaches_tlc_and_report(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)

            def tlc(command, cwd, **options):
                config = (cwd / "model.cfg").read_text()
                self.assertEqual(command[-1], "ActivationAdmission.tla")
                self.assertIn('OutagePolicy = "block"', config)
                self.assertIn("OTelEnabled = FALSE", config)
                self.assertIn("PROPERTY Termination", config)
                return subprocess.CompletedProcess(
                    command, 0, "Model checking completed. No error has been found.\n"
                    "10 states generated, 8 distinct states found", "")

            with patch.object(check.subprocess, "run", side_effect=tlc), patch("sys.stdout"):
                report = check.run_case("java", root / "jar", root, "profile",
                                        model="ActivationAdmission", outage_policy="block",
                                        otel_enabled=False)
            self.assertEqual(report["outage_policy"], "block")
            self.assertFalse(report["otel_enabled"])
            self.assertNotIn("profile", report)

    def test_focused_main_skips_unrelated_models(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            jar = root / "fixture.jar"
            jar.write_bytes(b"runtime fixture")
            results = root / "results"
            case = {"model": "ActivationAdmission", "model_sha256": hashlib.sha256(
                (check.ROOT / "ActivationAdmission.tla").read_bytes()).hexdigest()}
            with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                 patch.object(sys, "argv", ["check.py", "--model", "admission",
                                           "--results", str(results)]), \
                 patch.object(check, "JAR_SHA256", hashlib.sha256(jar.read_bytes()).hexdigest()), \
                 patch.object(check, "check_workflow_models") as other_models, \
                 patch.object(check, "check_activation_admission", return_value=[case]), \
                 patch.object(check, "check_admission_conclusion", return_value=[]) as conclusion, \
                 patch("sys.stdout"):
                check.main()
            other_models.assert_not_called()
            conclusion.assert_called_once()
            report = json.loads((results / "results.json").read_text())
            self.assertEqual(report["model"], "ActivationAdmission")
            self.assertEqual(report["additional_models"], [])

    def test_focused_mode_rejects_unrelated_options(self):
        for options in [["--write-examples"], ["--compiler", "binary"],
                        ["--verifier", "binary", "--compiled-workflows", "workflows"]]:
            with self.subTest(options=options), \
                 patch.object(sys, "argv", ["check.py", "--model", "admission", *options]), \
                 patch.object(check, "run_case") as run, patch("sys.stderr"):
                with self.assertRaises(SystemExit) as error:
                    check.main()
                self.assertEqual(error.exception.code, 2)
                run.assert_not_called()


class ConclusionModelTest(unittest.TestCase):
    def test_controls_and_witnesses_are_declared(self):
        model = (check.ROOT / "AdmissionConclusion.tla").read_text()
        config = (check.ROOT / "AdmissionConclusion.cfg").read_text()
        for fault, invariant in check.CONCLUSION_MUTATIONS.items():
            with self.subTest(fault=fault):
                self.assertIn(f'"{fault}"', model)
                self.assertRegex(model, rf"(?m)^{invariant}\s*==")
                self.assertRegex(config, rf"(?m)^\s+{invariant}$")
        for invariant in check.CONCLUSION_WITNESSES.values():
            self.assertRegex(model, rf"(?m)^{invariant}\s*==")
            self.assertNotRegex(config, rf"(?m)^\s+{invariant}$")
        self.assertIn("PROPERTY Termination", config)

    def test_runner_checks_policies_controls_and_witnesses(self):
        with patch.object(check, "run_case", return_value={}) as run:
            cases = check.check_admission_conclusion("java", Path("jar"), Path("results"))
        calls = run.call_args_list
        self.assertEqual({call.kwargs["outage_policy"] for call in calls[:2]}, {"ignore", "block"})
        controls = calls[2:2 + len(check.CONCLUSION_MUTATIONS)]
        self.assertEqual({call.args[4]: call.kwargs["invariant"] for call in controls},
                         check.CONCLUSION_MUTATIONS)
        best_effort = next(call for call in controls if call.args[4] == "delivery-fails-workflow")
        self.assertEqual(best_effort.kwargs["outage_policy"], "block")
        witnesses = calls[2 + len(check.CONCLUSION_MUTATIONS):]
        self.assertEqual({call.args[3]: call.kwargs["invariant"] for call in witnesses},
                         {f"conclusion-{name}": invariant
                          for name, invariant in check.CONCLUSION_WITNESSES.items()})
        self.assertTrue(all(call.kwargs["witness"] for call in witnesses))
        self.assertTrue(all(call.kwargs["model"] == "AdmissionConclusion" for call in calls))
        self.assertEqual(len(cases), 2 + len(controls) + len(witnesses))

    def test_conclusion_only_mode_skips_start_time_admission(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            jar = root / "fixture.jar"
            jar.write_bytes(b"fixture")
            case = {"model": "AdmissionConclusion", "model_sha256": hashlib.sha256(
                (check.ROOT / "AdmissionConclusion.tla").read_bytes()).hexdigest()}
            with patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}), \
                 patch.object(sys, "argv", ["check.py", "--model", "conclusion",
                                           "--results", str(root / "results")]), \
                 patch.object(check, "JAR_SHA256", hashlib.sha256(jar.read_bytes()).hexdigest()), \
                 patch.object(check, "check_workflow_models") as others, \
                 patch.object(check, "check_activation_admission") as activation, \
                 patch.object(check, "check_admission_conclusion", return_value=[case]), \
                 patch("sys.stdout"):
                check.main()
            others.assert_not_called()
            activation.assert_not_called()
            report = json.loads((root / "results" / "results.json").read_text())
            self.assertEqual(report["model"], "AdmissionConclusion")
            self.assertEqual(report["additional_models"], [])


if __name__ == "__main__":
    unittest.main()
