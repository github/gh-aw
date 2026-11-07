"""Regression checks for Archivx's bounded collection and coverage contract."""

import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / ".github/workflows/archivx-agentic-workflows-analyzer.md"
LOCK = ROOT / ".github/workflows/archivx-agentic-workflows-analyzer.lock.yml"


class ArchivxAnalyzerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = SOURCE.read_text()
        cls.frontmatter = cls.source.split("---", 2)[1]
        cls.prompt = re.sub(r"\s+", " ", cls.source.split("---", 2)[2])
        cls.lock = LOCK.read_text()

    def test_collection_is_compact_and_bounded_in_source_and_lock(self):
        command = (
            "./gh-aw logs --start-date -7d --count 2000 --artifacts usage "
            "--timeout 5 --max-storage 1024 -o /tmp/gh-aw/aw-mcp/logs"
        )
        self.assertIn(command, self.frontmatter)
        self.assertIn(command, self.lock)

    def test_incomplete_collection_cannot_be_reported_as_a_full_week(self):
        for requirement in (
            "summary.json",
            "continuation",
            "before_run_id",
            "Deduplicate combined pages by run ID",
            "a continuation times out",
            "completeness cannot be established",
            "6 hours = 3.6%",
            "not the fraction of all runs collected",
            "coverage as unknown",
            "sampled counts and sampled success rates",
        ):
            with self.subTest(requirement=requirement):
                self.assertIn(requirement, self.prompt)

    def test_all_publications_include_coverage(self):
        self.assertIn("into the SVG, pull request body, and discussion", self.prompt)
        self.assertIn('"Partial coverage — 6h / 168h (3.6%)', self.prompt)
        self.assertIn('Use `"Past 7 days"` only when complete', self.prompt)
        self.assertIn("| Coverage | Complete / Partial / Unknown", self.prompt)
        self.assertIn("Do not extrapolate partial results to weekly health", self.prompt)

    def test_weekly_data_count_excludes_summary_and_24h_guidance(self):
        self.assertNotIn("aw-logs-24h-fetch-prompt.md", self.source)
        self.assertNotIn("ls /tmp/gh-aw/aw-mcp/logs/ | wc -l", self.prompt)
        self.assertIn("jq '.runs | length'", self.prompt)
        self.assertIn("fewer than 5 collected runs", self.prompt)


if __name__ == "__main__":
    unittest.main()
