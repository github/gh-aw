"""Regression checks for the shared local PageIndex workflow."""

import contextlib
import importlib.util
import io
import json
import os
import re
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path
from unittest.mock import patch

import yaml


ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / ".github/workflows/shared/pageindex.md"
PAGEINDEX_AVAILABLE = importlib.util.find_spec("pageindex") is not None

if not PAGEINDEX_AVAILABLE:
    print(
        "Skipping PageIndex workflow tests: requires pageindex==0.5.3; "
        "run with `make test-pageindex-workflow`.",
        file=sys.stderr,
    )


@unittest.skipUnless(
    PAGEINDEX_AVAILABLE,
    "requires pageindex==0.5.3; run with `make test-pageindex-workflow`",
)
class PageIndexWorkflowTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = SOURCE.read_text()
        match = re.search(r"python - <<'PY'\n(.*?)          PY", cls.source, re.DOTALL)
        cls.script = textwrap.dedent(match.group(1))

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "repo"
        self.output = Path(self.temp.name) / "index"
        self.root.mkdir()

    def document(self, name, content):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def index(self, patterns="docs/**/*.md,docs/**/*.mdx"):
        script = self.script.replace("/tmp/gh-aw/pageindex/index", str(self.output))
        with patch.dict(os.environ, {
            "GITHUB_WORKSPACE": str(self.root),
            "PAGEINDEX_PATTERNS": patterns,
        }), patch("subprocess.check_output", return_value="fixture-revision\n"), \
                patch("socket.socket.connect", side_effect=AssertionError("Network forbidden")):
            with contextlib.redirect_stdout(io.StringIO()):
                exec(compile(script, str(SOURCE), "exec"), {})
        return json.loads((self.output / "catalog.json").read_text())

    def tree(self, entry):
        return json.loads(Path(entry["tree"]).read_text())

    def assert_unique_node_ids(self, nodes):
        node_ids = []

        def visit(current_nodes):
            for node in current_nodes:
                self.assertIn("node_id", node)
                self.assertIsInstance(node["node_id"], str)
                self.assertTrue(node["node_id"])
                node_ids.append(node["node_id"])
                visit(node.get("nodes", []))

        visit(nodes)
        self.assertEqual(len(node_ids), len(set(node_ids)))

    def test_hierarchy_line_numbers_and_text_are_preserved_offline(self):
        self.document(
            "docs/guide.md",
            "Introduction\n# Guide\nBody\n## Details\nEvidence\n"
            "```md\n# Not a heading\n```\n",
        )
        catalog = self.index()
        self.assertEqual(len(catalog), 1)
        entry = catalog[0]
        self.assertEqual(entry["source"], "docs/guide.md")
        self.assertNotIn('"text"', json.dumps(entry["structure"]))
        tree = self.tree(entry)["structure"]
        self.assertEqual(tree[0]["text"], "Introduction")
        self.assertEqual(tree[1]["title"], "Guide")
        self.assertEqual(tree[1]["line_num"], 2)
        child = tree[1]["nodes"][0]
        self.assertEqual(child["title"], "Details")
        self.assertEqual(child["line_num"], 4)
        self.assertIn("Evidence", child["text"])
        self.assertEqual(len(tree[1]["nodes"]), 1)
        self.assert_unique_node_ids(tree)

    def test_bold_paragraphs_do_not_detach_subsections(self):
        self.document(
            "docs/guide.md",
            "# Guide\n## Principles\n**Important principle**\nEvidence\n"
            "### Details\nNested evidence",
        )
        tree = self.tree(self.index()[0])["structure"]
        self.assertEqual(len(tree), 2)
        principle = tree[1]["nodes"][0]
        self.assertEqual(principle["title"], "Principles")
        self.assertIn("**Important principle**", principle["text"])
        self.assertEqual(principle["nodes"][0]["title"], "Details")

    def test_duplicate_basenames_and_mdx_have_distinct_trees(self):
        for name in ("docs/a/README.md", "docs/b/README.md", "docs/page.mdx"):
            self.document(name, "# Overview\nContent")
        catalog = self.index("docs/**/*.md,docs/**/*.mdx,docs/**/*.md")
        self.assertEqual(len(catalog), 3)
        self.assertEqual(len({entry["tree"] for entry in catalog}), 3)
        for entry in catalog:
            tree = self.tree(entry)
            self.assertEqual(tree["source"], entry["source"])
            self.assert_unique_node_ids(tree["structure"])

    def test_headingless_and_empty_documents_remain_readable(self):
        self.document("docs/plain.md", "No headings, but important evidence.")
        self.document("docs/empty.md", "")
        entries = {entry["source"]: self.tree(entry) for entry in self.index()}
        self.assertEqual(entries["docs/plain.md"]["structure"][0]["text"],
                         "No headings, but important evidence.")
        self.assertEqual(entries["docs/empty.md"]["structure"][0]["text"], "")

    def test_symlink_escapes_and_non_markdown_are_not_indexed(self):
        outside = Path(self.temp.name) / "outside.md"
        outside.write_text("Private content")
        (self.root / "docs").mkdir()
        (self.root / "docs/linked.md").symlink_to(outside)
        (self.root / "escape").symlink_to(outside.parent, target_is_directory=True)
        self.document("docs/code.py", "not Markdown")
        self.assertEqual(self.index("docs/*,escape/outside.md"), [])

    def test_absolute_and_traversal_patterns_are_rejected(self):
        for pattern in ("/tmp/*.md", "../*.md", "docs/../../*.md", ""):
            with self.subTest(pattern=pattern), self.assertRaises(ValueError):
                self.index(pattern)

    def test_rebuild_reflects_edits_and_removals(self):
        path = self.document("docs/guide.md", "# Old\nFirst version")
        first = self.tree(self.index()[0])
        path.write_text("# New\nUpdated version")
        second = self.tree(self.index()[0])
        self.assertNotIn("sha256", first)
        self.assertNotIn("sha256", second)
        self.assertEqual(second["structure"][1]["title"], "New")
        self.assertIn("Updated version", second["structure"][1]["text"])
        path.unlink()
        self.assertEqual(self.index(), [])

    def test_consumers_import_the_shared_component(self):
        docs = (ROOT / ".github/workflows/daily-doc-updater.md").read_text()
        specs = (ROOT / ".github/workflows/spec-librarian.md").read_text()
        self.assertIn("- shared/pageindex.md", docs)
        self.assertIn("- uses: shared/pageindex.md", specs)
        self.assertIn('patterns: "pkg/**/README.md"', specs)
        self.assertIn("exhaustive checks", specs)
        self.assertNotIn("secrets.", self.source)
        self.assertIn('"pageindex==0.5.3"', self.source)
        self.assertIn("inventory and search the current source files directly", self.source)
        self.index()
        self.assertEqual((self.output / "revision.txt").read_text(), "fixture-revision")

    def test_indexing_is_isolated_with_exact_cache_handoff(self):
        self.assertIn("needs: [pageindex]", self.source)
        self.assertIn("fetch-depth: 1", self.source)
        self.assertIn("persist-credentials: false", self.source)
        self.assertIn("ref: ${{ github.sha }}", self.source)
        self.assertIn("actions/cache/save@", self.source)
        self.assertIn("key: ${{ needs.pageindex.outputs.cache-key }}", self.source)
        self.assertIn("fail-on-cache-miss: true", self.source)
        self.assertIn("INDEX_SOURCE_HASH:", self.source)
        self.assertIn('printf \'%s\' "$PAGEINDEX_PATTERNS"', self.source)
        self.assertNotIn("restore-keys:", self.source)
        self.assertNotIn("network:", self.source)
        agent_steps = self.source.split("\nsteps:\n", 1)[1].split("\n---", 1)[0]
        self.assertNotIn("uv ", agent_steps)
        self.assertNotIn("python", agent_steps)

    def test_uv_setup_and_three_day_package_cooldown(self):
        self.assertLess(self.source.index("uses: astral-sh/setup-uv@"),
                        self.source.index("uv venv"))
        self.assertIn('version: "0.13.7"', self.source)
        self.assertIn("--exclude-newer \"$(date -u -d '3 days ago'", self.source)
        self.assertIn("--only-binary=:all:", self.source)

    def test_compiled_consumers_use_separate_indexing_job(self):
        for name in ("daily-doc-updater", "spec-librarian"):
            with self.subTest(workflow=name):
                workflow = yaml.safe_load(
                    (ROOT / f".github/workflows/{name}.lock.yml").read_text()
                )
                jobs = workflow["jobs"]
                self.assertIn("pageindex", jobs["agent"]["needs"])
                self.assertIn("activation", jobs["pageindex"]["needs"])
                self.assertEqual(jobs["pageindex"]["permissions"], {"contents": "read"})
                steps = jobs["pageindex"]["steps"]
                checkout = next(step for step in steps
                                if step.get("uses", "").startswith("actions/checkout@"))
                self.assertEqual(checkout["with"]["fetch-depth"], 1)
                self.assertFalse(checkout["with"]["persist-credentials"])
                agent_steps = jobs["agent"]["steps"]
                restore = next(step for step in agent_steps
                               if step.get("name") == "Restore PageIndex from indexing job")
                self.assertEqual(restore["with"]["key"],
                                 "${{ needs.pageindex.outputs.cache-key }}")
                self.assertTrue(restore["with"]["fail-on-cache-miss"])
                self.assertFalse(any("uv pip install" in step.get("run", "")
                                     for step in agent_steps))


if __name__ == "__main__":
    unittest.main()
