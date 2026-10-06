#!/usr/bin/env python3
"""Independent local native-engine gate; no remote API calls or publication.

Shared fixtures must contain independent expected values, not outputs copied
from either engine. The Python code only canonicalizes integer JSON and builds
workloads; scheduling/replay/packing always execute actual Go and JS engines.
All executables, generated ledgers and evidence go to --evidence-dir, outside
the checkout. A failed gate exits nonzero and retains its diagnostic evidence.
"""

import argparse
import copy
import hashlib
import json
import os
import platform
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SPEC = ROOT / "specs/work-queue"
MIB = 1024 * 1024


def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":"), allow_nan=False)


def digest(value):
    return hashlib.sha256(canonical(value).encode()).hexdigest()


def engine_source_hashes():
    files = [path for path in (ROOT / "pkg/workqueue").rglob("*")
             if path.is_file() and (path.suffix == ".json" or path.suffix == ".go" and not path.name.endswith("_test.go"))]
    files += [ROOT / "actions/setup/js" / f"work_queue_{module}.cjs"
              for module in ("codec", "policy", "scheduler", "graph", "limits", "replay")]
    files += [ROOT / "go.mod", ROOT / "go.sum", SPEC / "native_probe.cjs",
              SPEC / "verify_native.py", SPEC / "verify_native_fixtures.py"]
    files += list((SPEC / "native_probe").glob("*.go"))
    files += list((SPEC / "fixtures").glob("*.json"))
    files += [ROOT / "actions/setup/js/work_queue_conformance_fixtures.json"]
    return {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(files)}


class Native:
    def __init__(self, command, env):
        self.command, self.env = command, env
        self.process = None

    def __enter__(self):
        self.process = subprocess.Popen(self.command, cwd=ROOT, env=self.env, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding="utf-8")
        return self

    def call(self, request):
        self.process.stdin.write(canonical(request) + "\n")
        self.process.stdin.flush()
        output = self.process.stdout.readline()
        if not output:
            raise RuntimeError(f"{self.command} exited {self.process.poll()}: {self.process.stderr.read()}")
        return json.loads(output)

    def __exit__(self, *args):
        self.process.stdin.close()
        self.process.wait(timeout=30)
        error = self.process.stderr.read()
        self.process.stdout.close()
        self.process.stderr.close()
        if self.process.returncode:
            raise RuntimeError(f"{self.command}: exit {self.process.returncode}: {error}")


def ledger_text(value):
    return value if isinstance(value, str) else "".join(canonical(commit) + "\n" for commit in value)


def ordered_ledger(text):
    """Independent causal ordering oracle, not a runtime selector."""
    commits = {commit["id"]: commit for commit in (json.loads(line) for line in text.splitlines())}
    children = {commit["previous"]: commit["id"] for commit in commits.values()}
    result, previous = [], None
    while previous in children:
        previous = children[previous]
        result.append(commits[previous])
        if len(result) > len(commits):
            raise AssertionError("fixture causal cycle")
    if len(result) != len(commits):
        raise AssertionError("fixture does not contain one complete chain")
    return "".join(canonical(commit) + "\n" for commit in result)


def without_metrics(output):
    # The shared contract defines no projection-trimming equivalence. Only
    # measurements may differ; every actual serialized state field is checked.
    return {key: value for key, value in output.items() if key != "metrics"}


def assert_native_parity(go, js):
    differences = []

    def compare(left, right, path):
        if len(differences) >= 8:
            return
        if type(left) is not type(right):
            differences.append(f"{path}: different value types")
        elif isinstance(left, dict):
            for key in sorted(left.keys() | right.keys()):
                if key not in left or key not in right:
                    differences.append(f"{path}/{key}: missing from {'Go' if key not in left else 'JS'}")
                else:
                    compare(left[key], right[key], f"{path}/{key}")
        elif isinstance(left, list):
            if len(left) != len(right):
                differences.append(f"{path}: lengths {len(left)} vs {len(right)}")
            else:
                for index, (a, b) in enumerate(zip(left, right)):
                    compare(a, b, f"{path}/{index}")
        elif left != right:
            differences.append(f"{path}: {left!r} vs {right!r}")

    compare(without_metrics(go), without_metrics(js), "")
    if differences:
        raise AssertionError("; ".join(difference[:512] for difference in differences[:8]))


def assert_subset(actual, expected, path=""):
    if isinstance(expected, dict):
        assert isinstance(actual, dict), f"{path}: expected object, got {actual!r}"
        for key, value in expected.items():
            assert key in actual, f"{path}/{key}: missing"
            assert_subset(actual[key], value, f"{path}/{key}")
    else:
        assert actual == expected, f"{path}: actual={actual!r}, expected={expected!r}"


def request(ledger, **overrides):
    value = {
        "action": "replay", "data": ledger, "pool": "default", "at": 9007199254740991,
        "parameters": {"pool": "default", "max_claims": 16, "max_dispatches": 16, "max_bytes": 48 * 1024},
        "request_id": "verification-request", "commit_id": "verification-commit", "include_canonical": True,
    }
    value.update(overrides)
    return value


def default_policy():
    # Independent explicit benchmark policy, not a runtime default-policy call.
    return {
        "mode": "weighted-priority", "class_weights": [8, 4, 2, 1, 1], "accounting_weights": {"": 1},
        "producers": {"42": {"pools": ["default"], "priorities": [1, 2, 3, 4, 5], "fairness_keys": [""]}},
        "pools": {"default": {
            "default_profile": "default", "logical_limit": 4096, "native_limit": 256,
            "allowed_repositories": ["owner/repo"], "max_observation_age_ms": 60000,
            "retry": {"max_attempts": 3, "backoff_ms": 30000},
            "reconciliation": {"max_attempts": 5, "deadline_ms": 300000},
            "profiles": {"default": {"workflow": ".github/workflows/worker.lock.yml", "ref": "a" * 40,
                                    "principal": "42", "trust_domain": "default", "credential_scope": "repository",
                                    "effect_scope": "owner/repo", "max_claims": 16, "share_keys": False}}},
        },
        "limits": {"ledger_bytes": 64 * MIB, "recovery_bytes": 16 * MIB, "payload_bytes": 16 * 1024,
                   "graph_nodes": 4096, "predecessors": 64, "pending_nodes": 4096, "operations": 256,
                   "assignment_bytes": 48 * 1024, "result_bytes": 4096, "evidence_bytes": 1024, "observation_writes": 4096},
    }


class Ledger:
    def __init__(self, policy=None):
        self.commits = []
        self.actor = {"role": "administrator", "principal": "42", "repository": "owner/repo"}
        self.policy = policy or default_policy()
        self.append("policy", [{"kind": "Policy", "epoch": "epoch", "policy": self.policy}])

    def append(self, kind, operations, parameters=None, actor=None, request_id=None):
        actor = actor or self.actor
        index = len(self.commits)
        params = parameters if parameters is not None else {"operations": operations}
        logical = {"actor": actor, "kind": kind, "parameters": params}
        commit = {"version": 3, "id": f"c{index}", "previous": None if index == 0 else f"c{index-1}",
                  "request": {"id": request_id or f"r{index}", "kind": kind, "parameters": params, "fingerprint": digest(logical)},
                  "actor": actor, "policy_epoch": "epoch", "at": index + 1, "operations": operations}
        self.commits.append(commit)
        return commit

    def work(self, key, edges=None, payload_size=1024):
        graph = "benchmark"
        return {"kind": "Work", "work_id": digest({"graph_id": graph, "node_key": key}),
                "graph_id": graph, "node_key": key, "pool": "default", "priority": 3, "fairness_key": "",
                "worker_profile": "default", "batch_trust_domain": "default",
                "payload": {"plan": "x" * payload_size}, "depends_on": edges or [], "enqueued": 2}

    def submit(self, nodes):
        self.append("submit", nodes, {"nodes": nodes})

    def text(self):
        return ledger_text(self.commits)


def graph_workload():
    ledger = Ledger()
    nodes = []
    # All three graph vertex kinds: 128 Work + 3968 unique typed gates = 4096.
    # A subject alone is not an edge. No external reads are executed.
    resource_number = 0
    for index in range(128):
        edges = []
        # Sixteen independent eligible roots; redistribute their gates without
        # exceeding 64 predecessors, so selection is real and the graph is full.
        edge_count = 0 if index < 16 else 62 if index < 32 else 31
        for edge in range(edge_count):
            resource_number += 1
            identity = str(resource_number)
            kind = "issue" if edge % 2 == 0 else "pull_request"
            edges.append({"kind": kind, "condition": "completed" if kind == "issue" else "merged",
                          "resource": {"kind": kind, "host": "github.com", "repository": "owner/repo",
                                       "repository_id": "1", "resource_id": identity, "number": identity}})
        nodes.append(ledger.work(f"n{index}", edges))
    ledger.submit(nodes)
    return ledger


def ready_workload(mixed=False):
    ledger = Ledger()
    if mixed:
        # Construct a new genesis because Policy is immutable after installation.
        policy = ledger.policy
        alternate = dict(policy["pools"]["default"]["profiles"]["default"])
        alternate.update(workflow=".github/workflows/alternate.lock.yml", max_claims=1)
        policy["pools"]["default"]["profiles"]["alternate"] = alternate
        ledger = Ledger(policy)
    nodes = [ledger.work(f"ready-{index}") for index in range(128)]
    if mixed:
        for index, node in enumerate(nodes):
            if index % 3 == 1:
                node["worker_profile"] = "alternate"
    ledger.submit(nodes)
    return ledger


def drained_workload(ledger):
    drained = copy.deepcopy(ledger)
    works = [node for commit in ledger.commits for node in commit["operations"] if node["kind"] == "Work"]
    drained.append("cancel_work", [{"kind": "WorkCancellation", "work_id": node["work_id"],
                                  "reason": "benchmark_drain"} for node in works])
    return drained


def verify_benchmark_serialization(output, text):
    expected = text.encode()
    metrics = output["metrics"]
    assert metrics["input_bytes"] == metrics["canonical_bytes"] == len(expected), "canonical ledger byte length"
    assert metrics["canonical_sha256"] == hashlib.sha256(expected).hexdigest(), "canonical ledger content hash"


def mock_contention(commands, env, evidence):
    """Actual planning/replay with an explicitly MOCK in-memory CAS."""
    rows = []
    dispatcher = {"role": "dispatcher", "principal": "42", "repository": "owner/repo",
                  "workflow": ".github/workflows/dispatcher.lock.yml", "run_id": "10", "run_attempt": 1}
    for name, command in commands.items():
        for mixed in (False, True):
            for batch in (1, 16):
                ledger = ready_workload(mixed)
                params = {"pool": "default", "max_claims": batch, "max_dispatches": 1, "max_bytes": 48 * 1024}
                prefix = ledger.text()
                start = time.monotonic()
                with Native(command, env) as engine:
                    contenders = [
                        engine.call(request(prefix, parameters=params, request_id=f"mock-{index}",
                                            commit_id="c2", at=3, include_canonical=False))
                        for index in range(8)
                    ]
                    assert all("error" not in candidate for candidate in contenders), contenders
                    stale = 0
                    claims = 0
                    for index, tentative in enumerate(contenders):
                        current = tentative
                        if index:
                            stale += 1
                            current = engine.call(request(ledger.text(), parameters=params, request_id=f"mock-{index}",
                                                          commit_id=f"c{len(ledger.commits)}", at=len(ledger.commits)+1,
                                                          include_canonical=False))
                        assert "error" not in current, current
                        selected = current["packing"]["operations"]
                        claims += len(selected)
                        ledger.append("dispatch_next", selected, params, dispatcher, request_id=f"mock-{index}")
                    final = engine.call(request(ledger.text(), parameters=params, include_canonical=False))
                    assert "error" not in final, final
                    assert final["projection"]["stats"]["claims"] == claims, "mock retry duplicated or lost charge"
                rows.append({"engine": name, "gate": "MOCK-publisher-contention", "dispatchers": 8,
                             "mixed_profiles": mixed, "claim_budget": batch, "stale_candidates_regenerated": stale,
                             "committed_claims": claims, "elapsed_ms": (time.monotonic()-start)*1000,
                             "boundary": "serial in-memory CAS simulation; not Git/network transport performance"})
    return rows


def local_finalization(commands, env):
    """Native per-Claim commits with explicitly MOCK trusted launch evidence."""
    rows = []
    dispatcher = {"role": "dispatcher", "principal": "42", "repository": "owner/repo",
                  "workflow": ".github/workflows/dispatcher.lock.yml", "run_id": "10", "run_attempt": 1}
    for name, command in commands.items():
        ledger = ready_workload()
        params = {"pool": "default", "max_claims": 16, "max_dispatches": 1, "max_bytes": 48 * 1024}
        with Native(command, env) as engine:
            candidate = engine.call(request(ledger.text(), parameters=params, request_id="r2", commit_id="c2", at=3, include_canonical=False))
            assert "error" not in candidate, candidate
            assignment = candidate["packing"]["assignments"][0]
            ledger.append("dispatch_next", candidate["packing"]["operations"], params, dispatcher)
            ledger.append("dispatch", [{"kind": "Dispatch", "dispatch_id": assignment["dispatch_id"],
                                       "state": "started", "sender": dispatcher}], actor=dispatcher)
            # The authenticated read boundary is mocked, not claimed verified.
            binding = {"run_id": "20", "run_attempt": 1, "repository": "owner/repo",
                       "workflow": ".github/workflows/worker.lock.yml", "ref": "a" * 40,
                       "principal": "42", "event": "workflow_dispatch"}
            evidence = {"kind": "reconciliation", "source": "trusted_activation", "repository": "owner/repo",
                        "workflow": binding["workflow"], "ref": binding["ref"], "principal": "42",
                        "checked_at": 5, "run_id": "20", "run_attempt": 1, "attempts": 1}
            worker = {"role": "worker", "principal": "42", "repository": "owner/repo",
                      "workflow": binding["workflow"], "run_id": "20", "run_attempt": 1, "dispatch_id": assignment["dispatch_id"]}
            ledger.append("dispatch", [{"kind": "Dispatch", "dispatch_id": assignment["dispatch_id"],
                                       "state": "bound", "run": binding, "evidence": evidence}], actor=worker)
            before_bytes = len(ledger.text().encode())
            start = time.monotonic()
            finish_bytes, replay_ms = [], []
            for index, member in enumerate(assignment["claims"]):
                outcome = "completed" if index % 2 == 0 else "cancelled"
                at = len(ledger.commits) + 1
                operation = ({"kind": "Completion", "work_id": member["work_id"], "claim_id": member["claim_id"],
                              "dispatch_id": assignment["dispatch_id"], "claim_handle": member["handle"],
                              "run_id": "20", "run_attempt": 1} if outcome == "completed" else
                             {"kind": "ClaimCancellation", "work_id": member["work_id"], "claim_id": member["claim_id"],
                              "reason": "worker_cancelled", "retry_not_before": at + 30000})
                commit = ledger.append("finish", [operation],
                                       {"dispatch_id": assignment["dispatch_id"], "claim_handle": member["handle"], "outcome": outcome},
                                       {**worker, "claim_handle": member["handle"]})
                finish_bytes.append(len(canonical(commit).encode()) + 1)
                observed = engine.call(request(ledger.text(), include_canonical=False))
                assert "error" not in observed, observed
                # Existing Completions remain independent of cancelled siblings.
                assert observed["projection"]["claims"][member["claim_id"]]["state"] == outcome
                replay_ms.append(observed["metrics"]["cold_replay_ms"])
            elapsed = (time.monotonic() - start) * 1000
            assert sum(finish_bytes) == len(ledger.text().encode()) - before_bytes
            rows.append({"engine": name, "gate": "local-native-perClaim-finalization", "claims": len(finish_bytes),
                         "completion_commits": 8, "cancellation_commits": 8, "canonical_append_bytes": sum(finish_bytes),
                         "per_claim_bytes": finish_bytes, "per_prefix_cold_replay_ms": replay_ms, "elapsed_ms": elapsed,
                         "boundary": "actual native replay and serialization; MOCK activation evidence; no Git/host write latency"})
    return rows


def future_reference_workload(parent_count):
    ledger = Ledger()
    parents = [ledger.work(f"future-result-{index}", payload_size=8) for index in range(parent_count)]
    child = ledger.work("future-result-child", payload_size=8)
    child["depends_on"] = [{"kind": "work", "work_id": parent["work_id"]} for parent in parents]
    ledger.submit([child, *parents])
    return ledger


def closure_byte_cases():
    # Structural envelopes only: evidence strings are not delivery authority.
    limits = default_policy()["limits"]
    identity = "\\" * 256
    evidence = {
        "kind": "delivery", "source": "verified_receipts", "repository": "owner/repo",
        "workflow": ".github/workflows/" + "w" * 229 + ".lock.yml", "ref": "a" * 40,
        "run_id": "20", "run_attempt": 1, "principal": "9" * 256, "checked_at": 9007199254740991,
        "receipt": "",
    }
    available = limits["evidence_bytes"] - len(canonical(evidence).encode())
    evidence["receipt"] = "\\" * (available // 2) + "a" * (available % 2)
    assert len(evidence["receipt"].encode()) <= 256
    assert len(canonical(evidence).encode()) == limits["evidence_bytes"]
    descriptor = {"data": "x" * (limits["result_bytes"] - len(canonical({"data": ""}).encode()))}
    assert len(canonical(descriptor).encode()) == limits["result_bytes"]
    operation = {
        "kind": "Result", "work_id": "b" * 64, "claim_id": "c_" + "c" * 64 + "_16",
        "completion_id": identity, "descriptor": descriptor, "evidence": evidence,
    }
    actor = {"role": "reconciler", "principal": evidence["principal"], "repository": "owner/repo"}
    parameters = {"operations": [operation]}
    record = {
        "version": 3, "id": identity, "previous": identity,
        "request": {"id": identity, "kind": "result", "parameters": parameters,
                    "fingerprint": digest({"actor": actor, "kind": "result", "parameters": parameters})},
        "actor": actor, "policy_epoch": identity, "at": 9007199254740991, "operations": [operation],
    }
    # Distinct opaque identities of the same worst escaped byte width.
    record["id"] = '"' * 256
    record["previous"] = '\\"' * 128
    record["request"]["id"] = '"\\' * 128
    record["policy_epoch"] = "\\\\\"" * 85 + "\\"
    return [{
        "name": "released-completed-pending-max-Result", "record": record,
        "reserved_bytes": 2 * limits["result_bytes"] + 2 * limits["evidence_bytes"] + 8192,
        "candidate_formula": "2*result_bytes+2*evidence_bytes+8192",
        "scope": "future closed-wire envelope, maximum opaque identities/descriptor/evidence; not causal replay or receipt authentication",
    }]


def closure_byte_audit(commands, env):
    rows, failures = [], []
    for case in closure_byte_cases():
        text = canonical(case["record"])
        append_bytes = len(text.encode()) + 1
        for name, command in commands.items():
            with Native(command, env) as engine:
                output = engine.call({"action": "validate_commit", "data": text})
            valid = output == {"canonical": text}
            covered = append_bytes <= case["reserved_bytes"]
            rows.append({
                "engine": name, "gate": "canonical-closure-byte-audit", "case": case["name"],
                "append_bytes": append_bytes, "reserved_bytes": case["reserved_bytes"],
                "candidate_formula": case["candidate_formula"], "scope": case["scope"],
                "closed_wire_valid": valid, "covered": covered, "output": output,
            })
            if not valid:
                failures.append(f"{name}/{case['name']}: bounded envelope refused or canonical bytes changed")
            elif not covered:
                failures.append(f"{name}/{case['name']}: {append_bytes} canonical append bytes exceed {case['reserved_bytes']} forecast")
    return rows, failures


def benchmark(commands, env, evidence, sizes, iterations):
    rows, failures = [], []
    # Bounded retained Control records supply bytes without inventing huge
    # payloads or admitting thousands of Work without their recovery reserve.
    controls = [{"kind": "Control", "control": "grants_paused", "value": False,
                 "reason": "retained-control:" + "x" * 110} for _ in range(256)]
    for target in sizes:
        ledger = ready_workload() if target == 1 else graph_workload()
        if target == 80:
            # Full physical reserve is measured on drained retained history.
            # Filling an active queue's promised closure reserve with optional
            # records would be a resource-budget violation, not a useful test.
            ledger = drained_workload(ledger)
        base = ledger.text()
        while True:
            ledger.append("control", controls)
            addition = canonical(ledger.commits[-1]) + "\n"
            if len(base.encode()) + len(addition.encode()) > target * MIB:
                ledger.commits.pop()
                break
            base += addition
        path = evidence / f"ledger-{target}m.jsonl"
        path.write_text(base)
        for name, command in commands.items():
            for iteration in range(iterations):
                # Fresh native process and cold validated ledger every time.
                with Native(command, env) as engine:
                    output = engine.call(request("", action="benchmark", ledger_file=str(path), include_canonical=False))
                row = {"engine": name, "target_mib": target, "iteration": iteration, **output}
                rows.append(row)
                if "error" in output:
                    failures.append(f"{name} {target} MiB: {output['error']}")
                elif output["metrics"]["stats"]["nodes"] != (128 if target == 1 else 4096):
                    failures.append(f"{name}: mixed graph node budget not exercised")
                else:
                    try:
                        verify_benchmark_serialization(output, base)
                        if target == 80:
                            stats = output["metrics"]["stats"]
                            assert stats["cancelled"] == 128, "drained retained history"
                            assert stats["available"] == stats["claimed"] == stats["claims"] == 0, "no active Claims or Work"
                    except (AssertionError, KeyError) as error:
                        failures.append(f"{name} {target} MiB: {error}")
        if target == 64:
            drained = drained_workload(ledger)
            drained_text = drained.text()
            drained_path = evidence / "ledger-64m-drained.jsonl"
            drained_path.write_text(drained_text)
            for name, command in commands.items():
                for iteration in range(iterations):
                    with Native(command, env) as engine:
                        output = engine.call(request("", action="benchmark", ledger_file=str(drained_path),
                                                     include_canonical=False))
                    rows.append({"engine": name, "target_mib": target, "iteration": iteration,
                                 "gate": "drained-admission-watermark-history", **output})
                    try:
                        assert "error" not in output, output
                        stats = output["metrics"]["stats"]
                        assert stats["nodes"] == 4096 and stats["cancelled"] == 128, "drained mixed graph"
                        assert stats["available"] == stats["claimed"] == stats["claims"] == 0, "no active Claims or Work"
                        verify_benchmark_serialization(output, drained_text)
                    except (AssertionError, KeyError) as error:
                        failures.append(f"{name} drained {target} MiB: {error}")
            nodes = [ledger.work(f"over-watermark-{i}", payload_size=16000) for i in range(4)]
            for node in nodes:
                node["graph_id"] = "new-admission"
                node["work_id"] = digest({"graph_id": node["graph_id"], "node_key": node["node_key"]})
            ledger.submit(nodes)
            refusal = evidence / "ledger-admission-refusal.jsonl"
            refusal_text = ledger.text()
            per_work = (ledger.policy["pools"]["default"]["retry"]["max_attempts"] *
                        (4 * ledger.policy["limits"]["evidence_bytes"] + 8192) +
                        2 * ledger.policy["limits"]["result_bytes"] +
                        2 * ledger.policy["limits"]["evidence_bytes"] + 8192)
            outstanding = 132 * per_work
            assert len(refusal_text.encode()) > ledger.policy["limits"]["ledger_bytes"]
            assert outstanding <= ledger.policy["limits"]["recovery_bytes"]
            assert len(refusal_text.encode()) + outstanding <= (
                ledger.policy["limits"]["ledger_bytes"] + ledger.policy["limits"]["recovery_bytes"])
            refusal.write_text(refusal_text)
            for name, command in commands.items():
                with Native(command, env) as engine:
                    output = engine.call(request("", ledger_file=str(refusal), action="benchmark", include_canonical=False))
                rows.append({"engine": name, "gate": "admission-watermark-refusal",
                             "ordinary_watermark_excess_bytes": len(refusal_text.encode()) - ledger.policy["limits"]["ledger_bytes"],
                             "candidate_outstanding_reserve_bytes": outstanding,
                             "reserve_failure_excluded_by_candidate_formula": True, **output})
                if "ledger_limit" not in output.get("error", ""):
                    failures.append(f"{name}: missing ledger_limit above admission watermark")
    # Demonstrate reserve refusal BEFORE the advertised 4096-Work node cap.
    insufficient = Ledger()
    nodes = [insufficient.work(f"reserve-{i}", payload_size=8) for i in range(256)]
    insufficient.policy["limits"]["recovery_bytes"] = 1024
    insufficient = Ledger(insufficient.policy)
    insufficient.submit(nodes)
    for name, command in commands.items():
        with Native(command, env) as engine:
            output = engine.call(request(insufficient.text(), include_canonical=False))
        rows.append({"engine": name, "gate": "recovery-headroom-refusal", **output})
        if "ledger_limit" not in output.get("error", ""):
            failures.append(f"{name}: missing exact ledger_limit for reserve refusal")
    # The default 4096-node graph ceiling is not a promise to admit 4096 Work.
    # Each legal 256-operation batch fits the operation cap; their outstanding
    # bounded closure reserve, not payload/graph/ordinary ledger bytes, refuses
    # the second batch under the unchanged default recovery allocation.
    default_reserve = Ledger()
    for batch in range(2):
        default_reserve.submit([default_reserve.work(f"default-reserve-{batch}-{i}", payload_size=8)
                                for i in range(256)])
    for name, command in commands.items():
        with Native(command, env) as engine:
            output = engine.call(request(default_reserve.text(), include_canonical=False))
        rows.append({"engine": name, "gate": "default-recovery-headroom-refusal", "work_nodes": 512,
                     "graph_ceiling": 4096, "input_bytes": len(default_reserve.text().encode()), **output})
        if not output.get("error", "").startswith("ledger_limit:"):
            failures.append(f"{name}: missing ledger_limit for 512 active Work under default recovery allocation")
    # Eleven unresolved maximum-size descriptors plus escaped future commit IDs
    # already exceed 48 KiB before any assignment envelope overhead. Seven fit
    # with the documented bounded envelope; neither answer comes from an engine.
    for count, refusal in ((7, False), (11, True)):
        ledger = future_reference_workload(count)
        for name, command in commands.items():
            with Native(command, env) as engine:
                output = engine.call(request(ledger.text(), include_canonical=False))
            rows.append({"engine": name, "gate": "future-result-assignment-budget",
                         "unresolved_references": count, "expected_refusal": refusal, **output})
            if refusal:
                if not output.get("error", "").startswith("assignment_limit:"):
                    failures.append(f"{name}: missing assignment_limit for eleven escaped future Result references")
            elif "error" in output:
                failures.append(f"{name}: seven bounded future Result references refused: {output['error']}")
    try:
        closure_rows, closure_failures = closure_byte_audit(commands, env)
        rows.extend(closure_rows)
        failures.extend(closure_failures)
        rows.extend(mock_contention(commands, env, evidence))
        rows.extend(local_finalization(commands, env))
    except AssertionError as error:
        failures.append(f"mock publisher contention: {error}")
    report = {
        "status": "failed" if failures else "passed", "measurements": rows, "failures": failures,
        "environment": {"platform": platform.platform(), "python": platform.python_version(),
                        "node": subprocess.check_output(["node", "--version"], text=True).strip(),
                        "go": subprocess.check_output(["go", "version"], text=True).strip()},
        "scope": "Local cold parse/replay, actual selection/packing/serialization and typed graph/resource budgets.",
        "measurement_note": "Single fresh-process samples on a shared development host; no isolation, confidence interval or SLO.",
        "unmet_gates": ["No runtime latency/memory/API SLO set or validated.", "No incremental replay API measured.",
                        "No live Git CAS, remote publication, supported-host dispatch or observation API-rate measurement.",
                        "Closure-byte audit samples structural envelopes; not proof of all permitted retry/native-group closures.",
                        "No CPU-time fairness, unbounded retained-history or writer-restriction claim."],
    }
    (evidence / "benchmark.json").write_text(json.dumps(report, indent=2) + "\n")
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixtures", type=Path, default=SPEC / "fixtures")
    parser.add_argument("--evidence-dir", type=Path, required=True)
    parser.add_argument("--benchmark", action="store_true")
    parser.add_argument("--benchmark-only", action="store_true")
    parser.add_argument("--sizes", default="1,16,64,80")
    parser.add_argument("--iterations", type=int, default=1)
    args = parser.parse_args()
    evidence = args.evidence_dir.resolve()
    if evidence == ROOT or ROOT in evidence.parents:
        raise SystemExit("--evidence-dir must be outside the checkout (session-only evidence)")
    if args.iterations < 1 or args.iterations > 10:
        raise SystemExit("--iterations must be 1..10")
    sizes = [int(size) for size in args.sizes.split(",")]
    if not sizes or sizes != sorted(set(sizes)) or any(size < 1 or size > 80 for size in sizes):
        raise SystemExit("--sizes must be increasing unique MiB sizes in 1..80")
    evidence.mkdir(parents=True, exist_ok=True)
    build = evidence / "go-build"
    build.mkdir(exist_ok=True)
    env = {**os.environ, "GOTMPDIR": str(build), "TMPDIR": str(build)}
    before_sources = engine_source_hashes()
    executable = evidence / "native-go"
    subprocess.run(["go", "build", "-o", str(executable), "./specs/work-queue/native_probe"], cwd=ROOT, env=env, check=True)
    commands = {"go": [str(executable)], "js": ["node", str(SPEC / "native_probe.cjs")]}
    summaries = {}
    if not args.benchmark_only:
        from verify_native_fixtures import conformance
        summaries["conformance"] = conformance(args.fixtures, commands, env, evidence, sys.modules[__name__])["status"]
    if args.benchmark or args.benchmark_only:
        summaries["benchmark"] = benchmark(commands, env, evidence, sizes, args.iterations)["status"]
    after_sources = engine_source_hashes()
    source_status = "stable" if before_sources == after_sources else "changed_during_run"
    (evidence / "sources.json").write_text(json.dumps({"status": source_status, "before": before_sources, "after": after_sources}, indent=2) + "\n")
    if source_status != "stable":
        summaries["source_identity"] = "failed"
    print(canonical({"evidence_dir": str(evidence), **summaries}))
    return 1 if "failed" in summaries.values() else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (AssertionError, RuntimeError, subprocess.CalledProcessError, OSError) as error:
        print(f"verification blocked/failed: {error}", file=sys.stderr)
        sys.exit(1)
