#!/usr/bin/env python3
"""Credential-free mapping and real gh-aw runtime contract tests."""

import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
spec = importlib.util.spec_from_file_location("harbor_verify", HERE / "verify.py")
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


def selection(mapping="threshold", required=True, grader="outcome", reward="reward"):
    return {"grader": grader, "reward": reward, "mapping": mapping, "required": required}


def envelope(value=1, passed=True, status="pass", threshold=1, direction="higher_is_better"):
    return {
        "version": 1,
        "definition": {"id": "outcome", "threshold": threshold, "direction": direction},
        "result": {"id": "outcome", "value": value, "passed": passed, "status": status},
    }


class MappingTests(unittest.TestCase):
    def test_numeric_rewards(self):
        cases = [
            (envelope(), selection(), 1),
            (envelope(0, False, "fail"), selection(), 0),
            (envelope(0, True, threshold=1, direction="lower_is_better"), selection(), 1),
            (envelope(0, None, threshold=None), selection("raw"), 0),
            (envelope(0.25, None, threshold=None), selection("raw"), 0.25),
            (envelope(0, False, "fail"), selection("raw"), 0),
        ]
        for result, metric, expected in cases:
            with self.subTest(result=result, metric=metric):
                self.assertEqual(verify.map_result(result, metric), expected)

    def test_unavailable_is_not_zero(self):
        result = envelope(None, None, "unavailable")
        with self.assertRaises(verify.UnavailableError):
            verify.map_result(result, selection())
        self.assertIsNone(verify.map_result(result, selection(required=False)))

    def test_invalid_results(self):
        invalid = [
            envelope(0, None, threshold=None),
            envelope(float("nan")),
            envelope(float("inf")),
            envelope(True),
            envelope(1, None),
            envelope(0, True),
            envelope(1, False, "fail"),
            envelope(1, True, "error"),
            envelope(None, None, "unavailable"),
        ]
        # Null is valid only for the unavailable path, not a measured result.
        invalid[-1]["result"]["value"] = 1
        wrong_id = envelope()
        wrong_id["result"]["id"] = "different"
        invalid.extend([wrong_id, {}, {"version": True}, None])
        for result in invalid:
            with self.subTest(result=result), self.assertRaises(verify.VerificationError):
                verify.map_result(result, selection())

    def test_duplicate_json_keys(self):
        with self.assertRaises(verify.VerificationError):
            verify.parse_json('{"reward": 1, "reward": 0}')

    def test_nonfinite_json_is_rejected_before_publication(self):
        for value in ("NaN", "Infinity", "-Infinity", "1e309"):
            with self.subTest(value=value), self.assertRaises(verify.VerificationError):
                verify.parse_json('{"value":' + value + "}")


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.config = self.root / "mapping.json"
        self.workflow = self.root / "workflow.md"
        self.workflow.write_text("trusted workflow", encoding="utf-8")
        self.session = self.root / "session.jsonl"
        self.session.write_text("[]", encoding="utf-8")
        self.output = self.root / "output"
        self.config_data = {"version": 1, "workflow": "workflow.md", "metrics": [selection()]}
        self.save_config()

    def save_config(self):
        self.config.write_text(json.dumps(self.config_data), encoding="utf-8")

    def run_with(self, results):
        with patch.object(verify, "run_grader", side_effect=[(*result, "") for result in results]):
            return verify.verify(self.config, self.session, self.output, sys.executable)

    def report(self):
        return json.loads((self.output / "gh-aw-graders.json").read_text())

    def test_success_and_stale_reward_cleanup(self):
        self.output.mkdir()
        (self.output / "reward.txt").write_text("1")
        self.assertEqual(self.run_with([(0, envelope())]), 0)
        self.assertEqual(json.loads((self.output / "reward.json").read_text()), {"reward": 1})
        self.assertFalse((self.output / "reward.txt").exists())
        self.assertIn("binary_sha256", self.report()["provenance"])
        self.assertEqual(self.run_with([(0, envelope(None, None, "unavailable"))]), 2)
        self.assertEqual(self.report()["status"], "unavailable")
        self.assertFalse((self.output / "reward.json").exists())
        self.assertEqual(self.run_with([(1, envelope(None, None, "error"))]), 1)
        self.assertEqual(self.report()["status"], "error")
        self.assertFalse((self.output / "reward.json").exists())

    def test_optional_missing_reward_retains_coverage(self):
        self.config_data["metrics"].append(selection("raw", False, "optional", "optional"))
        self.save_config()
        optional = envelope(None, None, "unavailable")
        optional["definition"]["id"] = optional["result"]["id"] = "optional"
        self.assertEqual(self.run_with([(0, envelope()), (0, optional)]), 0)
        self.assertEqual(self.report()["coverage"], {"reward": True, "optional": False})
        self.assertEqual(json.loads((self.output / "reward.json").read_text()), {"reward": 1})

    def test_errors_do_not_publish_partial_rewards(self):
        self.config_data["metrics"].append(selection("raw", False, "optional", "optional"))
        self.save_config()
        self.assertEqual(self.run_with([(0, envelope()), (1, None)]), 1)
        self.assertFalse((self.output / "reward.json").exists())
        self.assertEqual(len(self.report()["results"]), 2)

    def test_only_reward_mapping_fields_are_persisted(self):
        secret = "synthetic-unclassified-private-content"
        result = envelope()
        result["definition"].update({
            "config": {"token": secret, "nested": {"target": secret}},
            "name": secret, "description": secret, "unit": secret,
            "script": secret, "unexpected": secret,
        })
        result["result"].update({
            "message": secret, "error": secret, "details": {"innocent": [secret]},
            "diagnostics": [{"message": secret}], "metrics": [{"id": secret, "value": 7}],
            "implementation": {"id": secret, "digest": secret},
        })
        result["unexpected"] = secret
        original = copy.deepcopy(result)
        with patch.object(verify, "run_grader", return_value=(0, result, secret)), patch(
            "sys.stderr", new_callable=io.StringIO
        ) as stderr:
            self.assertEqual(verify.verify(self.config, self.session, self.output, sys.executable), 0)
        report = self.report()
        self.assertNotIn(secret, json.dumps(report) + stderr.getvalue())
        self.assertEqual(report["results"][0], {
            "selection": selection(), "output": {
                **envelope(), "definition": {
                    **envelope()["definition"], "config": {}, "config_omitted": True,
                },
            },
            "exit_code": 0, "stderr": "", "stderr_omitted": True, "diagnostics_omitted": True,
        })
        self.assertEqual(result, original)
        self.assertEqual(json.loads((self.output / "reward.json").read_text()), {"reward": 1})

    def test_free_form_fields_cannot_bypass_allowlist(self):
        secret = "synthetic-invalid-field-secret"
        for field in ("version", "definition", "result"):
            result = envelope()
            result[field] = secret
            with self.subTest(field=field):
                self.assertEqual(self.run_with([(0, result)]), 1)
                self.assertNotIn(secret, json.dumps(self.report()))
        for section, fields in (
            ("definition", ("id", "threshold", "direction")),
            ("result", ("id", "status", "value", "passed")),
        ):
            for field in fields:
                result = envelope()
                result[section][field] = {"unclassified": secret}
                with self.subTest(section=section, field=field):
                    self.assertEqual(self.run_with([(0, result)]), 1)
                    self.assertNotIn(secret, json.dumps(self.report()))

    def test_evaluator_and_parser_errors_do_not_echo_content(self):
        secret = "synthetic-evaluator-error-secret"
        result = envelope(None, None, "error")
        result["result"]["error"] = secret
        with patch.object(verify, "run_grader", return_value=(1, result, secret)), patch(
            "sys.stderr", new_callable=io.StringIO
        ) as stderr:
            self.assertEqual(verify.verify(self.config, self.session, self.output, sys.executable), 1)
        self.assertNotIn(secret, json.dumps(self.report()) + stderr.getvalue())
        for error in (
            verify.VerificationError(secret), ValueError(secret), OSError(secret),
        ):
            with self.subTest(error=type(error)), patch.object(
                verify, "run_grader", side_effect=error
            ), patch("sys.stderr", new_callable=io.StringIO) as stderr:
                self.assertEqual(verify.verify(self.config, self.session, self.output, sys.executable), 1)
                self.assertNotIn(secret, json.dumps(self.report()) + stderr.getvalue())
                self.assertFalse((self.output / "reward.json").exists())

    def test_malformed_stdout_is_not_disclosed(self):
        secret = "synthetic-malformed-stdout-secret"
        executable = self.root / "fake-grader"
        for output in (secret, '{"' + secret + '":1,"' + secret + '":2}'):
            executable.write_text(f"#!{sys.executable}\nprint({output!r})\n")
            executable.chmod(0o700)
            with self.subTest(output=output), patch("sys.stderr", new_callable=io.StringIO) as stderr:
                self.assertEqual(verify.verify(self.config, self.session, self.output, executable), 1)
                self.assertNotIn(secret, json.dumps(self.report()) + stderr.getvalue())
                self.assertFalse((self.output / "reward.json").exists())

    def test_invalid_config(self):
        cases = []
        for field, value in (
            ("workflow", "../escape.md"), ("workflow", str(self.workflow)), ("version", True), ("metrics", [])
        ):
            config = copy.deepcopy(self.config_data)
            config[field] = value
            cases.append(config)
        duplicate = copy.deepcopy(self.config_data)
        duplicate["metrics"].append(selection())
        cases.append(duplicate)
        optional_only = copy.deepcopy(self.config_data)
        optional_only["metrics"][0]["required"] = False
        cases.append(optional_only)
        for config in cases:
            self.config.write_text(json.dumps(config))
            with self.subTest(config=config), self.assertRaises(verify.VerificationError):
                verify.load_config(self.config)

    def test_input_limit_and_missing_binary(self):
        with patch.object(verify, "MAX_SESSION_BYTES", 1):
            self.assertEqual(self.run_with([]), 1)
        self.assertFalse((self.output / "reward.json").exists())
        self.assertEqual(verify.verify(self.config, self.session, self.output, "no-such-gh-aw"), 1)

    def test_process_limits_and_environment(self):
        executable = self.root / "fake-grader"
        cases = [
            ("import time; time.sleep(10)", "timed out"),
            ("print('x' * 4096)", "output exceeds"),
            ("import sys; sys.stderr.write('x' * 4096)", "output exceeds"),
        ]
        for source, expected in cases:
            executable.write_text(f"#!{sys.executable}\n{source}\n")
            executable.chmod(0o700)
            with self.subTest(source=source), patch.object(verify, "GRADER_TIMEOUT", 0.2), patch.object(
                verify, "MAX_OUTPUT_BYTES", 1024
            ), self.assertRaisesRegex(verify.VerificationError, expected):
                verify.run_grader(executable, self.workflow, "outcome", b"[]")
        executable.write_text(
            f"#!{sys.executable}\nimport json, os\n"
            "assert 'GITHUB_TOKEN' not in os.environ\nprint(json.dumps({'version': 1}))\n"
        )
        with patch.dict(os.environ, {"GITHUB_TOKEN": "must-not-inherit"}):
            self.assertEqual(verify.run_grader(executable, self.workflow, "outcome", b"[]"), (0, {"version": 1}, ""))


@unittest.skipUnless((ROOT / "gh-aw").is_file(), "build gh-aw before running real-runtime tests")
class RuntimeTests(unittest.TestCase):
    def test_config_and_evaluator_diagnostics_are_not_persisted(self):
        secret = "synthetic-runtime-config-secret"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            session = root / "session.jsonl"
            session.write_text("[]")
            config = root / "mapping.json"
            config.write_text(json.dumps({"version": 1, "workflow": "workflow.md", "metrics": [selection()]}))
            for script, expected_code in (
                ("return { value: config.score, message: config.token, details: { nested: config.token } }", 0),
                ("throw new Error(config.token)", 1),
            ):
                (root / "workflow.md").write_text(
                    "---\non: workflow_dispatch\ngraders:\n  outcome:\n"
                    "    threshold: 1\n    direction: higher_is_better\n"
                    f"    config:\n      score: 1\n      token: {secret}\n"
                    f"    script: |\n      {script}\n---\nTest.\n"
                )
                with self.subTest(script=script), patch("sys.stderr", new_callable=io.StringIO) as stderr:
                    self.assertEqual(verify.verify(config, session, root / "output", ROOT / "gh-aw"), expected_code)
                    report = (root / "output/gh-aw-graders.json").read_text()
                    self.assertNotIn(secret, report + stderr.getvalue())
                    reward = root / "output/reward.json"
                    if expected_code == 0:
                        self.assertEqual(json.loads(reward.read_text()), {"reward": 1})
                    else:
                        self.assertFalse(reward.exists())

    def test_invalid_definitions_never_publish_rewards(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            session = root / "session.jsonl"
            session.write_text("[]")
            config = root / "mapping.json"
            config.write_text(json.dumps({"version": 1, "workflow": "workflow.md", "metrics": [selection()]}))
            for body in (
                "script: throw new Error('test failure')",
                "script: return Infinity",
                "script: return 0",
                "script: return 1\n    enabled: false",
            ):
                (root / "workflow.md").write_text(
                    "---\non: workflow_dispatch\ngraders:\n  outcome:\n    " + body + "\n---\nTest.\n"
                )
                with self.subTest(body=body):
                    self.assertEqual(verify.verify(config, session, root / "output", ROOT / "gh-aw"), 1)
                    self.assertFalse((root / "output/reward.json").exists())
            config.write_text(json.dumps({
                "version": 1, "workflow": "workflow.md",
                "metrics": [selection(grader="unknown")],
            }))
            self.assertEqual(verify.verify(config, session, root / "output", ROOT / "gh-aw"), 1)
            self.assertFalse((root / "output/reward.json").exists())

    def test_reference_evidence_matrix(self):
        fixture = HERE / "task"
        baseline = (fixture / "tests/workflow.md").read_text()
        records = [json.loads(line) for line in (fixture / "environment/aw_session.jsonl").read_text().splitlines()]
        good = records[-1]
        cases = [
            ("attained", {}, 0, 1),
            ("missed", {"status": "failure"}, 0, 0),
            ("wrong-target", {"number": 43}, 0, 0),
            ("staged-only", {"staged": True}, 0, 0),
            ("missing-identity", {"url": None}, 2, None),
            ("malformed", {"status": "unknown"}, 2, None),
            ("inapplicable", {"type": "create_issue"}, 2, None),
            ("legitimate-noop", {"status": "skipped", "url": None}, 0, 1),
            ("unexpected-noop", {"status": "skipped", "url": None}, 0, 0),
        ]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "mapping.json"
            config.write_bytes((fixture / "tests/mapping.json").read_bytes())
            session = root / "session.jsonl"
            for name, updates, expected_code, reward in cases:
                data = copy.deepcopy(good)
                data["data"].update(updates)
                session.write_text(json.dumps([records[0], data]))
                workflow = baseline.replace("expectNoop: false", "expectNoop: true") if name == "legitimate-noop" else baseline
                (root / "workflow.md").write_text(workflow)
                with self.subTest(name=name):
                    for _ in range(2):
                        code = verify.verify(config, session, root / "output", ROOT / "gh-aw")
                        self.assertEqual(code, expected_code)
                        output = root / "output/reward.json"
                        if reward is None:
                            self.assertFalse(output.exists())
                        else:
                            self.assertEqual(json.loads(output.read_text()), {"reward": reward})
            session.write_text("{invalid")
            self.assertEqual(verify.verify(config, session, root / "output", ROOT / "gh-aw"), 1)
            self.assertFalse((root / "output/reward.json").exists())


if __name__ == "__main__":
    unittest.main()
