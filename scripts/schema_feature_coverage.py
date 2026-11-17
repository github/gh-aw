#!/usr/bin/env python3
"""Evaluate and replay the versioned schema-feature-coverage textual measure."""

import argparse
import hashlib
import io
import json
import re
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path


PACKAGE = Path(__file__).with_name("schema-feature-coverage")
CONTRACT = PACKAGE / "contract.v1.json"
HISTORY = PACKAGE / "replay.v1.json"
SCHEMA = "pkg/parser/schemas/main_workflow_schema.json"
DIRECTORIES = (".github/workflows", "schema-demos")


def digest(content):
    return hashlib.sha256(content).hexdigest()


def evidence_digest(value):
    return digest(json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode())


def instant(value):
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if result.tzinfo is None:
        raise ValueError("Timestamps must include a timezone")
    return result.astimezone(timezone.utc)


def coverage(fields, documents):
    if not isinstance(fields, dict) or any(not isinstance(key, str) or not key for key in fields):
        raise ValueError("Schema properties must be an object with nonempty field names")
    if any(not isinstance(text, str) for text in documents):
        raise ValueError("Markdown evidence must be text")
    if not fields or not documents:
        return {
            "coverage_status": "unavailable",
            "covered": None, "total": None, "missing": None, "schema_fields": None,
            "missing_reason": "Empty schema population or absent Markdown inventory",
        }
    missing = [
        field for field in sorted(fields)
        if not any(re.search(rf"^{re.escape(field)}:", text, re.MULTILINE) for text in documents)
    ]
    return {
        "coverage_status": "available",
        "covered": len(fields) - len(missing), "total": len(fields),
        "missing": missing, "schema_fields": sorted(fields),
    }


def merge_delta(before, after):
    if before["coverage_status"] != "available" or after["coverage_status"] != "available":
        raise ValueError("Merge deltas require complete before and after inventories")
    old, new = set(before["schema_fields"]), set(after["schema_fields"])
    old_missing, new_missing = set(before["missing"]), set(after["missing"])
    retained = old & new
    return {
        "newly_covered": sorted(retained & old_missing - new_missing),
        "coverage_lost": sorted(retained & new_missing - old_missing),
        "schema_fields_added": sorted(new - old),
        "schema_fields_removed": sorted(old - new),
    }


def window_schedule(first_merge, last_merge):
    first, last = instant(first_merge), instant(last_merge)
    if last < first:
        raise ValueError("A merge batch cannot complete before it starts")
    return [
        {"period": "before" if day < 0 else "after", "day": day,
         "at": ((first if day < 0 else last) + timedelta(days=day)).isoformat()}
        for day in range(-7, 7)
    ]


class Repository:
    def __init__(self, path, tip):
        self.path = path
        if not re.fullmatch(r"[a-f0-9]{40}", tip):
            raise ValueError("An immutable full Git commit ID is required")
        self.tip = tip
        if self.git("rev-parse", "--is-shallow-repository").strip() != b"false":
            raise ValueError("Replay requires complete, non-shallow first-parent history")
        self.first_parents = set(self.git("rev-list", "--first-parent", tip).decode().splitlines())
        self.cache = {}

    def git(self, *args, **kwargs):
        return subprocess.check_output(["git", "-C", str(self.path), *args], **kwargs)

    def at(self, cutoff):
        sha = self.git("rev-list", "--first-parent", "-1", f"--before={cutoff}", self.tip).decode().strip()
        if not sha:
            raise ValueError(f"No first-parent source revision at {cutoff}")
        return sha

    def measure(self, sha):
        if sha not in self.first_parents:
            raise ValueError(f"Source revision is not on pinned first-parent history: {sha}")
        if sha in self.cache:
            return self.cache[sha]
        inventory = self.git("ls-tree", "-r", "-z", sha, "--", *DIRECTORIES, SCHEMA).split(b"\0")
        blobs = []
        for entry in filter(None, inventory):
            metadata, name = entry.split(b"\t", 1)
            mode, kind, oid = metadata.split()
            path = name.decode()
            if kind == b"blob" and mode in (b"100644", b"100755") and (
                path == SCHEMA or path.endswith(".md")
            ):
                blobs.append((path, oid))
        fields, documents, hashes = {}, [], {}
        if blobs:
            stream = io.BytesIO(self.git("cat-file", "--batch", input=b"".join(oid + b"\n" for _, oid in blobs)))
            for path, oid in blobs:
                actual_oid, kind, size = stream.readline().split()
                content = stream.read(int(size))
                if actual_oid != oid or kind != b"blob" or len(content) != int(size) or stream.read(1) != b"\n":
                    raise ValueError(f"Incomplete Git blob evidence at {sha}:{path}")
                hashes[path] = digest(content)
                if path == SCHEMA:
                    schema = json.loads(content)
                    if not isinstance(schema, dict) or "properties" not in schema:
                        raise ValueError(f"Malformed schema at {sha}")
                    fields = schema["properties"]
                else:
                    documents.append(content.decode("utf-8"))
        result = {
            "commit": sha, **coverage(fields, documents),
            "workflow_documents": len(documents), "source_hashes": dict(sorted(hashes.items())),
        }
        self.cache[sha] = result
        return result

    def witness(self, record):
        sha = record["commit"]
        if sha not in self.first_parents:
            raise ValueError(f"Accepted merge is not on pinned first-parent history: {sha}")
        parent = self.git("rev-parse", sha + "^1").decode().strip()
        if parent != record["parent"]:
            raise ValueError(f"First-parent witness conflicts for PR {record['number']}")
        before, after = self.measure(parent), self.measure(sha)
        observed = [before["covered"], before["total"], after["covered"], after["total"]]
        if observed != record["expected"]:
            raise ValueError(f"Accepted-merge evidence changed for PR {record['number']}: {observed}")
        return {
            **record, "before": before, "after": after, **merge_delta(before, after),
            "url": f"https://github.com/github/gh-aw/pull/{record['number']}",
            "adoption_status": "GitHub merge proxy; native lifecycle qualification not inferred",
            "attributed_value": None,
        }


def weekly_cutoffs(start, end):
    first, last = instant(start), instant(end)
    if last < first:
        raise ValueError("Replay end precedes start")
    result = []
    while first <= last:
        result.append(first.isoformat())
        first += timedelta(days=7)
    if result[-1] != last.isoformat():
        result.append(last.isoformat())
    return result


def replay(repository, history):
    observed_at = instant(history["observed_at"])
    cutoffs = weekly_cutoffs(history["start"], history["end"])
    if instant(history["end"]) > observed_at:
        raise ValueError("Historical cutoff exceeds the preserved as-of")
    observations = [{"cutoff": at, **repository.measure(repository.at(at))} for at in cutoffs]
    if [[row["covered"], row["total"]] for row in observations] != history["weekly_expected"]:
        raise ValueError("Weekly replay disagrees with preserved historical evidence")
    witnesses = [repository.witness(record) for record in history["merges"]]
    if len({row["number"] for row in witnesses}) != len(witnesses):
        raise ValueError("Duplicate PR witness")
    indexed = {row["number"]: row for row in witnesses}
    windows = []
    for case in history["cases"]:
        members = sorted([indexed[number] for number in case["members"]], key=lambda row: instant(row["merged_at"]))
        first, last = members[0], members[-1]
        samples = []
        for point in window_schedule(first["merged_at"], last["merged_at"]):
            if instant(point["at"]) > observed_at:
                raise ValueError("Daily replay sample exceeds preserved as-of")
            sha = last["commit"] if point["day"] == 0 else repository.at(point["at"])
            samples.append({**point, **repository.measure(sha)})
        endpoint = instant(last["merged_at"]) + timedelta(days=7)
        if endpoint > observed_at:
            raise ValueError("Post-window endpoint exceeds preserved as-of")
        windows.append({
            "id": case["id"], "members": case["members"],
            "before_start": samples[0]["at"], "before_end_exclusive": first["merged_at"],
            "after_start": last["merged_at"], "after_end_exclusive": endpoint.isoformat(),
            "samples": samples, "immediately_before": first["before"],
            "post_window_end": repository.measure(repository.at(endpoint.isoformat())),
            "example_gaps_closed": len(set().union(*(set(row["newly_covered"]) for row in members))),
            "schema_fields_removed": sorted(set().union(*(set(row["schema_fields_removed"]) for row in members))),
            "window_status": "elapsed; full-window survival unknown",
            "attributed_value": None,
        })
    compact = [
        [window["id"], [[s["at"], s["commit"], s["covered"], s["total"], s["missing"], s["schema_fields"]]
                        for s in window["samples"]]]
        for window in windows
    ]
    if evidence_digest(compact) != history["daily_expected_sha256"]:
        raise ValueError("Daily replay disagrees with preserved historical evidence")
    available = [set(row["schema_fields"]) for row in observations if row["coverage_status"] == "available"]
    if len(available) != len(observations):
        raise ValueError("Retained-cohort comparison requires complete weekly inventories")
    retained = set.intersection(*available)
    cohort = [
        {"cutoff": row["cutoff"], "covered": len(retained - set(row["missing"])), "total": len(retained)}
        for row in observations
    ]
    return {
        "kind": "retrospective-schema-example-replay", "version": "1.0.0",
        "repository": "github/gh-aw", "workflow": "schema-feature-coverage",
        "main_tip": repository.tip, "observed_at": history["observed_at"],
        "observations": observations, "retained_cohort": {"fields": sorted(retained), "observations": cohort},
        "merge_witnesses": witnesses, "windows": windows, "attributed_value": None,
        "registration_status": "unregistered offline package",
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path, default=Path("."))
    parser.add_argument("--commit", help="Measure one immutable first-parent revision instead of replaying")
    args = parser.parse_args()
    history = json.loads(HISTORY.read_bytes())
    provenance = {
        "contract_sha256": digest(CONTRACT.read_bytes()),
        "evaluator_sha256": digest(Path(__file__).read_bytes()),
        "replay_input_sha256": digest(HISTORY.read_bytes()),
    }
    for key in ("contract_sha256", "evaluator_sha256"):
        if provenance[key] != history[key]:
            raise ValueError(f"Frozen {key} mismatch; use the reviewed version, not altered bytes")
    repository = Repository(args.repository, history["main_tip"])
    result = repository.measure(args.commit) if args.commit else replay(repository, history)
    print(json.dumps({**result, "provenance": provenance}, indent=2, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        print(f"schema-feature-coverage: {error}", file=sys.stderr)
        sys.exit(1)
