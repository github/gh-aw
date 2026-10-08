#!/usr/bin/env python3
"""Independent wire fixture construction, without importing either queue engine."""
import hashlib
import json
import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent


def canonical(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


def node_id(key):
    return digest(canonical({"graph_id": "graph", "node_key": key}))


def actor(role, **scope):
    return {"role": role, "principal": "1001", "repository": "owner/repo", **scope}


REF = "0" * 40
PROFILE = {
    "workflow": ".github/workflows/worker.lock.yml", "ref": REF, "principal": "1001",
    "trust_domain": "default", "credential_scope": "repository", "effect_scope": "owner/repo",
    "max_claims": 3, "share_keys": False,
}
POLICY = {
    "mode": "weighted-priority", "class_weights": [8, 4, 2, 1, 1],
    "accounting_weights": {"": 1},
    "producers": {"1001": {"pools": ["default"], "priorities": [1, 2, 3, 4, 5], "fairness_keys": [""]}},
    "pools": {"default": {
        "default_profile": "default", "profiles": {"default": PROFILE},
        "logical_limit": 16, "native_limit": 16, "allowed_repositories": ["owner/repo"],
        "max_observation_age_ms": 60000, "retry": {"max_attempts": 3, "backoff_ms": 30000},
        "reconciliation": {"max_attempts": 5, "deadline_ms": 300000},
    }},
    "limits": {
        "ledger_bytes": 67108864, "recovery_bytes": 16777216, "payload_bytes": 16384,
        "graph_nodes": 4096, "predecessors": 64, "pending_nodes": 4096, "operations": 256,
        "assignment_bytes": 49152, "result_bytes": 4096, "evidence_bytes": 1024,
        "observation_writes": 4096,
    },
}
RESOURCE = {
    "kind": "issue", "host": "github.com", "repository": "owner/repo",
    "repository_id": "1", "resource_id": "9007199254740993", "number": "7",
}


def node(key, edges=None):
    return {
        "kind": "Work", "work_id": node_id(key), "graph_id": "graph", "node_key": key,
        "pool": "default", "priority": 3, "fairness_key": "", "worker_profile": "default",
        "batch_trust_domain": "default", "payload": {"task": key},
        "depends_on": edges or [], "enqueued": 1000,
    }


def evidence(kind, source="github_api", **extras):
    return {
        "kind": kind, "source": source, "repository": "owner/repo", "workflow": PROFILE["workflow"],
        "ref": REF, "principal": "1001", "checked_at": 4000, "run_id": "202", "run_attempt": 1,
        **extras,
    }


def generate():
    commits = []
    expected = []
    work_states = {}
    grants = 0
    native = 0
    epoch = "epoch1"

    def append(role, name, kind, at, operations, params=None, scope=None, next_key="", reason="no_work"):
        nonlocal grants, native, epoch
        who = actor(role, **(scope or {}))
        parameters = params if params is not None else {"operations": operations}
        fingerprint = digest(canonical({"actor": who, "kind": kind, "parameters": parameters}))
        commit = {
            "version": 3, "id": "q" + str(len(commits)), "previous": commits[-1]["id"] if commits else None,
            "request": {"id": name, "kind": kind, "parameters": parameters, "fingerprint": fingerprint},
            "actor": who, "policy_epoch": epoch, "at": at, "operations": operations,
        }
        commits.append(commit)
        expected.append({
            "tip": commit["id"], "policy_epoch": epoch, "claims": grants, "native_reservations": native,
            "next_node": next_key, "reason": reason,
            "class_v": "12" if grants and epoch == "epoch1" else "0",
            "key_v": "3" if grants and epoch == "epoch1" else "0",
            "works": json.loads(json.dumps(work_states)),
        })
        return commit

    append("administrator", "genesis", "policy", 1000, [{"kind": "Policy", "epoch": epoch, "policy": POLICY}])
    nodes = [
        node("a"),
        node("b", [{"kind": "issue", "resource": RESOURCE, "condition": "completed"}]),
        node("c"),
        node("d", [{"kind": "work", "work_id": node_id("a")}]),
        node("e", [{"kind": "work", "work_id": node_id("c")}]),
    ]
    work_states.update({n["node_key"]: {"state": "available", "barrier": "none"} for n in nodes})
    append("producer", "submit", "submit", 2000, nodes, {"nodes": nodes}, next_key="a", reason="selected")
    observation = {
        "kind": "Observation", "observation_id": "o1", "resource": RESOURCE, "condition": "completed",
        "state": "ready", "observed_at": 2500, "credential_generation": "initial",
        "read_status": "ok", "state_reason": "completed", "resource_state": "closed",
    }
    append("reconciler", "observe", "observe", 2500, [observation], next_key="a", reason="selected")
    prefix = digest("grant")
    dispatch_id = "d_" + prefix + "_1"
    claims = []
    for i, key in enumerate(["a", "b", "c"], 1):
        claims.append({
            "kind": "Claim", "work_id": node_id(key), "claim_id": "c_" + prefix + "_" + str(i),
            "dispatch_id": dispatch_id, "handle": "h" + str(i),
            "observations": ["o1"] if key == "b" else [],
        })
        work_states[key]["state"] = "claimed"
    grants, native = 3, 1
    grant = append("administrator", "grant", "dispatch_next", 3000, claims, {
        "pool": "default", "max_claims": 3, "max_dispatches": 1, "max_bytes": 49152,
    }, reason="no_eligible_work")
    sender = actor("dispatcher", workflow=".github/workflows/dispatcher.lock.yml", run_id="101", run_attempt=1)
    append("dispatcher", "start", "dispatch", 4000, [{
        "kind": "Dispatch", "dispatch_id": dispatch_id, "state": "started", "sender": sender,
    }], scope={k: v for k, v in sender.items() if k not in ("role", "principal", "repository")},
        reason="no_eligible_work")
    binding = {
        "run_id": "202", "run_attempt": 1, "repository": "owner/repo", "workflow": PROFILE["workflow"],
        "ref": REF, "principal": "1001", "event": "workflow_dispatch",
    }
    append("reconciler", "bind", "dispatch", 4100, [{
        "kind": "Dispatch", "dispatch_id": dispatch_id, "state": "bound", "run": binding,
        "evidence": evidence("reconciliation"),
    }], reason="no_eligible_work")

    def finish(index, outcome, at):
        key = ["a", "b", "c"][index]
        claim = claims[index]
        scope = {"workflow": PROFILE["workflow"], "run_id": "202", "run_attempt": 1,
                 "dispatch_id": dispatch_id, "claim_handle": claim["handle"]}
        if outcome == "completed":
            op = {"kind": "Completion", "work_id": claim["work_id"], "claim_id": claim["claim_id"],
                  "dispatch_id": dispatch_id, "claim_handle": claim["handle"], "run_id": "202", "run_attempt": 1}
            work_states[key] = {"state": "completed", "barrier": "pending"}
        else:
            op = {"kind": "ClaimCancellation", "work_id": claim["work_id"], "claim_id": claim["claim_id"],
                  "reason": "worker_cancelled", "retry_not_before": at + 30000}
            work_states[key]["state"] = "available"
        return append("worker", "finish-" + key, "finish", at, [op], {
            "dispatch_id": dispatch_id, "claim_handle": claim["handle"], "outcome": outcome,
        }, scope, next_key="d" if work_states["a"]["barrier"] == "verified" else "",
            reason="selected" if work_states["a"]["barrier"] == "verified" else "no_eligible_work")

    completion_a = finish(0, "completed", 4200)
    work_states["a"]["barrier"] = "verified"
    append("reconciler", "result-a", "result", 4300, [{
        "kind": "Result", "work_id": claims[0]["work_id"], "claim_id": claims[0]["claim_id"],
        "completion_id": completion_a["id"], "descriptor": {"ok": True},
        "evidence": evidence("delivery", "verified_receipts", receipt="verified-h1"),
    }], next_key="d", reason="selected")
    finish(1, "cancelled", 4400)
    completion_c = finish(2, "completed", 4500)
    work_states["c"]["barrier"] = "failed"
    append("reconciler", "failure-c", "delivery_failure", 4600, [{
        "kind": "DeliveryFailure", "work_id": claims[2]["work_id"], "claim_id": claims[2]["claim_id"],
        "completion_id": completion_c["id"], "reason": "delivery_unknown", "disposition": "unknown",
        "evidence": evidence("terminal_run", status="completed", conclusion="failure", attempts=5, effects="unknown"),
    }], next_key="d", reason="selected")
    work_states["b"]["state"] = "cancelled"
    append("administrator", "cancel-b", "cancel_work", 4700, [{
        "kind": "WorkCancellation", "work_id": node_id("b"), "reason": "abandoned",
    }], next_key="d", reason="selected")
    native = 0
    append("reconciler", "release", "release", 4800, [{
        "kind": "Release", "dispatch_id": dispatch_id,
        "evidence": evidence("terminal_run", status="completed", conclusion="failure"),
    }], next_key="d", reason="selected")
    append("administrator", "pause", "control", 4900, [{
        "kind": "Control", "control": "grants_paused", "value": True, "reason": "drain",
    }], reason="grants_paused")
    for key, at in [("d", 5000), ("e", 5100)]:
        work_states[key]["state"] = "cancelled"
        append("administrator", "cancel-" + key, "cancel_work", at, [{
            "kind": "WorkCancellation", "work_id": node_id(key), "reason": "drain",
        }], reason="grants_paused")
    epoch = "epoch2"
    append("administrator", "policy2", "policy", 5200, [{
        "kind": "Policy", "epoch": epoch, "policy": POLICY,
    }], reason="grants_paused")
    assignment = {
        "version": 3, "dispatch_id": dispatch_id, "request_id": "grant", "commit_id": grant["id"],
        "policy_epoch": "epoch1", "pool": "default", "worker_profile": "default",
        "claims": [{"handle": c["handle"], "claim_id": c["claim_id"], "work_id": c["work_id"],
                    "work": {"task": key}, "result_refs": []} for key, c in zip(["a", "b", "c"], claims)],
    }
    return {
        "description": "Independent FIFO answers and all twelve operations; every causal prefix is checked.",
        "commits": commits, "canonical": "\n".join(canonical(c) for c in commits) + "\n",
        "prefixes": expected, "assignment": assignment,
    }


def main():
    value = json.dumps(generate(), sort_keys=True, ensure_ascii=False, indent=2) + "\n"
    path = HERE / "canonical-prefix.json"
    if "--check" in sys.argv:
        if not path.exists() or json.loads(path.read_text()) != json.loads(value):
            print("canonical prefix fixture drift; run python3 specs/work-queue/fixtures/generate_prefix.py", file=sys.stderr)
            return 1
    else:
        path.write_text(value)
    return 0


if __name__ == "__main__":
    sys.exit(main())
