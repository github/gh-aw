"""Exercise the composed registry with the pinned runner's actual Weaver policies."""

import copy
import json
import subprocess
import sys
import unittest
from pathlib import Path

from genai_conformance import DOMAIN

REGISTRY = Path(sys.argv.pop(1)).resolve()
FIXTURES = Path(__file__).resolve().parents[1] / "fixtures" / "genai"


def attributes(values):
    return [{"name": name, "value": value} for name, value in values.items()]


def findings(value):
    if isinstance(value, dict):
        for advice in value.get("all_advice", []):
            if advice["level"] == "violation":
                yield advice
        for child in value.values():
            yield from findings(child)
    elif isinstance(value, list):
        for child in value:
            yield from findings(child)


class GenAIRegistryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults = DOMAIN.weaver_defaults(REGISTRY)
        cls.command = [
            "weaver", "registry", "live-check",
            "--registry", str(REGISTRY),
            "--config", str(FIXTURES / "weaver.toml"),
            "--advice-policies", defaults.policies,
            "--advice-data", defaults.advice_data,
            "--input-source", "stdin",
            "--format", "json",
            "--no-stream", "--no-stats", "--quiet",
        ]
        cls.mixed = [
            {"resource": {"attributes": attributes({
                "service.name": "gh-aw",
                "service.version": "test",
                "cicd.pipeline.name": "conformance",
                "vcs.provider.name": "github",
                "github.run_id": "123",
                "runner.os": "Linux",
                "gh-aw.workflow.name": "conformance",
            })}},
            {"span": {
                "name": "invoke_agent conformance",
                "kind": "internal",
                "attributes": attributes({
                    "gen_ai.operation.name": "invoke_agent",
                    "gen_ai.agent.name": "conformance",
                    "gen_ai.provider.name": "github",
                    "gh-aw.job.name": "agent",
                    "gh-aw.error_count": 0,
                    "gh-aw.run.attempt": "1",
                }),
            }},
            {"metric": {
                "name": "gen_ai.client.inference.usage.input_tokens",
                "instrument": "counter",
                "unit": "{token}",
                "data_points": [{"attributes": attributes({
                    "gen_ai.operation.name": "chat",
                    "gen_ai.provider.name": "github",
                    "gen_ai.token.modality": "text",
                    "gh-aw.job.name": "agent",
                }), "value": 100}],
            }},
            {"metric": {
                "name": "gh_aw.job.errors",
                "instrument": "counter",
                "unit": "{job}",
                "data_points": [{"attributes": attributes({
                    "gh-aw.job.name": "agent",
                    "cicd.pipeline.task.run.result": "failure",
                }), "value": 1}],
            }},
        ]

    def check(self, samples):
        result = subprocess.run(
            self.command, input=json.dumps(samples), capture_output=True, text=True,
            timeout=60, check=False,
        )
        self.assertIn(result.returncode, (0, 1), result.stderr)
        try:
            return list(findings(json.loads(result.stdout)))
        except json.JSONDecodeError:
            self.fail(result.stderr or result.stdout)

    def test_valid_mixed_telemetry(self):
        self.assertEqual(self.check(self.mixed), [])

    def test_invalid_attributes_are_not_suppressed(self):
        for name, value, expected in [
            ("gen_ai.provider.name", True, "type_mismatch"),
            ("gen_ai.agent.name", 42, "type_mismatch"),
            ("gen_ai.usage.input_tokens", "bad", "type_mismatch"),
            ("gen_ai.unknown_field", "bad", "missing_attribute"),
            ("gh-aw.unknown_field", "bad", "missing_attribute"),
            ("github.unknown_field", "bad", "missing_attribute"),
            ("gh-aw.error_count", "bad", "type_mismatch"),
            ("cicd.pipeline.name", 42, "type_mismatch"),
        ]:
            with self.subTest(attribute=name):
                samples = [{"attribute": {"name": name, "value": value}}]
                advice = self.check(samples)
                self.assertIn(expected, [finding["id"] for finding in advice])

    def test_unknown_metrics_and_invalid_instruments_still_fail(self):
        for name, instrument, unit, expected in [
            ("gh_aw.unknown_metric", "counter", "{job}", "missing_metric"),
            ("gen_ai.unknown_metric", "counter", "{token}", "missing_metric"),
            ("gh_aw.job.errors", "histogram", "{job}", "unexpected_instrument"),
            ("gh_aw.job.errors", "counter", "s", "unit_mismatch"),
            ("gen_ai.client.inference.usage.input_tokens", "counter", "s", "unit_mismatch"),
        ]:
            with self.subTest(metric=name, instrument=instrument, unit=unit):
                sample = copy.deepcopy(self.mixed[-1])
                sample["metric"].update(name=name, instrument=instrument, unit=unit)
                self.assertIn(expected, [finding["id"] for finding in self.check([sample])])

    def test_missing_required_genai_metric_attribute_still_fails(self):
        sample = copy.deepcopy(self.mixed[2])
        sample["metric"]["data_points"][0]["attributes"] = attributes({
            "gen_ai.operation.name": "chat",
            "gen_ai.token.modality": "text",
        })
        self.assertIn(
            "required_attribute_not_present",
            [finding["id"] for finding in self.check([sample])],
        )

    def test_invalid_genai_span_shape_still_fails(self):
        sample = copy.deepcopy(self.mixed[1])
        sample["span"]["name"] = "not an invoke_agent span"
        self.assertIn(
            "genai_span_name_format",
            [finding["id"] for finding in self.check([sample])],
        )


if __name__ == "__main__":
    unittest.main()
