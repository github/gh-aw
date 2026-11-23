import datetime as dt
import importlib.util
import pathlib
import unittest
import urllib.parse
from unittest.mock import patch
from types import SimpleNamespace

spec = importlib.util.spec_from_file_location("collector", pathlib.Path(__file__).with_name("collect.py"))
collector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(collector)


def run(identifier, created, path=".github/workflows/example.lock.yml"):
    return dict(id=identifier, created_at=created, path=path, name="example",
                head_sha="a" * 40, status="completed", conclusion="success",
                html_url=f"https://github.com/example/repo/actions/runs/{identifier}",
                run_attempt=1)


class CollectionTests(unittest.TestCase):
    def test_splits_ceiling_without_losing_boundary_seconds(self):
        start = collector.utc("2026-11-23T05:45:15Z")
        end = start + dt.timedelta(seconds=1)
        records = [run(i, collector.stamp(start if i < 700 else end)) for i in range(1200)]
        windows = []

        def request(endpoint):
            query = urllib.parse.parse_qs(endpoint.split("?")[1])
            left, right = map(collector.utc, query["created"][0].split(".."))
            windows.append((left, right))
            selected = [item for item in records if left <= collector.utc(item["created_at"]) <= right]
            page = int(query["page"][0])
            return {"total_count": len(selected), "workflow_runs": selected[(page - 1) * 100:page * 100]}

        result = collector.inventory("example/repo", start, end, request)
        self.assertEqual(len(result["runs"]), 1200)
        self.assertIn((start, start), windows)
        self.assertIn((end, end), windows)

    def test_excludes_ordinary_runs_and_detects_missing_pages(self):
        start = collector.utc("2026-11-23T05:45:15Z")
        request = lambda _: {"total_count": 2, "workflow_runs": [
            run(1, collector.stamp(start)), run(2, collector.stamp(start), "dynamic/copilot")
        ]}
        result = collector.inventory("example/repo", start, start, request)
        self.assertEqual(result["all_workflow_runs"], 2)
        self.assertEqual([item["id"] for item in result["runs"]], [1])
        with self.assertRaisesRegex(ValueError, "incomplete"):
            collector.inventory("example/repo", start, start, lambda _: {"total_count": 2, "workflow_runs": []})

    def test_stops_at_budget_and_unresolvable_ceiling(self):
        start = collector.utc("2026-11-23T05:45:15Z")
        with self.assertRaisesRegex(ValueError, "budget"):
            collector.inventory("example/repo", start, start, lambda _: {}, max_requests=0)
        with self.assertRaisesRegex(ValueError, "one second"):
            collector.inventory("example/repo", start, start, lambda _: {"total_count": 1001, "workflow_runs": []})

    def test_classifies_workflow_paths_with_optional_refs(self):
        start = collector.utc("2026-11-23T05:45:15Z")
        for path, included in (
            (".github/workflows/dispatcher.lock.yml", True),
            (".github/workflows/dispatcher.lock.yml@refs/heads/main", True),
            (".github/workflows/dispatcher.yml", False),
            (".github/workflows/dispatcher.yml@refs/heads/main", False),
            (".github/workflows/dispatcher.yml@refs/heads/example.lock.yml", False),
        ):
            with self.subTest(path=path):
                record = run(1, collector.stamp(start), path)
                result = collector.inventory(
                    "example/repo", start, start,
                    lambda _: {"total_count": 1, "workflow_runs": [record]},
                )
                self.assertEqual(result["all_workflow_runs"], 1)
                self.assertEqual(result["runs"], [record] if included else [])

    def test_pins_github_host_without_shell_or_credential_output(self):
        with patch.object(collector.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout="{}", stderr="")) as execute:
            self.assertEqual(collector.gh_request("repos/example/repo/actions/runs"), {})
            self.assertEqual(execute.call_args.args[0], ["gh", "api", "--hostname", "github.com", "repos/example/repo/actions/runs"])
            self.assertNotIn("shell", execute.call_args.kwargs)


if __name__ == "__main__":
    unittest.main()
