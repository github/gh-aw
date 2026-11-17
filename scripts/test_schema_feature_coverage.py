#!/usr/bin/env python3
"""Semantic and Git evidence tests for the offline schema coverage contract."""

import copy
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import schema_feature_coverage as measure


class CoverageTests(unittest.TestCase):
    def test_exact_lines_include_fences_but_not_nested_keys_or_prefixes(self):
        result = measure.coverage(
            {"runner": {}, "runner-extra": {}, "secret": {}, "a.b": {}},
            ["runner-extra: x\n  secret: nested\n```yaml\nrunner: {}\n```\naxb: no\n"],
        )
        self.assertEqual((result["covered"], result["total"]), (2, 4))
        self.assertEqual(result["missing"], ["a.b", "secret"])
        self.assertEqual(measure.coverage({"a.b": {}}, ["a.b: yes"])["covered"], 1)

    def test_missing_is_unavailable_not_zero_and_malformed_fails(self):
        for fields, documents in [({}, ["runner: {}"]), ({"runner": {}}, [])]:
            with self.subTest(fields=fields, documents=documents):
                result = measure.coverage(fields, documents)
                self.assertIsNone(result["covered"])
                self.assertIsNone(result["total"])
                self.assertIsNone(result["missing"])
                self.assertEqual(result["coverage_status"], "unavailable")
        self.assertEqual(measure.coverage({"runner": {}}, ["unrelated"])["covered"], 0)
        for fields in [None, [], {"": {}}]:
            with self.assertRaises(ValueError):
                measure.coverage(fields, ["runner: {}"])

    def test_retirement_is_not_an_example_and_regressions_remain_visible(self):
        before = measure.coverage({"added-example": {}, "retired": {}, "regression": {}}, ["regression: yes"])
        after = measure.coverage({"added-example": {}, "introduced": {}, "regression": {}}, ["added-example: yes\nintroduced: yes"])
        self.assertEqual(measure.merge_delta(before, after), {
            "newly_covered": ["added-example"], "coverage_lost": ["regression"],
            "schema_fields_added": ["introduced"], "schema_fields_removed": ["retired"],
        })

    def test_batch_windows_exclude_interval_and_eighth_endpoint(self):
        samples = measure.window_schedule("2026-05-18T13:37:21+01:00", "2026-05-18T13:57:51Z")
        self.assertEqual(len(samples), 14)
        self.assertEqual(samples[0]["at"], "2026-05-11T12:37:21+00:00")
        self.assertEqual(samples[6]["at"], "2026-05-17T12:37:21+00:00")
        self.assertEqual(samples[7]["at"], "2026-05-18T13:57:51+00:00")
        self.assertEqual(samples[-1]["at"], "2026-05-24T13:57:51+00:00")
        self.assertEqual([s["day"] for s in samples if s["period"] == "after"], list(range(7)))
        for first, last in [("2026-05-18", "2026-05-19Z"), ("2026-05-19T00:00:00Z", "2026-05-18T00:00:00Z")]:
            with self.assertRaises(ValueError):
                measure.window_schedule(first, last)


class RepositoryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.git("init", "--quiet")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.com")
        self.git("config", "commit.gpgsign", "false")
        self.write(measure.SCHEMA, '{"properties":{"alpha":{},"beta":{}}}')
        self.write(".github/workflows/shared/example.md", "alpha: yes\n")
        self.initial = self.commit("2026-05-01T00:00:00Z")
        self.git("mv", ".github/workflows/shared/example.md", "example.md")
        self.write("schema-demos/moved.md", "alpha: yes\n```yaml\nbeta: yes\n```\n")
        self.tip = self.commit("2026-05-18T13:57:51Z")

    def git(self, *args, **kwargs):
        return subprocess.check_output(["git", "-C", str(self.root), *args], **kwargs).decode().strip()

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")

    def commit(self, at):
        self.git("add", ".")
        self.git("commit", "--quiet", "-m", "fixture", env={**os.environ, "GIT_AUTHOR_DATE": at, "GIT_COMMITTER_DATE": at})
        return self.git("rev-parse", "HEAD")

    def test_relocation_pinned_history_and_regular_git_blobs(self):
        # Archive filters and symlinks must not substitute for the Git blob inventory.
        self.write(".gitattributes", "schema-demos/** export-ignore\n")
        (self.root / "schema-demos/link.md").symlink_to("../example.md")
        newer = self.commit("2026-05-19T00:00:00Z")
        repository = measure.Repository(self.root, self.tip)
        self.assertEqual(repository.at("2026-05-18T14:00:00Z"), self.tip)
        self.assertEqual(repository.measure(self.initial)["covered"], 1)
        self.assertEqual(repository.measure(self.tip)["covered"], 2)
        result = measure.Repository(self.root, newer).measure(newer)
        self.assertEqual(result["workflow_documents"], 1)
        self.assertEqual(result["source_hashes"]["schema-demos/moved.md"],
                         measure.digest((self.root / "schema-demos/moved.md").read_bytes()))
        with self.assertRaises(ValueError):
            repository.measure(newer)
        with self.assertRaises(ValueError):
            measure.Repository(self.root, "HEAD")

    def test_witness_requires_exact_first_parent_and_pinned_chain(self):
        repository = measure.Repository(self.root, self.tip)
        record = {"number": 1, "commit": self.tip, "parent": self.initial,
                  "merged_at": "2026-05-18T13:57:51Z", "expected": [1, 2, 2, 2]}
        witness = repository.witness(record)
        self.assertEqual(witness["newly_covered"], ["beta"])
        self.assertIsNone(witness["attributed_value"])
        with self.assertRaises(ValueError):
            repository.witness({**record, "parent": self.tip})
        self.git("checkout", "--quiet", "-b", "side", self.initial)
        self.write("side.md", "side")
        side = self.commit("2026-05-02T00:00:00Z")
        with self.assertRaises(ValueError):
            repository.witness({**record, "commit": side})

    def test_missing_schema_and_malformed_schema_do_not_score_as_clean(self):
        (self.root / measure.SCHEMA).unlink()
        missing = self.commit("2026-05-20T00:00:00Z")
        self.assertIsNone(measure.Repository(self.root, missing).measure(missing)["covered"])
        self.write(measure.SCHEMA, '{"not-properties":{}}')
        malformed = self.commit("2026-05-21T00:00:00Z")
        with self.assertRaises(ValueError):
            measure.Repository(self.root, malformed).measure(malformed)


class ReplayTests(unittest.TestCase):
    def test_cli_rejects_modified_frozen_contract(self):
        history = json.loads(measure.HISTORY.read_bytes())
        history["contract_sha256"] = "0" * 64
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "history.json"
            path.write_text(json.dumps(history), encoding="utf-8")
            with patch.object(measure, "HISTORY", path), patch("sys.argv", ["schema_feature_coverage.py"]):
                with self.assertRaisesRegex(ValueError, "Frozen contract_sha256 mismatch"):
                    measure.main()

    @unittest.skipUnless(os.environ.get("SCHEMA_COVERAGE_REPLAY") == "1", "requires full historical Git objects")
    def test_preserved_historical_replay_is_deterministic(self):
        history = json.loads(measure.HISTORY.read_bytes())
        repository = measure.Repository(Path(__file__).resolve().parents[1], history["main_tip"])
        result = measure.replay(repository, history)
        self.assertEqual(len(result["observations"]), 36)
        self.assertEqual(sum(len(row["samples"]) for row in result["windows"]), 84)
        cohort = result["retained_cohort"]["observations"]
        self.assertEqual([(r["covered"], r["total"]) for r in (cohort[0], cohort[-1])], [(30, 39), (39, 39)])
        self.assertEqual([r["example_gaps_closed"] for r in result["windows"]], [2, 7, 4, 5, 1, 1])
        self.assertEqual(result["windows"][1]["schema_fields_removed"], ["dependencies"])
        self.assertTrue(all(row["attributed_value"] is None for row in result["windows"]))
        self.assertEqual(result, measure.replay(repository, history))
        conflicting = copy.deepcopy(history)
        conflicting["daily_expected_sha256"] = "0" * 64
        with self.assertRaisesRegex(ValueError, "Daily replay disagrees"):
            measure.replay(repository, conflicting)


if __name__ == "__main__":
    unittest.main()
