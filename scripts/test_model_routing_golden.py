#!/usr/bin/env python3
"""Exercise model-routing fixture capture against a synthetic downloaded run."""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/model-routing-golden.py"
SOURCE = ROOT / "pkg/cli/testdata/model_routing_golden_capture"


class ModelRoutingGoldenCaptureTest(unittest.TestCase):
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
