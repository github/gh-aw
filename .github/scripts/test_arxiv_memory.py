#!/usr/bin/env python3
"""Run with python3 -m unittest discover -s .github/scripts -p test_arxiv_memory.py."""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

import arxiv_memory as storage


ROOT = Path(__file__).resolve().parents[2]


class PaperMemoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.memory = root / "memory"
        self.data = root / "data"
        self.cache = root / "cache"
        self.data.mkdir()
        self.cache.mkdir()

    def write_json(self, path, value):
        path.write_text(storage.json_text(value), encoding="utf-8")

    def paper(self, number, title="Paper title"):
        return {
            "id": f"2610.{number:05d}v1", "title": title, "published": "2026-10-27",
            "categories": ["cs.AI", "cs.SE"], "url": f"https://arxiv.org/abs/2610.{number:05d}v1",
        }

    def batch(self, papers, relevant=False):
        self.write_json(self.data / "papers.json", {"papers": papers, "fetched_at": "2026-10-27"})
        storage.prepare(self.memory, self.data, self.cache)
        new = storage.load_json(self.data / "new-papers.json")["papers"]
        results = [{"id": paper["id"], "relevant": relevant} for paper in new]
        self.write_json(self.data / "screened-papers.json", {"papers": results})

    def snapshot(self):
        return {file.name: file.read_bytes() for file in self.memory.glob("*")}

    def update(self):
        storage.update(self.memory, self.data, self.cache)

    def assert_rejected_without_writes(self, error):
        before = self.snapshot()
        cache = (self.cache / "seen-paper-ids.json").read_bytes()
        with self.assertRaisesRegex((ValueError, KeyError), error):
            self.update()
        self.assertEqual(self.snapshot(), before)
        self.assertEqual((self.cache / "seen-paper-ids.json").read_bytes(), cache)

    def test_first_run_and_all_irrelevant_papers(self):
        self.batch([self.paper(1), self.paper(2)])
        self.update()
        records = storage.load_json(self.memory / "paper-index-0001.json")["papers"]
        self.assertEqual([paper["id"] for paper in records], [self.paper(1)["id"], self.paper(2)["id"]])
        self.assertTrue(all(not paper["relevant"] for paper in records))
        ledger = (self.memory / "paper-ledger-0001.md").read_text()
        self.assertEqual(ledger.count("- **Relevant**: No"), 2)
        self.assertNotIn("- **Opportunity**:", ledger)
        self.assertEqual(len(storage.load_json(self.cache / "seen-paper-ids.json")["ids"]), 2)

    def test_legacy_history_near_failed_boundary_is_not_copied_or_appended(self):
        self.memory.mkdir()
        (self.memory / "paper-ledger.md").write_text("# Legacy table and block history\n" + "x" * 96000)
        self.write_json(self.memory / "paper-index.json", {"papers": [{"id": "old", "published": "2026-07-30"}]})
        legacy = self.snapshot()
        self.batch([self.paper(i, "A realistic title " * 4) for i in range(25)], relevant=True)
        results = storage.load_json(self.data / "screened-papers.json")
        results["papers"][0].update(opportunity="Improve typed safe outputs", area="safe-outputs")
        self.write_json(self.data / "screened-papers.json", results)
        self.update()
        for name, content in legacy.items():
            self.assertEqual((self.memory / name).read_bytes(), content)
        self.assertEqual(len(storage.known_ids(self.memory)), 26)
        self.assertIn("Improve typed safe outputs", (self.memory / "paper-ledger-0001.md").read_text())
        self.assertLess(sum(file.stat().st_size for file in self.memory.glob("*-0001.*")), storage.MAX_PATCH_BYTES)

    def test_cache_expiry_and_failed_push_reconcile_against_all_durable_indices(self):
        self.batch([self.paper(1)])
        self.update()
        self.write_json(self.memory / "paper-index.json", {"papers": [{"id": "legacy"}]})
        # A previous run saved its cache but failed its repo-memory push.
        self.write_json(self.cache / "seen-paper-ids.json", {"ids": [self.paper(2)["id"]]})
        self.batch([self.paper(1), self.paper(2), {"id": "legacy"}])
        self.assertEqual(storage.load_json(self.data / "new-papers.json")["papers"], [self.paper(2)])
        (self.cache / "seen-paper-ids.json").unlink()
        storage.prepare(self.memory, self.data, self.cache)
        self.assertEqual(storage.load_json(self.data / "new-papers.json")["new_count"], 1)

    def test_repeated_update_is_idempotent(self):
        self.batch([self.paper(1)])
        self.update()
        before = self.snapshot()
        self.update()
        self.assertEqual(before, self.snapshot())
        self.batch([self.paper(1)])
        self.assertEqual(storage.load_json(self.data / "new-papers.json")["new_count"], 0)
        self.update()
        self.assertEqual(before, self.snapshot())

    def test_continued_growth_rotates_unicode_shards_without_losing_history(self):
        sealed = {}
        for batch in range(12):
            self.batch([self.paper(i, "Unicode \u7814\u7a76 \U0001f52c " * 25)
                        for i in range(batch * 25, (batch + 1) * 25)])
            self.update()
            for name, content in sealed.items():
                self.assertEqual((self.memory / name).read_bytes(), content)
            numbers = sorted(self.memory.glob("paper-index-*.json"))
            for index in numbers[:-1]:
                sealed[index.name] = index.read_bytes()
                ledger = self.memory / index.name.replace("index", "ledger").replace(".json", ".md")
                sealed[ledger.name] = ledger.read_bytes()
            for file in self.memory.glob("*"):
                self.assertLessEqual(file.stat().st_size, storage.SHARD_BYTES)
        self.assertGreater(len(list(self.memory.glob("paper-index-*.json"))), 2)
        self.assertEqual(storage.known_ids(self.memory), {self.paper(i)["id"] for i in range(300)})
        # Exercise the push job's real JSON formatting and net-patch measurement.
        subprocess.run(["git", "init", "--quiet", str(self.memory)], check=True)
        subprocess.run(["git", "-C", str(self.memory), "add", "."], check=True)
        subprocess.run(["git", "-C", str(self.memory), "-c", "user.name=Test", "-c",
                        "user.email=test@example.com", "-c", "commit.gpgsign=false",
                        "commit", "--quiet", "-m", "Fixture"], check=True)
        self.batch([self.paper(i) for i in range(300, 325)])
        self.update()
        script = """
          const fs = require('fs');
          const path = require('path');
          const {execFileSync} = require('child_process');
          const {getPatchDiffSizeBytes} = require(process.argv[1]);
          const dir = process.argv[2];
          for (const name of fs.readdirSync(dir).filter(n => n.endsWith('.json'))) {
            const file = path.join(dir, name);
            fs.writeFileSync(file, JSON.stringify(JSON.parse(fs.readFileSync(file)), null, 2) + '\\n');
          }
          execFileSync('git', ['add', '.'], {cwd: dir});
          const diff = execFileSync('git', ['diff', '--cached', '--no-renames'], {cwd: dir, encoding: 'utf8'});
          console.log(getPatchDiffSizeBytes(diff));
        """
        result = subprocess.run(["node", "-e", script, str(ROOT / "actions/setup/js/git_patch_utils.cjs"),
                                 str(self.memory)], check=True, capture_output=True, text=True)
        self.assertLess(int(result.stdout), storage.MAX_PATCH_BYTES)
        for file in self.memory.glob("*.json"):
            self.assertLessEqual(file.stat().st_size, storage.SHARD_BYTES)
        self.assertEqual(len(storage.known_ids(self.memory)), 325)

    def test_rotation_when_index_is_full_before_markdown(self):
        self.batch([self.paper(1)])
        self.update()
        index = self.memory / "paper-index-0001.json"
        history = storage.load_json(index)
        history["papers"][0]["padding"] = "x" * 81000
        self.write_json(index, history)
        before = index.read_bytes()
        self.batch([self.paper(2, "Long title " * 100)])
        self.update()
        self.assertEqual(index.read_bytes(), before)
        self.assertTrue((self.memory / "paper-index-0002.json").exists())

    def test_invalid_screening_results_fail_before_writing(self):
        self.batch([self.paper(1)])
        for papers, error in [
            ([], "each new paper"),
            ([{"id": self.paper(1)["id"], "relevant": False}] * 2, "each new paper"),
            ([{"id": "unknown", "relevant": False}], "each new paper"),
            ([{"id": self.paper(1)["id"], "relevant": "false"}], "boolean"),
            ([{"id": self.paper(1)["id"], "relevant": True, "opportunity": []}], "string"),
        ]:
            with self.subTest(papers=papers):
                self.write_json(self.data / "screened-papers.json", {"papers": papers})
                self.assert_rejected_without_writes(error)

    def test_single_oversized_record_fails_without_writes(self):
        self.batch([self.paper(1, "\u7814" * 40000)])
        self.assert_rejected_without_writes("80 KiB")

    def test_patch_budget_is_preserved(self):
        self.batch([self.paper(i, "x" * 1500) for i in range(25)])
        self.assert_rejected_without_writes("50 KiB")

    def test_file_budget_exhaustion_is_explicit_and_preserves_history(self):
        self.memory.mkdir()
        for i in range(99):
            (self.memory / f"archive-{i}.md").write_text("Preserved history\n")
        self.batch([self.paper(1)])
        self.assert_rejected_without_writes("100-file")

    def test_malformed_index_is_not_silently_discarded(self):
        self.memory.mkdir()
        (self.memory / "paper-index.json").write_text("broken JSON")
        with self.assertRaises(json.JSONDecodeError):
            self.batch([self.paper(1)])


if __name__ == "__main__":
    unittest.main()
