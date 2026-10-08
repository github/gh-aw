"""Unit, property, rendering, CLI and independent-source regression tests."""

from __future__ import annotations

from copy import deepcopy
from datetime import datetime, timedelta, timezone
from decimal import Decimal, ROUND_HALF_UP, localcontext
from fractions import Fraction
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from hypothesis import given, settings, strategies as st
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

from audit_sources import audit_api, audit_git, percentile
from render_infographic import Canvas, LayoutError, discover_fonts, render, save
from statistics_data import DataError, Outcome, Statistics, decimal, percent


def example():
    return json.loads((ROOT / "examples/example.json").read_text())


class ArithmeticTests(unittest.TestCase):
    @settings(max_examples=200, derandomize=True, database=None)
    @given(st.integers(0, 10**18), st.integers(1, 10**18), st.integers(0, 4))
    def test_rounding_matches_independent_decimal_oracle(self, numerator, denominator, places):
        with localcontext() as context:
            context.prec = 100
            reference = (Decimal(numerator) / Decimal(denominator)).quantize(
                Decimal(1).scaleb(-places), rounding=ROUND_HALF_UP)
            self.assertEqual(decimal(Fraction(numerator, denominator), places), f"{reference:.{places}f}")

    @settings(max_examples=150, derandomize=True, database=None)
    @given(st.integers(0, 10**9), st.integers(0, 10**9), st.integers(0, 10**9), st.integers(1, 100))
    def test_outcomes_scale_without_changing_rates(self, merged, closed, opened, factor):
        a = Outcome("test", merged, closed, opened)
        b = Outcome("test", merged * factor, closed * factor, opened * factor)
        self.assertEqual(a.merge_rate, b.merge_rate)
        self.assertEqual(a.ratio, b.ratio)
        self.assertEqual(a.total * factor, b.total)
        self.assertEqual(a.merge_rate, percent(merged, merged + closed))

    @settings(max_examples=100, derandomize=True, database=None)
    @given(st.lists(st.integers(0, 10**12), min_size=1, max_size=100), st.integers(0, 100))
    def test_interpolated_quantiles_match_decimal_oracle(self, values, percentile_int):
        values.sort()
        q = Fraction(percentile_int, 100)
        position = Decimal(len(values) - 1) * Decimal(percentile_int) / 100
        i = int(position)
        reference = Decimal(values[i]) + Decimal(values[min(i + 1, len(values) - 1)] - values[i]) * (position - i)
        actual = percentile(values, q)
        self.assertEqual(Decimal(actual.numerator) / Decimal(actual.denominator), reference)

    def test_undefined_denominators_and_half_up_ties(self):
        self.assertEqual(percent(0, 0), "n/a")
        self.assertEqual(Outcome("empty", 0, 0, 3).merge_rate, "n/a")
        self.assertEqual(Outcome("merged", 3, 0, 0).ratio, "n/a")
        self.assertEqual(percent(1, 16), "6.3%")
        self.assertEqual(decimal(Fraction(-125, 100), 1), "-1.3")
        with self.assertRaises(DataError):
            percent(1, 0)


class ContractTests(unittest.TestCase):
    def test_example_and_immutable_components(self):
        s = Statistics.from_sources(example())
        self.assertEqual(s.all_prs.total, 100)
        self.assertEqual(s.all_prs.merged, 73)
        self.assertEqual(s.under(6), 55)
        self.assertEqual(s.all_prs.merge_rate, "75.3%")
        self.assertEqual(s.all_prs.ratio, "3.04 : 1")
        with self.assertRaises(TypeError):
            s.components["direct_aw_prs"] = 0

    def test_reject_invalid_inputs(self):
        changes = [
            ("origins", "total_prs", 101), ("origins", "total_prs", True),
            ("histogram", "merged_pr_count", 74),
            ("histogram", "snapshot_date", "2026-10-07"),
            ("community", "repository", "other/repo"),
            ("community", "third_party_issues", 31),
            ("community", "third_party_author_accounts", 31),
            ("community", "copilot_authored_delivery_prs", 19),
            ("community", "collected_at", "2026-10-08T12:00:00"),
            ("structure", "commit", "short"),
            ("structure", "nonblank_lines", -1),
            ("structure", "included_files", 76),
            ("history", "total_added", 34001),
            ("history", "week_count", 3),
        ]
        for section, field, value in changes:
            with self.subTest(section=section, field=field), self.assertRaises(ValueError):
                payload = example()
                payload[section][field] = value
                Statistics.from_sources(payload)

    def test_reject_bad_nested_shapes_and_stale_percentages(self):
        for value in (False, -1, 1.5, "10", float("nan"), float("inf")):
            with self.subTest(value=value), self.assertRaises(DataError):
                payload = example()
                payload["histogram"]["histogram"][0]["count"] = value
                Statistics.from_sources(payload)
        for field, value in (("median", 1000), ("p90", 1), ("p90", 10000)):
            with self.subTest(field=field), self.assertRaises(DataError):
                payload = example()
                payload["histogram"]["summary_hours"][field] = value
                Statistics.from_sources(payload)
        payload = example()
        payload["origins"]["breakdown"][0]["percent"] = 90
        with self.assertRaises(DataError):
            Statistics.from_sources(payload)
        payload = example()
        payload["origins"]["outcomes"][0]["origin"] = []
        with self.assertRaises(DataError):
            Statistics.from_sources(payload)
        payload = example()
        payload["origins"]["outcomes"].append(
            {"origin": "All PRs", "merged": 73.0, "closed_without_merging": 24, "open": 3})
        with self.assertRaises(DataError):
            Statistics.from_sources(payload)

    def test_reject_gaps_duplicates_and_unsorted_weeks(self):
        mutations = [
            lambda p: p["histogram"]["histogram"][1].update(lower_hours_inclusive=.3),
            lambda p: p["histogram"]["histogram"][0].update(upper_hours_exclusive=0),
            lambda p: p["histogram"]["histogram"][1].update(bin="<15 min"),
            lambda p: p["origins"]["outcomes"].append(deepcopy(p["origins"]["outcomes"][0])),
            lambda p: p["structure"]["groups"].__setitem__(1, deepcopy(p["structure"]["groups"][0])),
            lambda p: p["history"]["weeks"].reverse(),
            lambda p: p["history"]["weeks"][0].update(week_start="2026-09-29"),
        ]
        for mutate in mutations:
            with self.assertRaises(DataError):
                payload = example()
                mutate(payload)
                Statistics.from_sources(payload)

    def test_optimized_python_does_not_disable_validation(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "bad.json"
            payload = example()
            payload["origins"]["total_prs"] = 101
            source.write_text(json.dumps(payload))
            result = subprocess.run(
                [sys.executable, "-O", str(ROOT / "scripts/render_infographic.py"),
                 "--input", str(source), "--output", str(Path(directory) / "out.png")],
                capture_output=True, text=True,
            )
            self.assertEqual(result.returncode, 2)
            self.assertIn("PR totals", result.stderr)
            self.assertFalse((Path(directory) / "out.png").exists())


class RenderingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fonts = discover_fonts(None, None)

    def test_example_png_and_deterministic_pixels(self):
        s = Statistics.from_sources(example())
        a, b = render(s, self.fonts), render(s, self.fonts)
        self.assertEqual(a.image.tobytes(), b.image.tobytes())
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "nested/result.png"
            save(a, output)
            with Image.open(output) as image:
                self.assertEqual(image.size, (2560, 3200))
                self.assertEqual(image.format, "PNG")
                self.assertFalse(getattr(image, "is_animated", False))
            self.assertEqual(list(output.parent.iterdir()), [output])

    def test_zero_community_single_language_and_one_zero_week(self):
        payload = example()
        for key in ("third_party_issues", "third_party_author_accounts", "third_party_issues_landed_main",
                    "closed_without_merged_main_link", "third_party_open_issues",
                    "unique_merged_main_delivery_prs", "copilot_authored_delivery_prs"):
            payload["community"][key] = 0
        for group in payload["structure"]["groups"][2:]:
            group["files"] = group["nonblank_lines"] = 0
        payload["structure"].update(included_files=50, nonblank_lines=14000)
        payload["history"].update(week_count=1, total_added=0, total_deleted=0)
        payload["history"]["weeks"] = [{"week_start": "2026-10-05", "added": 0, "deleted": 0}]
        canvas = render(Statistics.from_sources(payload), self.fonts)
        canvas.verify()
        self.assertTrue(any(label == "n/a" for _, label in canvas.labels))
        self.assertTrue(any(label == "1 week" for _, label in canvas.labels))

    def test_layout_errors_and_explicit_font_errors(self):
        with self.assertRaises(LayoutError):
            render(Statistics.from_sources(example()), self.fonts, "word " * 100)
        c = Canvas(self.fonts)
        c.text("first", 100, 100)
        c.text("second", 100, 100)
        with self.assertRaises(LayoutError):
            c.verify()
        with self.assertRaises(LayoutError):
            discover_fonts(Path("missing.ttf"), None)
        with self.assertRaises(LayoutError):
            discover_fonts(Path("missing.ttf"), Path("missing-bold.ttf"))

    def test_discovery_uses_existing_font_pair(self):
        with patch("render_infographic.Path.is_file", return_value=False), self.assertRaises(LayoutError):
            discover_fonts(None, None)
        self.assertEqual(discover_fonts(*self.fonts), self.fonts)

    def test_cli_success_and_failure_preserves_output(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "result.png"
            command = [sys.executable, str(ROOT / "scripts/render_infographic.py"),
                       "--output", str(output), "--input", str(ROOT / "examples/example.json"),
                       "--font-regular", str(self.fonts[0]), "--font-bold", str(self.fonts[1])]
            success = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(success.returncode, 0, success.stderr)
            original = output.read_bytes()
            source = Path(directory) / "invalid.json"
            for contents in ("{", '{"origins": null}'):
                source.write_text(contents)
                result = subprocess.run(command[:-4] + ["--input", str(source)], capture_output=True, text=True)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertEqual(output.read_bytes(), original)
            self.assertEqual(sorted(p.name for p in output.parent.iterdir()), ["invalid.json", "result.png"])


class ApiAuditTests(unittest.TestCase):
    def fixture(self, raw):
        payload = example()
        prs, links = [], []
        durations = iter([5/60] * 10 + [.5] * 20 + [2] * 25 + [10] * 10 + [48] * 4 + [96] * 2 + [240] * 2)
        created = datetime(2026, 9, 1, tzinfo=timezone.utc)
        issues = [
            {"number": 1, "repository": {"nameWithOwner": "example/metrics"},
             "author": {"login": "github-actions", "__typename": "Bot"},
             "body": "<!-- gh-aw-workflow-id: sample -->"},
            {"number": 2, "repository": {"nameWithOwner": "example/metrics"},
             "author": {"login": "contributor", "__typename": "User"}, "body": ""},
        ]
        connection = lambda values: {"totalCount": len(values), "nodes": values}
        for origin, outcome in enumerate(payload["origins"]["outcomes"]):
            states = ["MERGED"] * outcome["merged"] + ["CLOSED"] * outcome["closed_without_merging"] + ["OPEN"] * outcome["open"]
            for index, state in enumerate(states):
                direct = origin == 0 and index < 15
                login = "github-actions" if direct else ("copilot-swe-agent" if origin < 2 else "maintainer")
                body = "<!-- gh-aw-workflow-id: sample -->" if direct else ""
                references = [issues[0]] if origin == 0 and not direct else ([issues[1]] if origin == 1 and index < 10 else [])
                if references:
                    body = f"<!-- START COPILOT CODING AGENT SUFFIX -->\nFixes #{references[0]['number']}"
                pr = {"number": 1000 + len(prs), "state": state, "author": {"login": login},
                      "body": body, "createdAt": created.isoformat(),
                      "mergedAt": (created + timedelta(hours=next(durations))).isoformat() if state == "MERGED" else None}
                prs.append(pr)
                if login == "copilot-swe-agent":
                    links.append({"number": pr["number"], "closingIssuesReferences": connection(references)})
        # A contradictory closing link must not override the explicit CCA suffix.
        links[0]["closingIssuesReferences"] = connection(issues)
        delivery = [p for p in prs if p["state"] == "MERGED" and p["author"]["login"] == "copilot-swe-agent"][:16] + prs[:2]
        community = []
        for i in range(40):
            matches = [dict(delivery[i % 18], baseRefName="main", repository={"nameWithOwner": "example/metrics"})] if i < 20 else []
            community.append({"number": 50000 + i, "state": "OPEN" if i == 29 else "CLOSED",
                              "author": {"login": f"reporter-{i % 12}", "__typename": "User"},
                              "authorAssociation": "NONE" if i < 30 else "MEMBER",
                              "closedByPullRequestsReferences": connection(matches)})
        records = {"pr-statistics-data.jsonl": prs, "cca-issue-links.jsonl": links,
                   "community-issues-data.jsonl": community}
        for name, values in records.items():
            (raw / name).write_text("\n".join(json.dumps(value) for value in values))
        return Statistics.from_sources(payload), records

    def test_reconciliation_and_corruption_detection(self):
        with tempfile.TemporaryDirectory() as directory:
            raw = Path(directory)
            stats, records = self.fixture(raw)
            audit_api(stats, raw)
            mutations = [
                ("pr-statistics-data.jsonl", lambda r: r.append(deepcopy(r[0]))),
                ("pr-statistics-data.jsonl", lambda r: r[0].update(mergedAt="2026-09-03T00:00:00Z")),
                ("pr-statistics-data.jsonl", lambda r: r[0].update(state="OPEN")),
                ("pr-statistics-data.jsonl", lambda r: r[0].update(createdAt="2026-10-09T00:00:00Z")),
                ("cca-issue-links.jsonl", lambda r: r[0]["closingIssuesReferences"].update(totalCount=3)),
                ("community-issues-data.jsonl", lambda r: r[20].update(state="UNKNOWN")),
                ("community-issues-data.jsonl", lambda r: r[18]["closedByPullRequestsReferences"]["nodes"][0].update(author={"login": "different"})),
            ]
            for name, mutate in mutations:
                changed = deepcopy(records[name])
                mutate(changed)
                path = raw / name
                path.write_text("\n".join(json.dumps(value) for value in changed))
                with self.subTest(source=name), self.assertRaises(DataError):
                    audit_api(stats, raw)
                path.write_text("\n".join(json.dumps(value) for value in records[name]))


class GitAuditTests(unittest.TestCase):
    def test_immutable_commit_even_when_worktree_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            env = {**os.environ, "GIT_AUTHOR_DATE": "2026-09-28T12:00:00Z",
                   "GIT_COMMITTER_DATE": "2026-09-28T12:00:00Z"}
            def run(*args):
                return subprocess.check_output(
                    ["git", "-C", str(repo), "-c", "user.name=Test", "-c", "user.email=test@example.invalid", *args],
                    env=env,
                )
            run("init", "-q")
            files = {"main.go": "package main\n\nfunc main() {}\n",
                     "main_test.go": "package main\nfunc TestX() {}\n",
                     "main.js": "export const x = 1;\n", "main.test.js": "test('x',()=>{});\n"}
            for name, value in files.items():
                (repo / name).write_text(value)
            run("add", ".")
            run("commit", "-q", "-m", "initial", "--no-gpg-sign")
            env.update(GIT_AUTHOR_DATE="2026-10-05T12:00:00Z", GIT_COMMITTER_DATE="2026-10-05T12:00:00Z")
            (repo / "main.go").write_text(files["main.go"] + "// another line\n")
            run("add", ".")
            run("commit", "-q", "-m", "second", "--no-gpg-sign")
            commit = run("rev-parse", "HEAD").decode().strip()
            payload = example()
            payload["structure"].update(commit=commit, included_files=4, nonblank_lines=7)
            for group, lines in zip(payload["structure"]["groups"], (3, 2, 1, 1)):
                group.update(files=1, nonblank_lines=lines)
            payload["history"].update(commit=commit, total_added=8, total_deleted=0)
            payload["history"]["weeks"] = [
                {"week_start": "2026-09-28", "added": 7, "deleted": 0},
                {"week_start": "2026-10-05", "added": 1, "deleted": 0},
            ]
            s = Statistics.from_sources(payload)
            (repo / "main.go").write_text("uncommitted content\n")
            audit_git(s, repo)
            payload["history"]["weeks"][1]["added"] = 2
            payload["history"]["total_added"] = 9
            with self.assertRaises(DataError):
                audit_git(Statistics.from_sources(payload), repo)

    def test_shallow_history_is_rejected_without_fetch(self):
        with patch("audit_sources.git", return_value=b"true\n") as command:
            with self.assertRaises(DataError):
                audit_git(Statistics.from_sources(example()), Path("."))
            self.assertEqual(command.call_count, 1)


if __name__ == "__main__":
    unittest.main()
