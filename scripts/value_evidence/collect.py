#!/usr/bin/env python3
"""Collect a mature UTC day's Git evidence, without executing the source workflow or a grader."""

import argparse
from contextlib import contextmanager
from datetime import date, datetime, time, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import tempfile

from measure import ANALYZER, FIXTURE, TOOLCHAIN, Unavailable, benchmark, command, deadcode


ROOT = Path(__file__).resolve().parents[2]
DEFS = ROOT / ".github/aw/value"
REPO = "github/gh-aw"
DEAD = "dead-code-remover"
DAYS = {DEAD: 7, "daily-cli-performance": 14}
UTC = timezone.utc


def stamp(dt):
    return dt.astimezone(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")


def selected_runs(page, workflow, branch, day):
    runs = page["workflow_runs"]
    if not isinstance(runs, list) or type(page["total_count"]) is not int or page["total_count"] != len(runs) or len(runs) > 100:
        raise ValueError("incomplete daily run listing; never sample a partial page")
    selected, seen = [], set()
    for r in runs:
        run_id = r["id"]
        at = datetime.fromisoformat(r["created_at"])
        if (type(run_id) is not int or run_id < 1 or run_id in seen or type(r["run_attempt"]) is not int or r["run_attempt"] < 1
                or r["repository"]["full_name"] != REPO or r["head_branch"] != branch
                or r["path"] != f".github/workflows/{workflow}.lock.yml" or at.tzinfo is None or at.astimezone(UTC).date() != day
                or r["html_url"] != f"https://github.com/{REPO}/actions/runs/{run_id}" or not re.fullmatch(r"[a-f0-9]{40}", r["head_sha"])):
            raise ValueError("source run has missing, duplicate or conflicting cohort identity")
        seen.add(run_id)
        if r["event"] in ("schedule", "workflow_dispatch") and r["head_repository"]["full_name"] == REPO:
            selected.append({k: r[k] for k in ("id", "run_attempt", "html_url", "head_sha", "created_at", "event", "status", "conclusion")})
    return sorted(selected, key=lambda r: r["id"])


@contextmanager
def snapshot(repo, sha):
    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp) / "snapshot"
        command(["git", "worktree", "add", "--quiet", "--detach", work, sha], repo)
        try:
            yield work
        finally:
            command(["git", "worktree", "remove", "--force", work], repo)


def observe_pair(workflow, definition, run, at, due, endpoints, context):
    before = endpoints[0]
    rows = []
    for phase, event, endpoint in zip(("baseline", "outcome"), (at, due), endpoints, strict=True):
        run_url = run["html_url"]
        scope, subject = definition["config"]["scope"], f"{run_url}/attempts/{run['run_attempt']}"
        row = dict(id=f"{workflow}-{run['id']}-{run['run_attempt']}-{phase}", workflow=workflow, metric=definition["id"],
                   scope=scope, run_id=run["id"], subject=subject, at=stamp(event), observed_at=stamp(datetime.now(UTC)),
                   phase=phase, context=context, references=[subject])
        if sha := endpoint.get("sha"):
            row.update(sha=sha, references=[subject, f"https://github.com/{REPO}/commit/{sha}"])
        missing = endpoint.get("error")
        if workflow == DEAD and "error" in before and not missing:
            missing = "baseline_function_population_unavailable"
        if missing:
            row.update(coverage="unavailable", reason=missing)
        elif workflow == DEAD:
            population = before["candidate_ids"]
            row.update(coverage="available", population=population, values=[len(set(population).intersection(endpoint["candidate_ids"]))])
        else:
            row.update(coverage="available", population=["BenchmarkCompileSimpleWorkflow"], values=endpoint["ns_per_op"])
        rows.append(row)
    return rows


def endpoints_for_run(repo, run, due, history, tip, measure):
    amounts = []
    if run["status"] != "completed" or run["head_sha"] not in history:
        why = "source_run_not_completed" if run["status"] != "completed" else "source_head_not_on_trusted_first_parent"
        return [{"error": why}, {"error": why}]
    end = command(["git", "rev-list", "--first-parent", "-1", "--before=" + stamp(due), tip], repo).strip()
    for sha in (run["head_sha"], end):
        if sha not in history or history[sha] > history[run["head_sha"]]:
            amounts.append({"error": "followup_not_on_or_after_baseline"})
            continue
        try:
            with snapshot(repo, sha) as work:
                amounts.append({"sha": sha, **measure(work)})
        except (Unavailable, OSError, UnicodeError, json.JSONDecodeError) as exc:
            amounts.append({"sha": sha, "error": "endpoint_measurement_failed", "diagnostic": str(exc)})
    return amounts


def collect(args):
    day, lag = args.day, DAYS[args.workflow]
    report_end = day + timedelta(days=lag + 1)
    if day < args.start or datetime.combine(report_end, time(12), UTC) > datetime.now(UTC):
        raise ValueError("select an enrolled UTC day with all follow-ups and the final noon cutoff in the past")
    origin = command(["git", "remote", "get-url", "origin"], ROOT).strip().removesuffix(".git")
    if origin not in ("https://github.com/" + REPO, "git@github.com:" + REPO) or command(["git", "rev-parse", "--is-shallow-repository"], ROOT).strip() != "false":
        raise ValueError("a full local checkout of github/gh-aw is required")
    page = json.loads(args.runs.read_bytes())
    runs = selected_runs(page, args.workflow, args.branch, day)
    command(["git", "check-ref-format", "refs/heads/" + args.branch], ROOT)
    tip = command(["git", "rev-parse", "--verify", "refs/remotes/origin/" + args.branch + "^{commit}"], ROOT).strip()
    collector_sha = command(["git", "rev-parse", "HEAD"], ROOT).strip()
    history = {sha: i for i, sha in enumerate(command(["git", "rev-list", "--first-parent", tip], ROOT).splitlines())}
    version = command(["go", "version"], ROOT).strip()
    if version != f"go version {TOOLCHAIN} linux/amd64":
        raise ValueError(f"expected {TOOLCHAIN} linux/amd64, found {version}")
    path = DEFS / ("prospective" if args.workflow == DEAD else "") / f"{args.workflow}.json"
    definition = json.loads(path.read_bytes())
    if args.workflow == DEAD:
        build_info = command(["go", "version", "-m", args.deadcode], ROOT) if args.deadcode else ""
        if (f"mod\tgolang.org/x/tools\t{ANALYZER}\t" not in build_info
                or "path\tgolang.org/x/tools/cmd/deadcode\n" not in build_info):
            raise ValueError(f"expected deadcode built from golang.org/x/tools {ANALYZER}")
        measure = lambda work: deadcode(work, args.deadcode.resolve())
        protocol = "deadcode:" + ANALYZER
    else:
        digest = hashlib.sha256((ROOT / FIXTURE).read_bytes()).hexdigest()
        measure = lambda work: benchmark(work, digest)
        protocol = "benchmark/1cpu/1s/5:" + digest
    context = f"v1:{version}:{protocol}:{os.environ.get('ImageVersion', 'local')}:{args.collector}"
    observations, witnesses = [], []
    for run in runs:
        at = datetime.fromisoformat(run["created_at"])
        due = at + timedelta(days=lag)
        ends = endpoints_for_run(ROOT, run, due, history, tip, measure)
        observations.extend(observe_pair(args.workflow, definition, run, at, due, ends, context))
        witnesses.append({"source": run, "endpoints": ends})
    recipe = dict(version=1, id=args.workflow + "-prospective-v1", repository="github.com/" + REPO, workflow=args.workflow,
                  start=str(day - timedelta(days=1)), end=str(report_end),
                  window=dict(version=1, baseline="paired_observation", before_days=1, after_days=lag,
                              minimum_before_samples=1, minimum_after_samples=1, durability_days=lag))
    out = args.output
    out.mkdir(parents=True, exist_ok=False)
    (out / "definition.json").write_bytes(path.read_bytes())
    docs = {"recipe": recipe, "observations": dict(version=1, repository="github.com/" + REPO, observations=observations),
            "provenance": dict(version=1, workflow=args.workflow, day=str(day), enrolled_from=str(args.start), branch=args.branch,
                               collector=args.collector, collector_sha=collector_sha, history_tip=tip, context=context,
                               listed=page["total_count"], runs=witnesses)}
    for key, data in docs.items():
        payload = json.dumps(data, indent=2, allow_nan=False).encode() + b"\n"
        if key == "observations" and len(payload) > 512 * 1024:
            raise ValueError("cohort exceeds the evidence reader's 512 KiB limit; do not truncate")
        (out / f"{key}.json").write_bytes(payload)
    missing = sum(row["coverage"] != "available" for row in observations)
    print(f"{day}: {len(runs)} eligible runs, {len(observations)} endpoints, {missing} unavailable")
    return 1 if missing else 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("workflow", choices=DAYS)
    parser.add_argument("runs", type=Path, help="unfiltered single-day GitHub workflow-runs API response")
    parser.add_argument("output", type=Path)
    parser.add_argument("--day", type=date.fromisoformat, required=True)
    parser.add_argument("--start", type=date.fromisoformat, required=True, help="first opted-in source cohort date")
    parser.add_argument("--branch", required=True, help="trusted repository default branch")
    parser.add_argument("--collector", required=True, help="collector execution identity (not the subject run)")
    parser.add_argument("--deadcode", type=Path)
    try:
        sys.exit(collect(parser.parse_args()))
    except (ValueError, KeyError, TypeError, OSError) as exc:
        sys.exit(f"Value evidence collection failed: {exc}")
