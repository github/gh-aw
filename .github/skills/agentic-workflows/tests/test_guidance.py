"""Documentation contract regressions, not an LLM or live workflow evaluation."""

import json
from pathlib import Path
import unittest


SKILL = Path(__file__).resolve().parents[1]
ROOT = SKILL.parents[2]
GUIDES = (ROOT / ".github/aw/debug-agentic-workflow.md", ROOT / "debug.md")
FIXTURES = json.loads((SKILL / "tests/fixtures.json").read_text())
ANCHOR = "#model-and-engine-misconfiguration"


def checklist(text):
    return text.split("## Model and engine misconfiguration\n", 1)[1].split("\n## ", 1)[0]


class GuidanceTests(unittest.TestCase):
    def test_customer_case_remedy_order(self):
        case = FIXTURES["model_mismatch"]
        self.assertIn("v0.89.21", case["lock_header"])
        self.assertIn("model: auto", case["workflow"])
        self.assertIn("gpt-5.6-luna-utility", case["agent_stdio"])
        self.assertIn("Cannot translate Copilot request feature", case["agent_stdio"])
        for path in GUIDES:
            with self.subTest(path=path):
                text = checklist(path.read_text())
                positions = [text.index(fix) for fix in case["expected_fixes"]]
                self.assertEqual(positions, sorted(positions))
                self.assertLess(positions[-1], text.index(case["last_resort"]))
                self.assertLess(text.index(case["last_resort"]), text.index("model: gpt-4.1"))
                for required in (
                    "v0.89.21", "64177", "compiler_version", "cli_version",
                    "including prereleases", "latest non-prerelease",
                    "gh extension install github/gh-aw --force --pin",
                    "underlying misconfiguration in place",
                ):
                    self.assertIn(required, " ".join(text.split()))

    def test_wire_api_and_artifact_contract(self):
        for path in GUIDES:
            with self.subTest(path=path):
                text = checklist(path.read_text())
                precedence = text[text.index("precedence"):] if "precedence" in text else text[text.index("order:"):]
                positions = [precedence.index(term) for term in (
                    "override", "wire_api", "-utility", "gpt-5+", "CLI default",
                )]
                self.assertEqual(positions, sorted(positions))
                for required in (
                    "/responses", "/chat/completions", "whole session",
                    "sub-agents", "67460", "5103", "/reflect",
                    "supported_endpoints", "Model endpoint mismatch:",
                    "engine.copilot-sdk: true",
                    "Cannot translate Copilot request feature",
                    "Unsupported Responses custom tool", "model_policy_violation",
                    "not accessible via the … endpoint",
                    "agent-stdio.log", "[copilot-harness]",
                    "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl",
                    "requested_model", "awf_version", "aw_info.json", "gh aw audit RUN_ID",
                ):
                    self.assertIn(required, text)
                silent_case = FIXTURES["silent_subagent_fallback"]
                for required in (
                    silent_case["awf_version"], silent_case["error"],
                    silent_case["event"], silent_case["event_file"],
                    silent_case["audit_finding"], silent_case["request_status"],
                ):
                    self.assertIn(required, text)
                self.assertLess(
                    text.index("supported_endpoints"),
                    text.index("For the normal Copilot harness path")
                    if "For the normal Copilot harness path" in text
                    else text.index("normal CLI wire-API precedence"),
                )

    def test_misplaced_version_fields(self):
        case = FIXTURES["misplaced_versions"]
        for path in GUIDES:
            with self.subTest(path=path):
                text = checklist(path.read_text())
                for failure in case["failures"]:
                    field_start = text.index(f"`{failure['field']}`")
                    boundary = text.find("\n", field_start) if path == GUIDES[0] else text.index("point", field_start)
                    self.assertIn(failure["step"], text[field_start:boundary])
                self.assertIn("1.0.x", text)
                self.assertIn("vX.Y.Z", text)
                self.assertIn("gh-aw-firewall", text)
                self.assertIn(f"There is no `{case['invalid_field']}` field", text)
                self.assertIn("remove", text)
                self.assertIn("compiled default", text)

    def test_routing_and_evidence_links(self):
        # SKILL.md is regenerated from the embedded template by `gh aw init`,
        # so both must carry the routing line.
        for path in (SKILL / "SKILL.md", ROOT / "pkg/cli/data/agentic_workflows_skill.md"):
            with self.subTest(path=path):
                lines = [line for line in path.read_text().splitlines() if ANCHOR in line]
                self.assertTrue(lines, f"{path} must route to {ANCHOR}")
                line = lines[0]
                self.assertIn(f"(../../aw/debug-agentic-workflow.md{ANCHOR})", line)
                for symptom in (
                    "AWF model/endpoint 400s",
                    "silent cross-family sub-agent failures",
                    "`model: auto` failures",
                    "install-step 404s after a version pin",
                    "version fields",
                ):
                    self.assertIn(symptom, line)
        self.assertTrue((SKILL / "../../aw/debug-agentic-workflow.md").resolve().is_file())
        full = GUIDES[0].read_text()
        for section in ("Collect Existing Evidence", "Identify the First Failing Boundary"):
            content = full.split(f"## {section}\n", 1)[1].split("\n## ", 1)[0]
            self.assertIn(ANCHOR, content)
        self.assertIn(f".github/aw/debug-agentic-workflow.md{ANCHOR}", GUIDES[1].read_text())


if __name__ == "__main__":
    unittest.main()
