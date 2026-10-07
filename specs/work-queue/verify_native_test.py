#!/usr/bin/env python3
"""Dependency-free checks for the independent driver (python3 -B ...)."""

import importlib.util
import io
import json
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

spec = importlib.util.spec_from_file_location("verify_native", Path(__file__).with_name("verify_native.py"))
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)
fixture_spec = importlib.util.spec_from_file_location("verify_native_fixtures", Path(__file__).with_name("verify_native_fixtures.py"))
fixtures = importlib.util.module_from_spec(fixture_spec)
fixture_spec.loader.exec_module(fixtures)


class IndependentDriverTests(unittest.TestCase):
    def test_conformance_refuses_missing_positive_before_native_execution(self):
        path = verify.ROOT / "actions/setup/js/work_queue_worker_child_fixtures.json"
        original = path.read_bytes()
        original_read = Path.read_bytes
        for scenario in ("no-positive", "substituted-positive"):
            with self.subTest(scenario=scenario):
                source = json.loads(original)
                for case in source["cases"]:
                    case["valid"] = False
                    if scenario == "substituted-positive" and case["name"] == "completed-parent-inherited-entitlement":
                        case["valid"] = True
                        case["name"] = "unrelated-completed-positive"
                if scenario == "substituted-positive":
                    source["cases"].append({"name": "second-unrelated-completed-positive", "valid": True, "expected": {}})
                    self.assertEqual(sum(case["valid"] for case in source["cases"]), 2)

                def read(candidate):
                    return verify.canonical(source).encode() if candidate == path else original_read(candidate)

                with mock.patch.object(Path, "read_bytes", read), mock.patch.object(verify, "Native") as native:
                    with self.assertRaisesRegex(AssertionError, "^required pending parent positive$"):
                        fixtures.conformance(verify.SPEC / "fixtures", {}, {}, Path("unused"), verify)
                    native.assert_not_called()

    def test_worker_child_required_scenarios_allow_additions_not_substitution(self):
        source = json.loads((verify.ROOT / "actions/setup/js/work_queue_worker_child_fixtures.json").read_bytes())
        original = verify.canonical(source)
        self.assertEqual(fixtures.validate_worker_child_cases(source), source["cases"])
        added = {"name": "additional-independent-positive", "valid": True, "expected": {}}
        source["cases"].append(added)
        self.assertIs(fixtures.validate_worker_child_cases(source)[-1], added)
        self.assertEqual(verify.canonical({**source, "cases": source["cases"][:-1]}), original)
        for scenario in ("substitute", "missing", "duplicate", "missing-negative", "positive-negative",
                         "nonboolean", "missing-expected", "wrong-terminal-code", "missing-terminal-code",
                         "terminal-acceptance"):
            with self.subTest(scenario=scenario):
                altered = json.loads(verify.canonical(source))
                verified = next(case for case in altered["cases"] if case["name"] == "verified-parent-inherited-entitlement")
                pending = next(case for case in altered["cases"] if case["name"] == "completed-parent-inherited-entitlement")
                negative = next(case for case in altered["cases"] if case["name"] == "foreign-native-run")
                if scenario == "substitute":
                    pending["valid"] = False
                elif scenario == "missing":
                    altered["cases"].remove(verified)
                elif scenario == "duplicate":
                    altered["cases"].append(dict(verified))
                elif scenario == "missing-negative":
                    altered["cases"].remove(negative)
                elif scenario == "positive-negative":
                    negative["valid"], negative["expected"] = True, {}
                elif scenario == "nonboolean":
                    altered["cases"][-1]["valid"] = 1
                elif scenario == "wrong-terminal-code":
                    verified["error_code"] = "admission_unauthorized"
                elif scenario == "missing-terminal-code":
                    del verified["error_code"]
                elif scenario == "terminal-acceptance":
                    verified["valid"], verified["expected"] = True, {}
                else:
                    del pending["expected"]
                with self.assertRaises(AssertionError):
                    fixtures.validate_worker_child_cases(altered)

    def test_recovery_headroom_audit_requires_exact_exported_integer_measurements(self):
        outputs = [{"headroom_bytes": value} for value in (0, 55296, 0)]
        with mock.patch.object(verify, "Native") as native:
            engine = native.return_value.__enter__.return_value
            engine.call.side_effect = outputs
            rows, failures = verify.recovery_headroom_audit({"go": ["native"]}, {})
            self.assertEqual(failures, [])
            self.assertEqual([row["output"] for row in rows], outputs)
            self.assertEqual([row["expected_headroom_bytes"] for row in rows], [0, 55296, 0])
            requests = [call.args[0] for call in engine.call.call_args_list]
            self.assertTrue(all(value["action"] == "recovery_headroom" for value in requests))
            self.assertEqual(len(requests[0]["data"].splitlines()), 1)
            self.assertEqual(len(requests[1]["data"].splitlines()), 2)
            self.assertEqual(len(requests[2]["data"].splitlines()), 3)
        for invalid in ({"headroom_bytes": True}, {"headroom_bytes": 55296.0},
                        {"headroom_bytes": None}, {"headroom_bytes": -1},
                        {"headroom_bytes": 0}, {"error": "ledger_limit: refused"},
                        {"headroom_bytes": 55296, "unexpected": 1}):
            with self.subTest(invalid=invalid), mock.patch.object(verify, "Native") as native:
                native.return_value.__enter__.return_value.call.side_effect = [outputs[0], invalid, outputs[2]]
                rows, failures = verify.recovery_headroom_audit({"go": ["native"]}, {})
                self.assertEqual(len(failures), 1)
                self.assertEqual(rows[1]["output"], invalid)

    def test_native_shutdown_timeout_kills_and_closes_owned_process(self):
        native = verify.Native(["test-native"], {})
        native.process = mock.Mock(stdin=io.StringIO(), stdout=io.StringIO(), stderr=io.StringIO())
        native.process.wait.side_effect = [subprocess.TimeoutExpired("test-native", 30), 0]
        with self.assertRaisesRegex(RuntimeError, "did not stop within 30 seconds"):
            native.__exit__(None, None, None)
        native.process.kill.assert_called_once_with()
        self.assertTrue(native.process.stdin.closed)
        self.assertTrue(native.process.stdout.closed)
        self.assertTrue(native.process.stderr.closed)

    def test_native_build_cleanup_retains_reports_on_success_and_failure(self):
        args = SimpleNamespace(benchmark_only=True, benchmark=True, sizes=[1], iterations=1)
        for scenario in ("success", "build-failure", "benchmark-failure"):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as temporary:
                evidence = Path(temporary)

                def build(command, **kwargs):
                    Path(command[command.index("-o") + 1]).write_bytes(b"temporary executable")
                    if scenario == "build-failure":
                        raise subprocess.CalledProcessError(1, command)

                def benchmark(*unused):
                    (evidence / "benchmark.json").write_text('{"status":"passed"}\n')
                    if scenario == "benchmark-failure":
                        raise RuntimeError("expected benchmark failure")
                    return {"status": "passed"}

                with mock.patch.object(verify, "engine_source_hashes", return_value={"test": "hash"}), \
                        mock.patch.object(verify.subprocess, "run", side_effect=build), \
                        mock.patch.object(verify, "benchmark", side_effect=benchmark):
                    if scenario == "success":
                        self.assertEqual(verify.run_native_gates(args, evidence), {"benchmark": "passed"})
                    else:
                        with self.assertRaises((RuntimeError, subprocess.CalledProcessError)):
                            verify.run_native_gates(args, evidence)
                    self.assertFalse(any(evidence.glob("native-build-*")))
                    self.assertEqual(json.loads((evidence / "sources.json").read_text())["status"], "stable")
                    if scenario != "build-failure":
                        self.assertEqual(json.loads((evidence / "benchmark.json").read_text())["status"], "passed")

    def test_existing_evidence_cannot_be_overwritten(self):
        for filename in ("conformance.json", "run.log"):
            with self.subTest(filename=filename), tempfile.TemporaryDirectory() as temporary:
                evidence = Path(temporary)
                report = evidence / filename
                report.write_text('{"status":"historical"}\n')
                with mock.patch.object(sys, "argv", ["verify_native.py", "--evidence-dir", str(evidence)]), \
                        mock.patch.object(verify, "run_native_gates") as gates:
                    with self.assertRaisesRegex(SystemExit, "existing evidence is preserved"):
                        verify.main()
                    gates.assert_not_called()
                self.assertEqual(report.read_text(), '{"status":"historical"}\n')

    def test_empty_caller_run_log_does_not_block_fresh_capture(self):
        with tempfile.TemporaryDirectory() as temporary:
            evidence = Path(temporary)
            log = evidence / "run.log"
            log.touch()
            with mock.patch.object(sys, "argv", ["verify_native.py", "--evidence-dir", str(evidence)]), \
                    mock.patch.object(verify, "run_native_gates", return_value={"conformance": "passed"}) as gates, \
                    mock.patch.object(sys, "stdout", io.StringIO()):
                self.assertEqual(verify.main(), 0)
                gates.assert_called_once()
            self.assertEqual(log.read_bytes(), b"")

    def test_benchmark_resources_require_real_finite_measurements(self):
        metrics = {"rss_peak_bytes": 1, **{key: 0.5 for key in (
            "parse_ms", "cold_replay_ms", "selection_ms", "packing_ms", "serialization_ms")}}
        verify.verify_benchmark_resources({"metrics": metrics})
        for key, invalid in (
            ("rss_peak_bytes", None), ("rss_peak_bytes", 0), ("rss_peak_bytes", -1),
            ("rss_peak_bytes", True), ("rss_peak_bytes", "1"),
            ("parse_ms", -1), ("parse_ms", float("nan")), ("parse_ms", float("inf")),
            ("cold_replay_ms", True), ("packing_ms", None), ("serialization_ms", "0.5"),
        ):
            with self.subTest(key=key, invalid=invalid), self.assertRaises(AssertionError):
                verify.verify_benchmark_resources({"metrics": {**metrics, key: invalid}})

    def test_typed_parser_controls_preserve_literal_answers(self):
        cases = fixtures.typed_canonical_cases()
        self.assertEqual(len(cases), 7)
        self.assertEqual([case["expected"] for case in cases[:3]], ["1", "1", '{"x":2}'])
        self.assertEqual(cases[3]["input"], '{"x":"\\ud800","x":"valid"}')
        self.assertEqual(cases[3]["error_code"], "invalid_unicode")
        for case in cases:
            with self.subTest(case=case["name"]):
                output = {"error": "invalid_unicode: original string"} if case.get("reject") else {
                    "canonical": case["expected"]}
                fixtures.verify_canonical_output(case, output)
                invalid = {"error": "codec_invalid: source"} if case.get("reject") else {"canonical": "false"}
                with self.assertRaises(AssertionError):
                    fixtures.verify_canonical_output(case, invalid)

    def test_constructed_number_fixture_answers_and_typed_transport_are_preserved(self):
        source = json.loads((verify.SPEC / "fixtures/canonical-typed.json").read_bytes())
        original = verify.canonical(source)
        cases = fixtures.shared_constructed_number_cases(source)
        self.assertEqual(len(cases), len(source["cases"]))
        self.assertEqual({case["container"] for case in cases}, {"root", "object", "array", "nested"})
        self.assertTrue({"NaN", "+Infinity", "-Infinity", "-0"}.issubset({case["literal"] for case in cases}))
        for raw, case in zip(source["cases"], cases):
            with self.subTest(case=case["name"]):
                self.assertEqual(case["literal"], raw["literal"])
                self.assertEqual(case["container"], raw["container"])
                if "expected" in raw:
                    self.assertEqual(case["expected"], raw["expected"])
                    fixtures.verify_canonical_output(case, {"canonical": raw["expected"]})
                else:
                    self.assertEqual(case["error_code"], raw["code"])
                    fixtures.verify_canonical_output(case, {"error": f"{raw['code']}: actual typed guard"})
                    with self.assertRaises(AssertionError):
                        fixtures.verify_canonical_output(case, {"canonical": "null"})
        self.assertEqual(verify.canonical(source), original)
        for scenario in ("version", "api", "container", "duplicate", "both-answers"):
            with self.subTest(scenario=scenario):
                altered = json.loads(original)
                if scenario == "version":
                    altered["version"] = 3
                elif scenario == "api":
                    altered["go_api"] = "json.Marshal"
                elif scenario == "container":
                    altered["cases"][0]["container"] = "foreign"
                elif scenario == "duplicate":
                    altered["cases"].append(dict(altered["cases"][0]))
                else:
                    altered["cases"][0]["code"] = "noncanonical_number"
                with self.assertRaises(AssertionError):
                    fixtures.shared_constructed_number_cases(altered)

    def test_shared_typed_numeric_oracles_preserve_negative_zero_and_nested_payload(self):
        codec = {
            "valid": [{"name": "nested", "input": '{"z":["1.25",null,0,{"n":"-0"}]}',
                       "expected": '{"z":["1.25",null,0,{"n":"-0"}]}'}],
            "typed_number_rejections": [
                {"name": "negative-zero", "input": "-0", "code": "noncanonical_number"},
                {"name": "fraction", "input": "1.25", "code": "noncanonical_number"},
            ],
            "error_codes": [
                {"name": "surrogate-value", "input": '{"x":"\\ud800"}', "code": "invalid_unicode"},
                {"name": "surrogate-key", "input": '{"\\udc00":1}', "code": "invalid_unicode"},
                {"name": "duplicate", "input": '{"x":1,"x":2}', "code": "duplicate_key"},
            ],
        }
        original = verify.canonical(codec)
        cases = fixtures.shared_typed_canonical_cases(codec)
        self.assertEqual(len(cases), 7)
        self.assertEqual(cases[0]["input"], codec["valid"][0]["input"])
        self.assertEqual(cases[0]["expected"], codec["valid"][0]["expected"])
        self.assertEqual([case["input"] for case in cases[1:5]], [
            "-0", '{"payload":{"nested":[-0]}}',
            "1.25", '{"payload":{"nested":[1.25]}}',
        ])
        self.assertEqual([case["input"] for case in cases[5:]],
                         [case["input"] for case in codec["error_codes"][:2]])
        self.assertTrue(all(case["error_code"] == "invalid_unicode" for case in cases[5:]))
        for index, case in enumerate(cases[1:], 1):
            self.assertEqual(case["error_code"], "noncanonical_number" if index < 5 else "invalid_unicode")
            fixtures.verify_canonical_output(case, {"error": f"{case['error_code']}: typed value"})
            for output in ({"canonical": "0"}, {"error": "codec_invalid: typed value"}):
                with self.subTest(case=case["name"], output=output), self.assertRaises(AssertionError):
                    fixtures.verify_canonical_output(case, output)
        self.assertEqual(verify.canonical(codec), original)

    def test_worker_child_provenance_uses_public_request_records(self):
        producer = {"role": "producer", "principal": "11", "repository": "owner/repo"}
        worker = {"role": "worker", "principal": "22", "repository": "owner/repo",
                  "run_id": "202", "run_attempt": 1, "workflow": ".github/workflows/worker.lock.yml",
                  "dispatch_id": "original-dispatch", "claim_handle": "h1"}
        records = [{"request": {"id": "root"}, "actor": producer},
                   {"request": {"id": "child"}, "actor": worker}]
        case = {"transactions": records, "canonical": verify.ledger_text(records),
                "expected": {"worker_principal": "22", "producer_principal": "11",
                             "child_work_id": "child", "parent_work_id": "parent",
                             "pool": "default", "priority": 1, "fairness_key": "tenant"}}
        output = {"canonical_ledger": case["canonical"], "projection": {
            "works": {"child": {"pool": "default", "priority": 1, "fairness_key": "tenant", "state": "available"},
                      "parent": {"position": {"commit": 0}, "state": "completed", "barrier": "pending"}},
            "requests": {"child": {"actor": dict(worker)}, "root": {"actor": dict(producer)}},
            "policy": {"producers": {"11": {}}},
        }}
        original = verify.canonical(output)
        fixtures.verify_worker_child_output(case, output)
        self.assertNotIn("transactions", output["projection"])
        self.assertEqual(verify.canonical(output), original)
        for scenario in ("worker-role", "worker-principal", "producer-principal",
                         "producer-role", "worker-run", "worker-attempt", "worker-workflow", "worker-dispatch",
                         "worker-handle", "worker-repository", "actor-origin-spoof",
                         "child-pool", "child-priority", "child-key", "root-entitlement", "canonical-bytes"):
            with self.subTest(scenario=scenario):
                altered = json.loads(json.dumps(output))
                projection = altered["projection"]
                if scenario == "worker-role":
                    projection["requests"]["child"]["actor"]["role"] = "producer"
                elif scenario == "worker-principal":
                    projection["requests"]["child"]["actor"]["principal"] = "11"
                elif scenario == "producer-principal":
                    projection["requests"]["root"]["actor"]["principal"] = "22"
                elif scenario == "producer-role":
                    projection["requests"]["root"]["actor"]["role"] = "administrator"
                elif scenario == "actor-origin-spoof":
                    projection["requests"]["child"]["actor"]["logical_origin"] = producer
                elif scenario.startswith("worker-"):
                    field, value = {
                        "worker-run": ("run_id", "203"), "worker-attempt": ("run_attempt", 2),
                        "worker-workflow": ("workflow", ".github/workflows/foreign.lock.yml"),
                        "worker-dispatch": ("dispatch_id", "sibling-dispatch"),
                        "worker-handle": ("claim_handle", "h2"), "worker-repository": ("repository", "foreign/repo"),
                    }[scenario]
                    projection["requests"]["child"]["actor"][field] = value
                elif scenario == "root-entitlement":
                    projection["policy"]["producers"]["22"] = {}
                elif scenario == "canonical-bytes":
                    altered["canonical_ledger"] += "\n"
                else:
                    field, value = {"child-pool": ("pool", "foreign"), "child-priority": ("priority", 5),
                                    "child-key": ("fairness_key", "")}[scenario]
                    projection["works"]["child"][field] = value
                with self.assertRaises(AssertionError):
                    fixtures.verify_worker_child_output(case, altered)
        for name in ("completed-parent-inherited-entitlement",
                     "completed-parent-cancelled-sibling-unit-weight-entitlement"):
            with self.subTest(name=name):
                named = {**case, "name": name}
                altered = json.loads(original)
                fixtures.verify_worker_child_output(named, altered)
                for field, value in (("state", "claimed"), ("barrier", "failed"),
                                     ("barrier", "verified")):
                    invalid = json.loads(verify.canonical(altered))
                    invalid["projection"]["works"]["parent"][field] = value
                    with self.assertRaises(AssertionError):
                        fixtures.verify_worker_child_output(named, invalid)

    def test_worker_child_mixed_sibling_output_checks_all_independent_expectations(self):
        producer = {"role": "producer", "principal": "11"}
        worker = {"role": "worker", "principal": "22", "dispatch_id": "dispatch"}
        records = [{"request": {"id": "root"}, "actor": producer},
                   {"request": {"id": "child"}, "actor": worker}]
        case = {
            "name": "completed-parent-cancelled-sibling-unit-weight-entitlement",
            "transactions": records, "canonical": verify.ledger_text(records),
            "expected": {"worker_principal": "22", "producer_principal": "11", "child_work_id": "child",
                         "parent_work_id": "parent", "pool": "default", "priority": 1, "fairness_key": "tenant",
                         "accounting_weight": 1, "bound_claims": 2, "cancelled_sibling_work_id": "sibling"},
        }
        output = {"canonical_ledger": case["canonical"], "projection": {
            "works": {"child": {"pool": "default", "priority": 1, "fairness_key": "tenant", "state": "available"},
                      "parent": {"position": {"commit": 0}, "state": "completed", "barrier": "pending"},
                      "sibling": {"state": "available", "retry_not_before": 34400}},
            "requests": {"child": {"actor": worker}, "root": {"actor": producer}},
            "policy": {"producers": {"11": {}}, "accounting_weights": {"tenant": 1}},
            "dispatches": {"dispatch": {"claims": [{"work_id": "parent", "claim_id": "c1"},
                                                 {"work_id": "sibling", "claim_id": "c2"}],
                                        "state": "bound", "released": False}},
            "claims": {"c1": {"state": "completed"}, "c2": {"state": "cancelled"}},
        }}
        original = verify.canonical(output)
        fixtures.verify_worker_child_output(case, output)
        for scenario in ("weight", "boolean-weight", "size", "assignment", "unbound", "released", "work-count",
                         "parent", "retry", "sibling", "parent-claim", "sibling-claim"):
            with self.subTest(scenario=scenario):
                altered = json.loads(original)
                projection = altered["projection"]
                if scenario == "weight":
                    projection["policy"]["accounting_weights"]["tenant"] = 3
                elif scenario == "boolean-weight":
                    projection["policy"]["accounting_weights"]["tenant"] = True
                elif scenario == "size":
                    projection["dispatches"]["dispatch"]["claims"].pop()
                elif scenario == "assignment":
                    projection["dispatches"]["dispatch"]["claims"].reverse()
                elif scenario == "unbound":
                    projection["dispatches"]["dispatch"]["state"] = "started"
                elif scenario == "released":
                    projection["dispatches"]["dispatch"]["released"] = True
                elif scenario == "work-count":
                    projection["works"]["extra"] = {}
                elif scenario == "parent":
                    projection["works"]["parent"]["barrier"] = "verified"
                elif scenario == "retry":
                    projection["works"]["sibling"]["retry_not_before"] = 0
                elif scenario == "sibling":
                    projection["works"]["sibling"]["state"] = "completed"
                else:
                    projection["claims"]["c1" if scenario == "parent-claim" else "c2"]["state"] = "open"
                with self.assertRaises(AssertionError):
                    fixtures.verify_worker_child_output(case, altered)
        self.assertEqual(verify.canonical(output), original)

    def test_worker_child_fixtures_retain_literal_authority_and_valid_fingerprints(self):
        path = "actions/setup/js/work_queue_worker_child_fixtures.json"
        source = json.loads((verify.ROOT / path).read_bytes())
        fence = verify.engine_source_hashes()
        self.assertIn(path, fence)
        self.assertIn("actions/setup/js/work_queue_worker_child_fixture_generator.cjs", fence)
        for case in source["cases"]:
            with self.subTest(case=case["name"]):
                self.assertEqual(verify.ledger_text(case["transactions"]), case["canonical"])
                for record in case["transactions"]:
                    self.assertEqual(record["request"]["fingerprint"], verify.digest({
                        "actor": record["actor"], "kind": record["request"]["kind"],
                        "parameters": record["request"]["parameters"],
                    }))
                if case["valid"]:
                    record = case["transactions"][-1]
                    self.assertEqual(record["actor"]["principal"], "22")
                    self.assertEqual(record["actor"]["role"], "worker")
                    self.assertEqual(case["transactions"][1]["actor"]["principal"], "11")
                    self.assertNotIn("22", case["transactions"][0]["operations"][0]["policy"]["producers"])
                    self.assertEqual(record["operations"][0]["priority"], 1)
                    self.assertEqual(record["operations"][0]["fairness_key"], "tenant")
        fixtures.validate_worker_child_cases(source)

    def test_integer_and_unicode_canonical_oracle(self):
        self.assertEqual(verify.canonical({"\U00010000": 9007199254740991, "\ue000": "9007199254740992"}),
                         '{"\ue000":"9007199254740992","\U00010000":9007199254740991}')
        self.assertEqual(verify.canonical({"a": False, "b": None, "c": "\n"}), '{"a":false,"b":null,"c":"\\n"}')

    def test_shared_canonical_error_codes_preserve_literal_answers(self):
        codec = {
            "valid": [{"name": "literal", "input": '{"a":1}', "expected": '{"a":1}'}],
            "invalid": ['{} {}', {"input": '{"a":-0}', "error_code": "noncanonical_number"}],
            "error_codes": [{"name": "surrogate", "input": '{"a":"\\ud800"}', "code": "invalid_unicode"}],
        }
        extra = {"codec": [{"name": "duplicate", "input": '{"a":1,"a":2}', "invalid": True,
                            "error_code": "duplicate_key"}]}
        original = verify.canonical({"codec": codec, "extra": extra})
        cases = fixtures.shared_canonical_cases(codec, extra)
        self.assertEqual(len(cases), 5)
        self.assertEqual(cases[0], codec["valid"][0])
        self.assertEqual(cases[1]["input"], codec["invalid"][0])
        self.assertNotIn("error_code", cases[1])
        for case, code in zip(cases[2:], ("noncanonical_number", "invalid_unicode", "duplicate_key")):
            self.assertTrue(case["reject"])
            self.assertEqual(case["error_code"], code)
        self.assertEqual(verify.canonical({"codec": codec, "extra": extra}), original)

    def test_canonical_rejections_require_exact_optional_error_code(self):
        case = {"reject": True, "error_code": "invalid_unicode"}
        for diagnostic in ("invalid_unicode", "invalid_unicode: unpaired surrogate",
                           "invalid_unicode: a different diagnostic"):
            fixtures.verify_canonical_output(case, {"error": diagnostic})
        for output in ({"canonical": "{}"}, {"error": None}, {"error": ""},
                       {"error": "codec_invalid: unpaired surrogate"},
                       {"error": "invalid_unicode_suffix: unpaired surrogate"},
                       {"error": " invalid_unicode: unpaired surrogate"}):
            with self.subTest(output=output), self.assertRaises(AssertionError):
                fixtures.verify_canonical_output(case, output)
        fixtures.verify_canonical_output({"reject": True}, {"error": "legacy rejection without prescribed code"})
        fixtures.verify_canonical_output({"expected": '{"a":1}'}, {"canonical": '{"a":1}'})
        with self.assertRaises(AssertionError):
            fixtures.verify_canonical_output({"expected": '{"a":1}'}, {"canonical": '{"a":2}'})

    def test_independent_lone_surrogate_literal_requires_unicode_code(self):
        case = next(case for case in fixtures.canonical_cases() if case["name"] == "lone-surrogate")
        self.assertEqual(case["input"], '{"a":"\\ud800"}')
        self.assertTrue(case["reject"])
        self.assertEqual(case["error_code"], "invalid_unicode")
        fixtures.verify_canonical_output(case, {"error": "invalid_unicode: unpaired high surrogate"})
        with self.assertRaises(AssertionError):
            fixtures.verify_canonical_output(case, {"error": "codec_invalid: unpaired high surrogate"})

    def test_actual_shared_canonical_error_cases_are_consumed(self):
        codec = json.loads((verify.SPEC / "fixtures/canonical.json").read_bytes())
        extra = json.loads((verify.ROOT / "actions/setup/js/work_queue_conformance_fixtures.json").read_bytes())
        cases = fixtures.shared_canonical_cases(codec, extra)
        self.assertEqual(len(cases), len(codec["valid"]) + len(codec["invalid"]) +
                         len(codec.get("error_codes", [])) + len(extra["codec"]))
        for literal in codec.get("error_codes", []):
            case = next(case for case in cases if case["name"] == f"shared-error-code/{literal['name']}")
            self.assertEqual(case["input"], literal["input"])
            self.assertEqual(case["error_code"], literal["code"])
            self.assertTrue(case["reject"])

    def test_reversed_restart_chain(self):
        ledger = verify.ready_workload()
        lines = ledger.text().splitlines()
        self.assertEqual(verify.ordered_ledger("\n".join(reversed(lines)) + "\n"), ledger.text())
        invalid = verify.ledger_text([
            {"id": "genesis", "previous": None},
            {"id": "left", "previous": "genesis"},
            {"id": "right", "previous": "genesis"},
        ])
        with self.assertRaises(AssertionError):
            verify.ordered_ledger(invalid)

    def test_full_mixed_graph_and_real_ready_frontier(self):
        ledger = verify.graph_workload()
        nodes = ledger.commits[1]["operations"]
        resources = {verify.canonical(edge["resource"]) for node in nodes for edge in node["depends_on"]}
        self.assertEqual(len(nodes) + len(resources), 4096)
        self.assertEqual(sum(not node["depends_on"] for node in nodes), 16)
        self.assertLessEqual(max(len(node["depends_on"]) for node in nodes), 64)
        self.assertEqual({edge["kind"] for node in nodes for edge in node["depends_on"]}, {"issue", "pull_request"})

    def test_drained_workload_preserves_original_prefix_and_cancels_every_work(self):
        ledger = verify.graph_workload()
        original = ledger.text()
        drained = verify.drained_workload(ledger)
        self.assertEqual(ledger.text(), original)
        self.assertEqual(drained.commits[:-1], ledger.commits)
        record = drained.commits[-1]
        self.assertEqual(record["request"]["kind"], "cancel_work")
        self.assertEqual(record["previous"], ledger.commits[-1]["id"])
        self.assertEqual(record["request"]["parameters"]["operations"], record["operations"])
        self.assertEqual(len(record["operations"]), 128)
        self.assertEqual({node["work_id"] for node in record["operations"]},
                         {node["work_id"] for node in ledger.commits[1]["operations"]})
        self.assertEqual({node["kind"] for node in record["operations"]}, {"WorkCancellation"})
        self.assertEqual(record["request"]["fingerprint"], verify.digest({
            "actor": record["actor"], "kind": "cancel_work", "parameters": record["request"]["parameters"],
        }))

    def test_benchmark_serialization_checks_content_not_just_equal_lengths(self):
        text = '{"a":"b"}\n'
        metrics = {"input_bytes": len(text.encode()), "canonical_bytes": len(text.encode()),
                   "canonical_sha256": verify.hashlib.sha256(text.encode()).hexdigest()}
        verify.verify_benchmark_serialization({"metrics": metrics}, text)
        altered = {**metrics, "canonical_sha256": verify.hashlib.sha256(b'{"a":"c"}\n').hexdigest()}
        with self.assertRaises(AssertionError):
            verify.verify_benchmark_serialization({"metrics": altered}, text)
        with self.assertRaises(AssertionError):
            verify.verify_benchmark_serialization({"metrics": {**metrics, "canonical_bytes": 1}}, text)

    def test_full_projection_parity_preserves_authority_and_metadata(self):
        projection = {
            "works": {"w": {"state": "completed", "barrier": "pending"}},
            "claims": {}, "dispatches": {"d": {"released": False, "state": "uncertain"}},
            "clocks": {}, "observations": {},
        }
        original = {"projection": projection, "metrics": {"rss_peak_bytes": 1}}
        verify.assert_native_parity(original, {"projection": projection, "metrics": {"rss_peak_bytes": 2}})
        for field, value in (("repository", "owner/repo"), ("completion_at", 1),
                             ("optional_null", None), ("idle_clock", {"pass": {}})):
            altered = {**projection, field: value}
            with self.subTest(field=field), self.assertRaises(AssertionError):
                verify.assert_native_parity(original, {"projection": altered})
        altered = {**projection, "works": {"w": {"state": "completed", "barrier": "verified"}}}
        with self.assertRaises(AssertionError):
            verify.assert_native_parity(original, {"projection": altered})
        self.assertNotIn("repository", projection)

    def test_source_identity_includes_native_adapters_and_contract_inputs(self):
        sources = verify.engine_source_hashes()
        for path in ("go.mod", "go.sum", "specs/work-queue/native_probe.cjs",
                     "specs/work-queue/native_probe/main.go", "specs/work-queue/native_probe/rss_unix.go",
                     "specs/work-queue/verify_native.py", "specs/work-queue/verify_native_fixtures.py",
                     "specs/work-queue/fixtures/canonical-prefix.json",
                     "specs/work-queue/fixtures/canonical-typed.json",
                     "specs/work-queue/fixtures/reason-validation.json",
                     "specs/work-queue/fixtures/identity-validation.json",
                     "specs/work-queue/effect-contract.schema.json",
                     "specs/work-queue/resource-scope.schema.json",
                     "specs/work-queue/fixtures/effect-contract.json",
                     "specs/work-queue/fixtures/resource-scope.json",
                     "pkg/workqueue/resource_scope.go",
                     "actions/setup/js/work_queue_resource_scope.cjs",
                     "actions/setup/js/work_queue_store.cjs",
                     "actions/setup/js/work_queue_provisioning.cjs",
                     "actions/setup/js/work_queue_yaml.cjs",
                     "actions/setup/js/work_queue_conformance_fixtures.json"):
            self.assertIn(path, sources)
        for path in sources:
            if not path.startswith("actions/setup/js/") or not path.endswith(".cjs"):
                continue
            text = (verify.ROOT / path).read_text()
            for dependency in re.findall(r"""require\(["'](\./[^"']+\.cjs)["']\)""", text):
                resolved = (verify.ROOT / path).parent.joinpath(dependency).resolve()
                self.assertIn(str(resolved.relative_to(verify.ROOT)), sources, path)
        original_read = Path.read_bytes
        for name in ("actions/setup/js/work_queue_claim_scope.cjs",
                     "actions/setup/js/work_queue_store.cjs", "actions/setup/js/work_queue_provisioning.cjs",
                     "actions/setup/js/work_queue_yaml.cjs", "specs/work-queue/effect-contract.schema.json",
                     "specs/work-queue/resource-scope.schema.json"):
            with self.subTest(source=name):
                target = verify.ROOT / name
                data = original_read(target)
                with mock.patch.object(Path, "read_bytes", autospec=True,
                                       side_effect=lambda path: data + b"\n" if path == target else original_read(path)):
                    changed = verify.engine_source_hashes()
                self.assertEqual(set(changed), set(sources))
                self.assertEqual([path for path in sources if changed[path] != sources[path]], [name])

    def test_principal_policy_answers_are_not_engine_outputs(self):
        cases = fixtures.principal_policy_cases(verify)
        self.assertEqual(len(cases), 16)
        self.assertEqual(sum(case["valid"] for case in cases), 4)
        for case in cases:
            if "principal-profile-" in case["name"]:
                principal = case["policy"]["pools"]["default"]["profiles"]["default"]["principal"]
            else:
                self.assertEqual(len(case["policy"]["producers"]), 1)
                principal = next(iter(case["policy"]["producers"]))
            valid = bool(re.fullmatch(r"[1-9][0-9]{0,255}", principal))
            self.assertEqual(valid, case["valid"], case["name"])
            self.assertEqual(case["error_code"], "policy_invalid")

    def test_future_reference_budget_control_is_independent(self):
        ledger = verify.future_reference_workload(11)
        nodes = ledger.commits[1]["operations"]
        self.assertEqual(len(nodes), 12)
        self.assertEqual(len(nodes[0]["depends_on"]), 11)
        self.assertEqual({edge["work_id"] for edge in nodes[0]["depends_on"]},
                         {node["work_id"] for node in nodes[1:]})
        limit = ledger.policy["limits"]["assignment_bytes"]
        descriptor = ledger.policy["limits"]["result_bytes"]
        self.assertLess(11 * (descriptor + 256), limit, "retired metadata approximation falsely fits")
        self.assertGreater(11 * (descriptor + 256 * 2), limit, "legal escaped future IDs alone establish refusal")
        fitting = verify.future_reference_workload(7)
        child = fitting.commits[1]["operations"][0]
        identity = "\\" * 256
        assignment = {
            "version": 3, "dispatch_id": "d_" + "f" * 64 + "_16",
            "request_id": identity, "commit_id": identity, "policy_epoch": "epoch",
            "pool": "default", "worker_profile": "default",
            "claims": [{"handle": "h16", "claim_id": "c_" + "f" * 64 + "_16",
                        "work_id": child["work_id"], "work": child["payload"],
                        "result_refs": [{"work_id": edge["work_id"], "result_commit_id": identity, "descriptor": {}}
                                        for edge in child["depends_on"]]}],
        }
        upper_bound = len(verify.canonical(assignment).encode()) + 7 * (descriptor - 2)
        self.assertLess(upper_bound, limit, "seven references fit even with escaped future identities")

    def test_future_reference_closed_envelope_bounds_are_literal_and_legal(self):
        for count, expected_bytes, refusal in ((7, 34515, False), (9, 43961, False),
                                               (10, 48684, False), (11, 53407, True)):
            with self.subTest(references=count):
                ledger = verify.future_reference_workload(count)
                original = ledger.text()
                assignment, bound = verify.future_reference_assignment_bound(ledger)
                self.assertEqual(bound, expected_bytes)
                self.assertEqual(bound, 1454 + count * 4723)
                self.assertEqual(bound > ledger.policy["limits"]["assignment_bytes"], refusal)
                identities = [assignment["request_id"], assignment["commit_id"],
                              *[reference["result_commit_id"] for reference in assignment["claims"][0]["result_refs"]]]
                for identity in identities:
                    self.assertEqual(len(identity.encode()), 256)
                    self.assertEqual(len(verify.canonical(identity).encode()) - 2, 512)
                    self.assertFalse(any(ord(char) < 32 or ord(char) == 127 for char in identity))
                descriptor = {"p": "x" * (ledger.policy["limits"]["result_bytes"] - 8)}
                self.assertEqual(len(verify.canonical(descriptor).encode()), 4096)
                for reference in assignment["claims"][0]["result_refs"]:
                    reference["descriptor"] = descriptor
                self.assertEqual(len(verify.canonical(assignment).encode()), expected_bytes)
                self.assertEqual(ledger.text(), original)

    def test_future_exact_boundary_policy_is_installed_before_fingerprinting(self):
        for count, bound in ((9, 43961), (10, 48684)):
            for limit in (bound, bound - 1):
                with self.subTest(references=count, assignment_bytes=limit):
                    ledger = verify.future_reference_workload(count, assignment_bytes=limit)
                    self.assertEqual(ledger.policy["limits"]["assignment_bytes"], limit)
                    genesis = ledger.commits[0]
                    self.assertEqual(genesis["request"]["fingerprint"], verify.digest({
                        "actor": genesis["actor"], "kind": "policy", "parameters": genesis["request"]["parameters"],
                    }))
                    _, actual = verify.future_reference_assignment_bound(ledger)
                    self.assertEqual(actual, bound)
        self.assertEqual(verify.default_policy()["limits"]["assignment_bytes"], 49152)

    def test_structural_wire_answers_preserve_shared_inputs_and_request_binding(self):
        source = json.loads((verify.SPEC / "fixtures/canonical-prefix.json").read_bytes())
        original = verify.canonical(source)
        cases = fixtures.structural_wire_cases(source, verify)
        self.assertEqual(len(cases), 16)
        self.assertEqual(sum(case["valid"] for case in cases), 6)
        for case in cases:
            with self.subTest(case=case["name"]):
                record = case["record"]
                self.assertEqual(record["request"]["fingerprint"], verify.digest({
                    "actor": record["actor"], "kind": record["request"]["kind"],
                    "parameters": record["request"]["parameters"],
                }))
                if case["name"].startswith("wire-claim-observations-"):
                    values = next(operation["observations"] for operation in record["operations"]
                                  if operation["kind"] == "Claim")
                    self.assertEqual(len(values) <= 64 and len(set(values)) == len(values), case["valid"])
                else:
                    value = record["actor"]["principal"] if "actor-principal" in case["name"] else next(
                        operation["resource"]["resource_id"] for operation in record["operations"]
                        if operation["kind"] == "Observation")
                    self.assertEqual(bool(re.fullmatch(r"[1-9][0-9]{0,255}", value)), case["valid"])
        self.assertEqual(verify.canonical(source), original)

    def test_shared_deadline_answers_and_trusted_record_binding_are_preserved(self):
        source = json.loads((verify.SPEC / "fixtures/canonical-prefix.json").read_bytes())
        deadlines = json.loads((verify.SPEC / "fixtures/delivery-deadlines.json").read_bytes())
        original = verify.canonical(source)
        cases = fixtures.delivery_deadline_cases(source, deadlines, verify)
        self.assertEqual(len(cases), len(deadlines["cases"]))
        for case, literal in zip(cases, deadlines["cases"]):
            with self.subTest(case=case["name"]):
                eligible = literal["attempts"] >= deadlines["max_attempts"] or (
                    literal["at"] >= deadlines["completion_at"] + deadlines["deadline_ms"])
                effects = literal.get("effects", "unknown")
                proof = literal["disposition"] == effects and (
                    effects == "unknown" or bool(literal.get("receipt")))
                expected = "reconciliation_pending" if not eligible else "allowed" if proof else "evidence_invalid"
                self.assertEqual(case["expected"], literal["expected"])
                self.assertEqual(case["expected"], expected)
                record = case["commits"][-1]
                self.assertEqual(record["previous"], case["commits"][-2]["id"])
                self.assertEqual(case["commits"][-2]["at"], deadlines["completion_at"])
                self.assertEqual(record["request"]["parameters"]["operations"], record["operations"])
                evidence = record["operations"][0]["evidence"]
                self.assertEqual(evidence.get("attempts", 0), literal["attempts"])
                self.assertEqual("attempts" in evidence, literal["attempts"] != 0)
                self.assertEqual(record["request"]["fingerprint"], verify.digest({
                    "actor": record["actor"], "kind": record["request"]["kind"],
                    "parameters": record["request"]["parameters"],
                }))
        self.assertEqual(verify.canonical(source), original)

    def test_shared_reason_answers_and_authenticated_input_are_preserved(self):
        source = json.loads((verify.SPEC / "fixtures/reason-validation.json").read_bytes())
        self.assertEqual(source["version"], 3)
        self.assertEqual(source["pattern"], "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
        for case in source["cases"]:
            with self.subTest(case=case["name"]):
                self.assertEqual(bool(re.fullmatch(source["pattern"], case["reason"])), case["valid"])
                ledger = fixtures.reason_ledger(case["reason"], verify)
                commit = ledger.commits[-1]
                self.assertEqual(commit["operations"][0]["reason"], case["reason"])
                self.assertEqual(commit["request"]["parameters"]["operations"], commit["operations"])
                self.assertEqual(commit["request"]["fingerprint"], verify.digest({
                    "actor": commit["actor"], "kind": "control", "parameters": commit["request"]["parameters"],
                }))

    def test_closure_audit_counts_duplicate_result_and_escaped_metadata(self):
        case = verify.closure_byte_cases()[0]
        record = case["record"]
        limits = verify.default_policy()["limits"]
        operation = record["operations"][0]
        self.assertEqual(record["request"]["parameters"]["operations"], record["operations"])
        self.assertEqual(len(verify.canonical(operation["descriptor"]).encode()), limits["result_bytes"])
        self.assertEqual(len(verify.canonical(operation["evidence"]).encode()), limits["evidence_bytes"])
        for identity in (record["id"], record["previous"], record["policy_epoch"], record["request"]["id"],
                         operation["completion_id"]):
            self.assertEqual(len(identity.encode()), 256)
            self.assertEqual(len(verify.canonical(identity).encode()), 514)
            self.assertFalse(any(ord(char) < 32 or ord(char) == 127 for char in identity))
        self.assertEqual(record["request"]["fingerprint"], verify.digest({
            "actor": record["actor"], "kind": "result", "parameters": record["request"]["parameters"],
        }))
        self.assertLess(len(verify.canonical(record).encode()) + 1, case["reserved_bytes"])

    def test_shared_identity_answers_and_typed_positions_are_preserved(self):
        source = json.loads((verify.SPEC / "fixtures/identity-validation.json").read_bytes())
        self.assertEqual(source["identity_max_utf8_bytes"], 256)
        self.assertEqual(source["decimal_max_digits"], 256)
        for case in source["identity"]:
            with self.subTest(case=case["name"]):
                text = case["text"] * case["repeat"]
                self.assertEqual(len(text.encode()), case["expected_utf8_bytes"])
                valid = bool(text) and len(text.encode()) <= 256 and not any(
                    ord(char) < 32 or ord(char) == 127 for char in text)
                self.assertEqual(valid, case["valid"])
                self.assertEqual(fixtures.identity_ledger(text, False, verify).commits[0]["id"], text)
        for case in source["decimal"]:
            with self.subTest(case=case["name"]):
                self.assertEqual(1 <= case["digits"] <= 256, case["valid"])
                record = fixtures.identity_ledger("9" * case["digits"], True, verify).commits[0]
                self.assertEqual(record["actor"]["run_id"], "9" * case["digits"])
                self.assertEqual(record["request"]["fingerprint"], verify.digest({
                    "actor": record["actor"], "kind": "policy", "parameters": record["request"]["parameters"],
                }))

    def test_additional_shared_selection_answers_survive_shape_adapter(self):
        source = json.loads((verify.ROOT / "actions/setup/js/work_queue_conformance_fixtures.json").read_bytes())
        self.assertEqual(source["version"], 3)
        for original in source["selection"]:
            with self.subTest(case=original["name"]):
                case = fixtures.shared_selection_case(original, verify)
                self.assertEqual(case["expected"], original["expected_names"])
                self.assertEqual([node["key"] for node in case["nodes"]],
                                 [work["name"] for work in original["works"]])
                self.assertEqual(case["max_claims"], len(original["expected_names"]))
                self.assertEqual(case["accounting_weights"], original["accounting_weights"])
                ledger, nodes = fixtures.selection_ledger(case, verify)
                self.assertEqual([node["node_key"] for node in ledger.commits[1]["operations"]],
                                 [work["name"] for work in original["works"]])
                operations, assignments = fixtures.packing_oracle(case, nodes, verify, "independent", "candidate")
                self.assertEqual([nodes[key]["work_id"] for key in original["expected_names"]],
                                 [operation["work_id"] for operation in operations])
                self.assertEqual(sum(len(assignment["claims"]) for assignment in assignments), len(case["expected"]))

    def test_default_recovery_allocation_is_distinct_from_graph_ceiling(self):
        ledger = verify.Ledger()
        for batch in range(2):
            ledger.submit([ledger.work(f"default-reserve-{batch}-{index}", payload_size=8)
                           for index in range(256)])
        limits = ledger.policy["limits"]
        per_work = (ledger.policy["pools"]["default"]["retry"]["max_attempts"] *
                    (4 * limits["evidence_bytes"] + 8192) +
                    2 * limits["result_bytes"] + 2 * limits["evidence_bytes"] + 8192)
        self.assertLessEqual(256 * per_work, limits["recovery_bytes"])
        self.assertGreater(512 * per_work, limits["recovery_bytes"])
        self.assertLess(512, limits["graph_nodes"])
        self.assertLess(len(ledger.text().encode()), limits["ledger_bytes"])
        self.assertTrue(all(len(commit["operations"]) <= limits["operations"] for commit in ledger.commits))

    def test_byte_overflow_packing_oracle_uses_earliest_compatible_group(self):
        case = fixtures.byte_packing_case()
        _, nodes = fixtures.selection_ledger(case, verify)
        operations, assignments = fixtures.packing_oracle(case, nodes, verify, "independent", "candidate")
        self.assertEqual([operation["work_id"] for operation in operations],
                         [nodes[key]["work_id"] for key in ("a", "b", "c")])
        self.assertEqual([[claim["work"]["task"] for claim in assignment["claims"]] for assignment in assignments],
                         [["a", "c"], ["b"]])
        combined = {**assignments[0], "claims": [assignments[0]["claims"][0], assignments[1]["claims"][0]]}
        self.assertGreater(len(verify.canonical(combined).encode()), case["max_bytes"])
        self.assertTrue(all(len(verify.canonical(assignment).encode()) <= case["max_bytes"] for assignment in assignments))


if __name__ == "__main__":
    unittest.main()
