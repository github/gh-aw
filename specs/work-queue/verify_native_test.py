#!/usr/bin/env python3
"""Dependency-free checks for the independent driver (python3 -B ...)."""

import importlib.util
import json
import re
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("verify_native", Path(__file__).with_name("verify_native.py"))
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)
fixture_spec = importlib.util.spec_from_file_location("verify_native_fixtures", Path(__file__).with_name("verify_native_fixtures.py"))
fixtures = importlib.util.module_from_spec(fixture_spec)
fixture_spec.loader.exec_module(fixtures)


class IndependentDriverTests(unittest.TestCase):
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
                     "specs/work-queue/fixtures/reason-validation.json",
                     "specs/work-queue/fixtures/identity-validation.json",
                     "actions/setup/js/work_queue_conformance_fixtures.json"):
            self.assertIn(path, sources)

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

    def test_independent_expected_values_are_not_parity_only(self):
        with self.assertRaises(AssertionError):
            verify.assert_subset({"selection": {"work_id": "wrong"}}, {"selection": {"work_id": "expected"}})

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
