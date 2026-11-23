"""Inventory existing gh-aw runs without GitHub's 1,000-result truncation."""

import argparse
import datetime as dt
import json
import os
import pathlib
import re
import subprocess
import urllib.parse


class CollectionError(ValueError):
    """A safe, actionable diagnostic without source payloads."""


def utc(value):
    parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None or parsed.microsecond:
        raise CollectionError("Use timezone-qualified timestamps at whole-second precision")
    return parsed.astimezone(dt.timezone.utc)


def stamp(value):
    return value.isoformat().replace("+00:00", "Z")


def inventory(repo, start, end, request, max_requests=200):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        raise CollectionError("Repository must be an explicit github.com OWNER/REPO")
    if start > end:
        raise CollectionError("Start must not follow end")
    calls = 0
    runs = {}
    observed = set()

    def page(left, right, number):
        nonlocal calls
        if calls >= max_requests:
            raise CollectionError("API budget exhausted; inventory is incomplete")
        calls += 1
        query = urllib.parse.urlencode(
            {"created": f"{stamp(left)}..{stamp(right)}", "per_page": 100, "page": number}
        )
        result = request(f"repos/{repo}/actions/runs?{query}")
        if not isinstance(result.get("total_count"), int) or not isinstance(
            result.get("workflow_runs"), list
        ):
            raise CollectionError("Invalid Actions run-list response")
        return result

    def visit(left, right):
        first = page(left, right, 1)
        total = first["total_count"]
        if total > 1000:
            seconds = int((right - left).total_seconds())
            if seconds == 0:
                raise CollectionError("More than 1,000 runs in one second; incomplete inventory")
            middle = left + dt.timedelta(seconds=seconds // 2)
            visit(left, middle)
            visit(middle + dt.timedelta(seconds=1), right)
            return
        window_ids = set()
        for number in range(1, (total + 99) // 100 + 1):
            response = first if number == 1 else page(left, right, number)
            if response["total_count"] != total:
                raise CollectionError("Run inventory changed during pagination; recollect")
            for run in response["workflow_runs"]:
                created = utc(run["created_at"])
                if not left <= created <= right:
                    raise CollectionError("API returned a run outside the fixed window")
                window_ids.add(run["id"])
                observed.add(run["id"])
                if run["path"].endswith(".lock.yml"):
                    runs[run["id"]] = {
                        key: run[key]
                        for key in (
                            "id", "name", "path", "created_at", "head_sha",
                            "status", "conclusion", "html_url", "run_attempt",
                        )
                    }
        if len(window_ids) != total:
            raise CollectionError("Pagination returned incomplete membership; recollect")

    visit(start, end)
    return {
        "repository": repo,
        "start": stamp(start),
        "end": stamp(end),
        "coverage": "complete",
        "api_requests": calls,
        "all_workflow_runs": len(observed),
        "runs": sorted(runs.values(), key=lambda run: run["id"]),
    }


def gh_request(endpoint):
    result = subprocess.run(
        ["gh", "api", "--hostname", "github.com", endpoint], capture_output=True, text=True, timeout=120
    )
    if result.returncode:
        # Do not expose credentials or source-supplied API response bodies.
        status = re.search(r"HTTP (\d{3})", result.stderr)
        raise CollectionError(f"gh api failed{': HTTP ' + status[1] if status else ''}; collection stopped")
    return json.loads(result.stdout)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", required=True)
    parser.add_argument("--start", required=True)
    parser.add_argument("--end", required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--max-requests", type=int, default=200)
    args = parser.parse_args()
    if args.max_requests < 1:
        parser.error("--max-requests must be positive")
    result = inventory(args.repo, utc(args.start), utc(args.end), gh_request, args.max_requests)
    args.output.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as output:
        json.dump(result, output, separators=(",", ":"))
        output.write("\n")
    print(json.dumps({key: value for key, value in result.items() if key != "runs"} |
                     {"agentic_runs": len(result["runs"])}))


if __name__ == "__main__":
    try:
        main()
    except CollectionError as error:
        raise SystemExit(f"Inventory incomplete: {error}")
    except (ValueError, KeyError, OSError, subprocess.TimeoutExpired) as error:
        # Exception details can contain private API or filesystem values.
        raise SystemExit(f"Inventory failed ({type(error).__name__}); no complete report written")
