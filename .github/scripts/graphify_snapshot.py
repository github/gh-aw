"""Seal and authenticate Graphify snapshots outside the agent's writable mounts."""

import argparse
import hashlib
import json
import os
from datetime import datetime, timezone
from pathlib import Path


COMPATIBILITY = {
    "schema": 1,
    "tool": "graphifyy==0.9.106",
    "dependency_cutoff": "2026-11-12T00:00:00Z",
    "corpus": "repository-code-only",
}


def snapshot_digest(graph, manifest):
    return hashlib.sha256(graph + b"\0" + manifest).hexdigest()


def seal(root):
    graph = (root / "build/graphify-out/graph.json").read_bytes()
    data = json.loads(graph)
    if (
        not isinstance(data, dict)
        or not isinstance(data.get("nodes"), list)
        or not data["nodes"]
        or not isinstance(data.get("edges"), list)
    ):
        raise ValueError("Graphify extraction produced an empty or invalid code graph")
    index = root / "index"
    index.mkdir(parents=True, exist_ok=False)
    (index / "graph.json").write_bytes(graph)
    manifest = json.dumps({
        **COMPATIBILITY,
        "source_sha": os.environ["SOURCE_SHA"],
        "built_at": datetime.now(timezone.utc).isoformat(),
        "graph_sha256": hashlib.sha256(graph).hexdigest(),
    }, sort_keys=True).encode()
    (index / "manifest.json").write_bytes(manifest)
    key = "graphify-v1-0.9.106-linux-{}-{}-{}".format(
        os.environ["SOURCE_SHA"], os.environ["BUILD_ID"], os.environ["BUILD_ATTEMPT"]
    )
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        output.write(f"cache-key={key}\nsnapshot-digest={snapshot_digest(graph, manifest)}\n")
        output.write(f"source-sha={os.environ['SOURCE_SHA']}\n")


def validate(root):
    index = root / "index"
    if index.is_symlink() or {p.name for p in index.iterdir()} != {"graph.json", "manifest.json"}:
        raise ValueError("Graphify snapshot must contain exactly graph.json and manifest.json")
    if any(p.is_symlink() or not p.is_file() for p in index.iterdir()):
        raise ValueError("Graphify snapshot contains a symlink or non-file")
    graph = (index / "graph.json").read_bytes()
    manifest_bytes = (index / "manifest.json").read_bytes()
    if not os.environ["EXPECTED_KEY"] or os.environ["RESTORED_KEY"] != os.environ["EXPECTED_KEY"]:
        raise ValueError("Graphify cache key does not match the trusted publisher")
    if snapshot_digest(graph, manifest_bytes) != os.environ["EXPECTED_DIGEST"]:
        raise ValueError("Graphify snapshot digest does not match the trusted publisher")
    manifest = json.loads(manifest_bytes)
    expected = {
        **COMPATIBILITY,
        "source_sha": os.environ["EXPECTED_SOURCE_SHA"],
        "graph_sha256": hashlib.sha256(graph).hexdigest(),
    }
    if any(manifest.get(key) != value for key, value in expected.items()):
        raise ValueError("Graphify snapshot is incompatible with the trusted publisher")
    if not expected["source_sha"] or expected["source_sha"] != os.environ["CHECKOUT_SHA"]:
        raise ValueError("Graphify snapshot does not describe this workflow's exact source revision")
    age = (datetime.now(timezone.utc) - datetime.fromisoformat(manifest["built_at"])).total_seconds()
    if not 0 <= age <= 86400:
        raise ValueError("Graphify snapshot must be at most 24 hours old")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("seal", "validate"))
    parser.add_argument("root", type=Path)
    args = parser.parse_args()
    {"seal": seal, "validate": validate}[args.operation](args.root)
