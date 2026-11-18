#!/usr/bin/env python3
"""Replay the frozen ten-workflow pilot. Never collect, dispatch, or update goldens."""

import argparse
import json
import subprocess
import sys
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFS = ROOT / ".github/aw/value"
PILOT = DEFS / "pilots"
DATE = "2026-11-18"


def equal(label, actual, expected):
    if actual != expected:
        raise ValueError(f"{label}: expected {expected!r}, got {actual!r}")


def report(cli, name, logs, bundle):
    args = [cli, "value", name, "--repo", "github/gh-aw", "--logs-dir", logs,
            "--recipe", DEFS / f"{name}.pilot.json", "--evidence", bundle, "--json"]
    result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=60)
    if result.returncode:
        raise ValueError(f"value exited {result.returncode}: {result.stderr.strip()}")
    document = json.loads(result.stdout)
    del document["observed_at"]  # Only the report invocation clock may change.
    return document


def check_native(doc, case, archived):
    evidence = case["observed"]
    coverage = evidence["value_lifecycle_coverage"] if archived else "unavailable"
    lifecycle = doc["lifecycle"]
    equal("lifecycle coverage", lifecycle["coverage"], coverage)
    if coverage == "retained_subset":
        equal("delivered effects", lifecycle["delivered"], evidence["native_delivered"])
        equal("effect runs", {r["run_id"] for r in lifecycle["records"]}, {case["run_id"]})
        for field, source in (("accepted_human", "native_human_accepted"),
                              ("accepted_delivery_fallback", "native_delivery_fallback"), ("pending", "native_pending")):
            equal(field, lifecycle["adoption"][field], evidence.get(source, 0))
    else:
        equal("missing ledger counts", lifecycle["delivered"], None)
    retained = archived and evidence["collection"] == "complete"
    grades = doc["run_grades"]
    equal("run coverage", grades["coverage"], "retained_subset" if retained else "unavailable")
    actual = [(r["run_id"], r["run_attempt"], r["grader"]["id"], r["grader"]["status"],
               r["grader"].get("value")) for r in grades["runs"]]
    equal("recorded grades", actual, [(case["run_id"], 1, "operational-value", "unavailable", None)] if retained else [])


def check_workflow(cli, case, expected, bundle, bundle_digest, logs, archived):
    name = case["workflow"]
    doc = report(cli, name, logs, bundle)
    equal("identical replay", doc == report(cli, name, logs, bundle), True)
    equal("workflow", doc["measurement"]["workflow"], name)
    equal("configuration", doc["measurement_status"], "configured")
    equal("definition digest", doc["measurement"]["digest"], expected["definition_digest"])
    equal("recipe digest", doc["recipe"]["digest"], expected["recipe_digest"])
    equal("window policy", doc["window_policy"], expected["window_policy"])
    evidence = doc["evidence"]
    equal("evidence digest", evidence["digest"], bundle_digest)
    equal("evidence trust", evidence["trust"], "caller_supplied_not_native_adoption")
    equal("evidence coverage", evidence["coverage"], expected["evidence_coverage"])
    observations = []
    for row in evidence["observations"]:
        equal("producer identity", (row["input"]["workflow"], row["input"]["run_id"]), (name, case["run_id"]))
        observations.append({"id": row["input"]["id"], "value": row["value"], "reason": row.get("reason")})
    equal("independent measurements", observations, expected["observations"])
    changes = [dict(c) for c in expected["changes"]]
    if not archived and changes:
        # File Diet's fixed outcome precedes the retained human decision by three seconds.
        equal("only paired witness", (name, len(changes)), ("daily-file-diet", 1))
        changes[0]["matures_at"] = "2026-12-01T06:09:01Z"
    equal("paired changes", evidence["changes"], changes)
    equal("attributed subtotal", doc["attributed_value"], None)
    equal("attribution coverage", doc["attribution_coverage"], "unavailable")
    check_native(doc, case, archived)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cli", type=Path, default=ROOT / "gh-aw", help="built gh-aw executable")
    parser.add_argument("--logs-dir", type=Path, help="original pilot lifecycle.jsonl and nine-run summary.json")
    args = parser.parse_args()
    cohort = json.loads((PILOT / f"{DATE}.json").read_bytes())["workflows"]
    golden = json.loads((PILOT / f"{DATE}.reevaluation.json").read_bytes())
    indexed = {row["workflow"]: row for row in golden["workflows"]}
    equal("distinct workflows", len({c["workflow"] for c in cohort}), 10)
    equal("cohort", sorted(c["workflow"] for c in cohort), sorted(indexed))
    failures = 0
    with tempfile.TemporaryDirectory() as empty:
        for case in cohort:
            name = case["workflow"]
            try:
                check_workflow(args.cli.resolve(), case, indexed[name], PILOT / f"{DATE}.evidence.json",
                               golden["evidence_digest"], args.logs_dir.resolve() if args.logs_dir else empty,
                               args.logs_dir is not None)
                print(f"PASS {name} ({case['run_id']}): outcome {indexed[name]['evidence_coverage']}")
            except (ValueError, KeyError, TypeError, OSError, subprocess.TimeoutExpired) as exc:
                print(f"FAIL {name} ({case['run_id']}): {exc}", file=sys.stderr)
                failures += 1
    print(f"{len(cohort) - failures}/{len(cohort)} workflow pairs matched")
    return int(failures != 0)


if __name__ == "__main__":
    sys.exit(main())
