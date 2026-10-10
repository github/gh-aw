#!/usr/bin/env python3
"""Exercise model-routing fixture capture against a synthetic downloaded run."""

import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/model-routing-golden.py"
SOURCE = ROOT / "pkg/cli/testdata/model_routing_golden_capture"


class ModelRoutingGoldenCaptureTest(unittest.TestCase):
    def test_structured_identifier_values_are_redacted(self):
        spec = importlib.util.spec_from_file_location("model_routing_golden", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        for key in ("input", "arguments"):
            self.assertEqual(
                module.redact({key: {"agent": {"query": "PRIVATE_TEXT"}, "model": ["PRIVATE_TEXT"]}}, {}),
                {key: {"agent": "[redacted]", "model": "[redacted]"}},
            )

    def test_pi_capture_preserves_subagent_identifiers(self):
        for session_path in ("usage/aw_session.jsonl", "agent-session.jsonl"):
            with self.subTest(session_path=session_path), tempfile.TemporaryDirectory(prefix="pi-capture-test-") as temporary:
                source = Path(temporary) / "download"
                source.mkdir()
                (source / "aw_info.json").write_bytes((SOURCE / "pi/aw_info.json").read_bytes())
                session = source / session_path
                session.parent.mkdir(parents=True, exist_ok=True)
                session.write_bytes((SOURCE / "pi" / session_path).read_bytes())
                usage_path = "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"
                usage = source / usage_path
                usage.parent.mkdir(parents=True, exist_ok=True)
                usage.write_bytes((SOURCE / "pi" / usage_path).read_bytes())
                output_root = Path(temporary) / "fixtures"
                subprocess.run(
                    [
                        sys.executable,
                        str(SCRIPT),
                        "--source-dir",
                        str(source),
                        "--case",
                        "pi-capture",
                        "--output-root",
                        str(output_root),
                    ],
                    cwd=ROOT,
                    check=True,
                )
                fixture = output_root / "pi-capture"
                records = [json.loads(line) for line in (fixture / session_path).read_text(encoding="utf-8").splitlines()]
                start = next(record for record in records if record["type"] == "tool.execution_start")
                tool_input = start["data"]["input"]
                self.assertEqual(tool_input["agent"], "file-summarizer")
                for key in ("task", "prompt", "text", "query"):
                    self.assertEqual(tool_input[key], "[redacted]")
                self.assertEqual(
                    start["data"]["arguments"],
                    {"agent": "file-summarizer", "task": "[redacted]"},
                )
                argument_text = json.loads(start["data"]["argumentText"])
                self.assertEqual(argument_text["agent"], "file-summarizer")
                for key in ("task", "title", "body"):
                    self.assertEqual(argument_text[key], "[redacted]")
                all_content = "\n".join(path.read_text(encoding="utf-8") for path in fixture.rglob("*") if path.is_file())
                for secret in (
                    "PRIVATE_",
                    "private-org",
                    "private-repo",
                    "private-user",
                    "/home/",
                    "PRIVATE_ARGUMENT_TEXT_TASK",
                    "PRIVATE_ISSUE_TITLE",
                    "PRIVATE_ISSUE_BODY",
                ):
                    self.assertNotIn(secret, all_content)
                start["data"]["input"]["agent"] = "[redacted]"
                (fixture / session_path).write_text(
                    "".join(json.dumps(record) + "\n" for record in records),
                    encoding="utf-8",
                )
                env = os.environ.copy()
                env["MODEL_ROUTING_GOLDEN_FULL_DIR"] = str(source)
                env["MODEL_ROUTING_GOLDEN_FIXTURE_DIR"] = str(fixture)
                result = subprocess.run(
                    ["go", "test", "./pkg/cli", "-run", "^TestModelRoutingGoldenCaptureMatchesDownload$", "-count=1"],
                    cwd=ROOT,
                    env=env,
                    capture_output=True,
                    text=True,
                )
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('"invocation_count": 1', result.stdout)
                self.assertIn('"completed_count": 1', result.stdout)
                self.assertIn('"subagent_model_requests"', result.stdout)

    def test_capture_redacts_and_preserves_analysis_inputs(self):
        with tempfile.TemporaryDirectory(prefix="model-routing-capture-test-") as temporary:
            output_root = Path(temporary) / "fixtures"
            subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "--source-dir",
                    str(SOURCE),
                    "--case",
                    "capture-smoke",
                    "--output-root",
                    str(output_root),
                ],
                cwd=ROOT,
                check=True,
            )
            fixture = output_root / "capture-smoke"
            files = {path.relative_to(fixture).as_posix() for path in fixture.rglob("*") if path.is_file()}
            self.assertEqual(
                files,
                {
                    "aw_info.json",
                    "agent/aw_info.json",
                    "agent/awf-routing-outcome.json",
                    "agent-session.jsonl",
                    "agent_usage.json",
                    "usage/agent_usage.json",
                    "usage/aw_info.json",
                    "usage/aw_session.jsonl",
                    "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl",
                    "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl",
                },
            )
            all_content = "\n".join(path.read_text(encoding="utf-8") for path in fixture.rglob("*") if path.is_file())
            for secret in ("PRIVATE_", "private-org", "private-repo", "private-user", "/home/"):
                self.assertNotIn(secret, all_content)

            token_usage = (fixture / "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl").read_text(encoding="utf-8")
            self.assertIn('"ai_credits_this_response":0.25', token_usage)
            self.assertIn('"request_id":"capture-request-2"', token_usage)
            self.assertIn('"model":"gpt-5.6-luna"', token_usage)
            self.assertIn('"/responses"', token_usage)

            session = [json.loads(line) for line in (fixture / "usage/aw_session.jsonl").read_text(encoding="utf-8").splitlines()]
            event_types = {record["type"] for record in session}
            self.assertIn("workflow.info", event_types)
            self.assertIn("firewall.token_usage", event_types)
            self.assertIn("tool.execution_start", event_types)
            self.assertNotIn("user.message", event_types)
            agent_session = [
                json.loads(line)
                for line in (fixture / "agent-session.jsonl").read_text(encoding="utf-8").splitlines()
            ]
            self.assertIn("tool.execution_start", {record["type"] for record in agent_session})


if __name__ == "__main__":
    unittest.main()
