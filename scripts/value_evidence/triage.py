#!/usr/bin/env python3
"""Join a sealed pre-action cohort, a blind human reference, and an independent follow-up."""

import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
from pathlib import Path
import sys

from collect import REPO, stamp


LABELS = {"bug", "feature", "enhancement", "documentation", "question", "help-wanted", "good-first-issue"}


def clock(value):
    at = datetime.fromisoformat(value)
    if at.tzinfo is None or at > datetime.now(timezone.utc):
        raise ValueError("the observation needs a timezone and cannot be in the future")
    return at


def join(raw, review, followup):
    source = json.loads(raw)
    digest = hashlib.sha256(raw).hexdigest()
    run, attempt = source["run_id"], source["run_attempt"]
    if (source["repository"] != "github.com/" + REPO or source["workflow"] != "issue-triage-agent"
            or type(run) is not int or type(attempt) is not int or min(run, attempt) <= 0):
        raise ValueError("expected a github/gh-aw issue-triage-agent run and positive attempt")
    if review["cohort_sha256"] != digest or followup["cohort_sha256"] != digest:
        raise ValueError("review and follow-up must identify the exact sealed cohort bytes")
    at, end = clock(source["at"]), clock(followup["at"])
    if end != at + timedelta(days=14):
        raise ValueError("the follow-up event must describe issue state at baseline + 14 days")
    decision_at = clock(review["reviewed_at"])
    before_clock, after_clock = clock(source["observed_at"]), clock(followup["observed_at"])
    if not at <= before_clock <= decision_at or not before_clock < end <= after_clock:
        raise ValueError("conflicting snapshot, review, or collection clocks")
    issues = source["eligible_issues"]
    if not isinstance(issues, list):
        raise ValueError("eligible_issues must be the complete issue array; use [] only for a verified empty inventory")
    population = {}
    for issue in issues:
        number = issue["number"]
        if type(number) is not int or number < 1 or str(number) in population or issue["labels"] != [] or issue["assignees"] != []:
            raise ValueError("expected unique unlabeled, unassigned issue IDs at baseline")
        population[str(number)] = f"https://github.com/{REPO}/issues/{number}"
    reference, actual = review["classifications"], followup["labels"]
    if not isinstance(reference, dict) or not isinstance(actual, dict):
        raise ValueError("classifications and observed labels must be keyed by issue number")
    reason = None
    if not all(d["complete"] is True for d in (source, review, followup)):
        reason = "independent_collection_incomplete"
    elif review["blind_human_review"] is not True:
        reason = "independent_blind_human_reference_missing"
    elif set(reference) != set(population) or set(actual) != set(population):
        reason = "eligible_subjects_missing_or_changed"
    elif any(label not in LABELS for label in reference.values()):
        raise ValueError("each reviewed issue requires one allowed reference label")
    elif any(not isinstance(labels, list) or not all(isinstance(s, str) for s in labels) for labels in actual.values()):
        raise ValueError("follow-up labels must be an array of observed label names per issue")
    elif any(len(set(labels) & LABELS) > 1 for labels in actual.values()):
        reason = "ambiguous_followup_classification"
    refs = [d["reference"] for d in (source, review, followup)]
    if any(not isinstance(ref, str) or not ref.strip() for ref in refs):
        raise ValueError("retain a source reference for each of the three inputs")
    refs.extend("sha256:" + hashlib.sha256(json.dumps(d, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
                for d in (review, followup))
    subject = f"https://github.com/{REPO}/actions/runs/{run}/attempts/{attempt}"
    rows = []
    for phase, event in (("baseline", at), ("outcome", end)):
        row = dict(id=f"triage-{run}-{attempt}-{phase}", workflow="issue-triage-agent", metric="correctly-triaged-eligible-issues-v1",
                   scope="object_state", run_id=run, subject=subject, at=stamp(event), observed_at=stamp(datetime.now(timezone.utc)),
                   phase=phase, context="blind-human-classification-v1:" + digest, references=[subject, *refs])
        if reason:
            row.update(coverage="unavailable", reason=reason)
        else:
            count = 0 if phase == "baseline" else sum(reference[k] in actual[k] for k in population)
            row.update(coverage="available", population=sorted(population.values()), values=[count])
        rows.append(row)
    return dict(version=1, repository=source["repository"], observations=rows)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("cohort", "review", "followup"):
        parser.add_argument(name, type=Path)
    args = parser.parse_args()
    try:
        result = join(args.cohort.read_bytes(), json.loads(args.review.read_bytes()), json.loads(args.followup.read_bytes()))
        print(json.dumps(result, indent=2, allow_nan=False))
    except (ValueError, KeyError, TypeError, OSError) as exc:
        sys.exit(f"Triage evidence projection failed: {exc}")
