"""Regression checks for the trusted Graphify index/search handoff."""

import hashlib
import importlib.util
import json
import os
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location(
    "graphify_snapshot", ROOT / ".github/scripts/graphify_snapshot.py"
)
snapshot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(snapshot)


class GraphifySnapshotTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.build = self.root / "build/graphify-out"
        self.build.mkdir(parents=True)
        self.graph = json.dumps({
            "nodes": [{"id": "example", "source_file": "pkg/example.go"}],
            "edges": [],
        }).encode()
        (self.build / "graph.json").write_bytes(self.graph)
        self.env = {
            "SOURCE_SHA": "trusted-revision",
            "BUILD_ID": "123",
            "BUILD_ATTEMPT": "1",
            "GITHUB_OUTPUT": str(self.root / "output"),
        }
        self.env_patch = patch.dict(os.environ, self.env)
        self.env_patch.start()
        self.addCleanup(self.env_patch.stop)

    def seal(self):
        snapshot.seal(self.root)
        outputs = dict(line.split("=", 1) for line in (self.root / "output").read_text().splitlines())
        os.environ.update({
            "EXPECTED_KEY": outputs["cache-key"],
            "RESTORED_KEY": outputs["cache-key"],
            "EXPECTED_DIGEST": outputs["snapshot-digest"],
            "EXPECTED_SOURCE_SHA": outputs["source-sha"],
            "CHECKOUT_SHA": "trusted-revision",
        })

    def rewrite_manifest(self, **changes):
        path = self.root / "index/manifest.json"
        manifest = json.loads(path.read_bytes())
        manifest.update(changes)
        path.write_text(json.dumps(manifest))
        os.environ["EXPECTED_DIGEST"] = snapshot.snapshot_digest(self.graph, path.read_bytes())

    def test_sealed_snapshot_validates_without_writing_and_preserves_source_paths(self):
        self.seal()
        before = {p.name: p.read_bytes() for p in (self.root / "index").iterdir()}
        snapshot.validate(self.root)
        self.assertEqual(before, {p.name: p.read_bytes() for p in (self.root / "index").iterdir()})
        self.assertEqual(before["graph.json"], self.graph)
        manifest = json.loads(before["manifest.json"])
        self.assertEqual(manifest["graph_sha256"], hashlib.sha256(self.graph).hexdigest())

    def test_tampering_cannot_authenticate_using_a_digest_inside_the_cache(self):
        self.seal()
        graph = self.root / "index/graph.json"
        graph.write_bytes(self.graph + b" ")
        path = self.root / "index/manifest.json"
        manifest = json.loads(path.read_bytes())
        manifest["graph_sha256"] = hashlib.sha256(graph.read_bytes()).hexdigest()
        path.write_text(json.dumps(manifest))
        with self.assertRaisesRegex(ValueError, "digest"):
            snapshot.validate(self.root)

    def test_wrong_cache_key_source_and_compatibility_are_rejected(self):
        self.seal()
        for variable in ("RESTORED_KEY", "EXPECTED_KEY", "CHECKOUT_SHA", "EXPECTED_SOURCE_SHA"):
            with self.subTest(variable=variable), patch.dict(os.environ, {variable: "different"}):
                with self.assertRaises(ValueError):
                    snapshot.validate(self.root)
        self.rewrite_manifest(tool="graphifyy==other")
        with self.assertRaisesRegex(ValueError, "incompatible"):
            snapshot.validate(self.root)

    def test_age_boundaries_and_future_snapshots_are_rejected(self):
        self.seal()
        for delta in (timedelta(hours=24, seconds=1), timedelta(seconds=-60)):
            with self.subTest(delta=delta):
                self.rewrite_manifest(built_at=(datetime.now(timezone.utc) - delta).isoformat())
                with self.assertRaisesRegex(ValueError, "24 hours"):
                    snapshot.validate(self.root)

    def test_missing_extra_and_symlinked_snapshot_files_are_rejected(self):
        self.seal()
        index = self.root / "index"
        extra = index / "extra"
        extra.write_text("not part of the authenticated snapshot")
        with self.assertRaisesRegex(ValueError, "exactly"):
            snapshot.validate(self.root)
        extra.unlink()
        graph = index / "graph.json"
        graph.unlink()
        with self.assertRaisesRegex(ValueError, "exactly"):
            snapshot.validate(self.root)
        graph.symlink_to(self.build / "graph.json")
        with self.assertRaisesRegex(ValueError, "symlink"):
            snapshot.validate(self.root)

    def test_empty_extraction_is_not_published_and_existing_snapshots_are_not_overwritten(self):
        for graph in ({"nodes": [], "edges": []}, {"nodes": [{}]}, []):
            with self.subTest(graph=graph):
                (self.build / "graph.json").write_text(json.dumps(graph))
                with self.assertRaisesRegex(ValueError, "empty"):
                    snapshot.seal(self.root)
                self.assertFalse((self.root / "index").exists())
        (self.build / "graph.json").write_bytes(self.graph)
        self.seal()
        with self.assertRaises(FileExistsError):
            snapshot.seal(self.root)


if __name__ == "__main__":
    unittest.main()
