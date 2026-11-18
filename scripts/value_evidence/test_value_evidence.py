import copy
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from collect import DEAD, DEFS, REPO, ROOT, collect, endpoints_for_run, observe_pair, selected_runs
from measure import FIXTURE, MODULE, Unavailable, benchmark, command, eligible_functions
from triage import join


AT = datetime(2026, 9, 1, 10, tzinfo=timezone.utc)
RUN = dict(id=42, run_attempt=1, html_url=f"https://github.com/{REPO}/actions/runs/42", head_sha="a" * 40,
           created_at=AT.isoformat(), path=f".github/workflows/{DEAD}.lock.yml", status="completed", conclusion="skipped",
           repository={"full_name": REPO}, head_repository={"full_name": REPO}, head_branch="main", event="schedule")


class EvidenceTests(unittest.TestCase):
    def test_cohort_includes_noops_and_rejects_incomplete_identity(self):
        def select(runs, total):
            return selected_runs({"workflow_runs": runs, "total_count": total}, DEAD, "main", AT.date())
        self.assertEqual(select([RUN], 1)[0]["conclusion"], "skipped")
        self.assertEqual(select([dict(RUN, head_repository={"full_name": "other/repo"})], 1), [])
        for rows, total in (([RUN], 2), ([RUN, RUN], 2), ([dict(RUN, path="another.lock.yml")], 1),
                            ([dict(RUN, html_url="https://github.com/elsewhere")], 1)):
            with self.assertRaises(ValueError):
                select(rows, total)
        ends = endpoints_for_run(None, RUN, AT, {}, None, lambda _: self.fail("untrusted SHA executed"))
        self.assertEqual(ends[0]["error"], "source_head_not_on_trusted_first_parent")

    def test_conservative_independent_inventory(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "main.go").write_text("func gone() {}\nfunc wasmOnly() {}\nfunc extractJobSection() {}\n")
            (root / "other_wasm.go").write_text("func run() { wasmOnly() }")
            (root / "old_test.go").write_text("func TestOld() { gone() }")
            entries = [{"Name": n, "Position": {"File": "main.go"}, "Generated": False}
                       for n in ("gone", "wasmOnly", "extractJobSection", "Receiver.method")]
            self.assertEqual(eligible_functions(root, [{"Path": MODULE, "Funcs": entries}]), [MODULE + ".gone"])
            self.assertEqual(eligible_functions(root, None), [])
            entries[0]["Position"]["File"] = "../outside.go"
            with self.assertRaises(Unavailable):
                eligible_functions(root, [{"Path": MODULE, "Funcs": entries}])

    def test_fixed_population_zero_and_unavailable(self):
        definition = json.loads((DEFS / "prospective" / f"{DEAD}.json").read_bytes())
        def rows(before, after):
            return observe_pair(DEAD, definition, RUN, AT, AT + timedelta(days=7), [before, after], "fixture")
        pair = rows({"candidate_ids": ["one", "two"]}, {"candidate_ids": ["two", "new"]})
        self.assertEqual([r["values"] for r in pair], [[2], [1]])
        self.assertEqual(pair[0]["population"], pair[1]["population"])
        zero = rows({"candidate_ids": []}, {"candidate_ids": ["new"]})
        self.assertEqual(zero[0]["values"], [0])
        failed = rows({"error": "analysis_failed"}, {"candidate_ids": []})
        self.assertTrue(all(r["coverage"] == "unavailable" and "values" not in r for r in failed))

    def test_benchmark_rejects_missing_or_incomparable_replicates(self):
        digest = hashlib.sha256((ROOT / FIXTURE).read_bytes()).hexdigest()
        line = "BenchmarkCompileSimpleWorkflow-1  100  12345 ns/op\n"
        with patch("measure.command", return_value=line * 5):
            self.assertEqual(benchmark(ROOT, digest)["ns_per_op"], [12345] * 5)
        with patch("measure.command", return_value=line * 4), self.assertRaises(Unavailable):
            benchmark(ROOT, digest)
        with self.assertRaises(Unavailable):
            benchmark(ROOT, "changed")

    def test_collection_selects_fixed_followup_not_latest_commit(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            def git(*args, **env):
                return subprocess.check_output(["git", "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
                                                *args], cwd=root, env={**os.environ, **env}, text=True).strip()
            git("init", "-q", "-b", "main")
            git("remote", "add", "origin", "https://github.com/" + REPO)
            shas = []
            for day, content in ((0, "a b"), (7, "a c"), (8, "none")):
                date = (AT + timedelta(days=day, minutes=-10)).isoformat()
                (root / "candidates").write_text(content)
                git("add", "candidates")
                git("commit", "-qm", "test snapshot", "-m",
                    "Co-authored-by: copilot-swe-agent[bot] <198982749+Copilot@users.noreply.github.com>\n"
                    "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>",
                    GIT_AUTHOR_DATE=date, GIT_COMMITTER_DATE=date)
                shas.append(git("rev-parse", "HEAD"))
            git("update-ref", "refs/remotes/origin/main", shas[-1])
            runs = root / "runs.json"
            runs.write_text(json.dumps({"total_count": 1, "workflow_runs": [dict(RUN, head_sha=shas[0])]}))
            args = SimpleNamespace(workflow=DEAD, runs=runs, output=root / "artifact", day=AT.date(), start=AT.date(),
                                   branch="main", collector="synthetic", deadcode=root / "tool")
            def commands(args, cwd):
                if args[:2] == ["go", "version"]:
                    return ("path\tgolang.org/x/tools/cmd/deadcode\nmod\tgolang.org/x/tools\tv0.52.0\ttest\n"
                            if "-m" in args else "go version go1.27.4 linux/amd64\n")
                return command(args, cwd)
            with patch("collect.ROOT", root), patch("collect.command", side_effect=commands), patch(
                "collect.deadcode", side_effect=lambda w, _: {"candidate_ids": (w / "candidates").read_text().split()}
            ):
                self.assertEqual(collect(args), 0)
            data = json.loads((args.output / "observations.json").read_bytes())["observations"]
            self.assertEqual([o["sha"] for o in data], shas[:2])
            self.assertEqual([o["values"] for o in data], [[2], [1]])
            if (ROOT / "gh-aw").is_file():
                raw = subprocess.check_output([ROOT / "gh-aw", "value", DEAD, "--repo", REPO, "--value-function",
                                               args.output / "definition.json", "--recipe", args.output / "recipe.json",
                                               "--evidence", args.output / "observations.json", "--logs-dir", tmp, "--json"])
                self.assertIsNone(json.loads(raw)["attributed_value"])

    def test_blind_reference_and_missing_followup(self):
        raw = json.dumps(dict(repository="github.com/" + REPO, workflow="issue-triage-agent", run_id=42, run_attempt=1,
                              at=AT.isoformat(), observed_at=AT.isoformat(), reference="archive:cohort", complete=True,
                              eligible_issues=[{"number": n, "labels": [], "assignees": []} for n in (1, 2)])).encode()
        digest = hashlib.sha256(raw).hexdigest()
        review = dict(cohort_sha256=digest, reviewed_at=AT.isoformat(), reference="archive:review", complete=True,
                      blind_human_review=True, classifications={"1": "bug", "2": "documentation"})
        end = (AT + timedelta(days=14)).isoformat()
        follow = dict(cohort_sha256=digest, at=end, observed_at=end, reference="archive:labels", complete=True,
                      labels={"1": ["bug"], "2": []})
        observations = join(raw, review, follow)["observations"]
        self.assertEqual([r["values"] for r in observations], [[0], [1]])
        self.assertEqual(len(observations[0]["population"]), 2)
        for variant in (dict(follow, complete=False), dict(follow, labels={"1": ["bug"]}),
                        dict(follow, labels={"1": ["bug", "question"], "2": []})):
            self.assertNotIn("values", join(raw, review, variant)["observations"][1])
        with self.assertRaisesRegex(ValueError, "exact sealed cohort"):
            join(raw, dict(review, cohort_sha256="wrong"), follow)
        self.assertNotIn("values", join(raw, dict(review, blind_human_review=False), follow)["observations"][1])

    @unittest.skipUnless((ROOT / "gh-aw").is_file(), "build gh-aw to test end-to-end rendering")
    def test_production_reader(self):
        path = DEFS / "prospective" / f"{DEAD}.json"
        definition = json.loads(path.read_bytes())
        endpoints = [{"sha": c * 40, "candidate_ids": ids} for c, ids in (("a", ["one", "two"]), ("b", ["two", "new"]))]
        observations = observe_pair(DEAD, definition, RUN, AT, AT + timedelta(days=7), endpoints, "synthetic")
        with tempfile.TemporaryDirectory() as tmp:
            evidence = Path(tmp) / "observations.json"
            def read(rows):
                evidence.write_text(json.dumps({"version": 1, "repository": "github.com/" + REPO, "observations": rows}))
                raw = subprocess.check_output([ROOT / "gh-aw", "value", DEAD, "--repo", REPO, "--value-function", path,
                                               "--evidence", evidence, "--logs-dir", tmp, "--start", "2026-08-31",
                                               "--end", "2026-09-09", "--json"])
                return json.loads(raw)
            result = read(observations)
            self.assertEqual(result["evidence"]["changes"][0]["change"]["value"], 1)
            self.assertIsNone(result["attributed_value"])
            missing = copy.deepcopy(observations)
            missing[1].update(coverage="unavailable", reason="analysis_failed")
            del missing[1]["values"]
            self.assertIsNone(read(missing)["evidence"]["changes"][0]["change"]["value"])
