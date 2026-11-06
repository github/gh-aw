"""Regression checks for the PR specification reviewer's publication contract."""

import json
import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / ".github/workflows/pr-specification-reviewer.md"
LOCK = ROOT / ".github/workflows/pr-specification-reviewer.lock.yml"


class PRSpecificationReviewerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = SOURCE.read_text()
        cls.frontmatter = cls.source.split("---", 2)[1]
        cls.prompt = cls.source.split("---", 2)[2]
        cls.text = re.sub(r"\s+", " ", cls.prompt)
        cls.lock = LOCK.read_text()
        config = re.search(
            r'GH_AW_SAFE_OUTPUTS_CONFIG: ("[^\n]+")', cls.lock
        )
        if config is None:
            raise AssertionError("Compiled safe-output configuration is missing")
        cls.config = json.loads(json.loads(config.group(1)))

    def test_reviews_new_revisions_and_excludes_drafts(self):
        self.assertIn(
            "types: [opened, synchronize, reopened, ready_for_review]",
            self.frontmatter,
        )
        self.assertIn("name: review-specs", self.frontmatter)
        self.assertIn("github.event.pull_request.draft == false", self.lock)
        self.assertIn("closed, merged, or draft PR", self.text)

    def test_primary_contracts_and_other_specs_are_in_scope(self):
        for path in (
            "specs/compiler-threat-detection-spec.md",
            "specs/compiler-threat-detection-changelog.md",
            "docs/src/content/docs/specs/safe-outputs-specification.md",
            "specs/security-architecture-spec.md",
        ):
            self.assertIn(path, self.prompt)
        self.assertIn("discover other applicable specifications", self.text)
        self.assertIn("pre-change contract", self.text)
        self.assertIn("weakened by the same PR", self.text)

    def test_writer_adapter_cannot_overwrite_its_canonical_guidance(self):
        self.assertIn("## agent: `pr-w3c-specification-writer`", self.prompt)
        self.assertNotIn("## agent: `w3c-specification-writer`", self.prompt)
        self.assertIn(".github/agents/w3c-specification-writer.agent.md", self.prompt)
        self.assertIn("trusted workflow configuration restored", self.text)
        base_restore = self.lock.index("Restore agent config folders from base branch")
        inline_restore = self.lock.index("Restore inline sub-agents from activation artifact")
        self.assertLess(base_restore, inline_restore)

    def test_static_review_has_no_external_egress_or_code_execution(self):
        self.assertIn("network:\n  allowed: []", self.frontmatter)
        self.assertIn("min-integrity: approved", self.frontmatter)
        for prohibition in (
            "never fetch, pull, push, install packages",
            "execute PR code",
            "never claim they ran or passed here",
        ):
            self.assertIn(prohibition, self.text)
        self.assertIn("A missing or truncated patch is not proof of conformance", self.text)
        self.assertIn("report_incomplete", self.prompt)

    def test_spec_patch_is_scoped_to_triggering_pr_and_documents(self):
        push = self.config["push_to_pull_request_branch"]
        self.assertEqual(push["target"], "triggering")
        self.assertEqual(
            push["allowed_files"],
            [
                "specs/*.md",
                "docs/src/content/docs/specs/*.md",
                "docs/src/content/docs/experimental/experiments-specification.md",
            ],
        )
        self.assertFalse(push["fallback_as_pull_request"])
        self.assertEqual(push["protected_files_policy"], "blocked")
        self.assertEqual(push["if_no_changes"], "error")
        self.assertEqual(push["max_patch_size"], 512)
        self.assertNotIn("GH_AW_CI_TRIGGER_TOKEN", self.lock)
        self.assertIn("Limit edits to five existing", self.text)

    def test_regressions_are_not_normalized_and_versions_stay_consistent(self):
        for guard in (
            "Do not rewrite the contract to bless the bug",
            "A PR description alone is insufficient",
            "unapproved breaking changes or security-policy changes are not",
            "minimum-binary compatibility table and separate change log",
            "Never invent a released binary version or a new threat ID",
        ):
            self.assertIn(guard, self.text)

    def test_fork_and_stale_head_updates_are_not_published(self):
        self.assertIn("git rev-parse HEAD", self.prompt)
        self.assertIn("Before any publication, re-read the PR state and head", self.text)
        self.assertIn("head differs from the captured SHA", self.text)
        self.assertIn("For fork PRs, do not attempt branch writes", self.text)
        self.assertIn("Never force-push or retry against a different head", self.text)
        self.assertIn("acknowledges intent, not a successful push", self.text)

    def test_reviews_are_bounded_and_clean_noops_do_not_create_issues(self):
        review = self.config["submit_pull_request_review"]
        self.assertEqual(review["max"], 1)
        self.assertEqual(review["allowed_events"], ["COMMENT", "REQUEST_CHANGES"])
        self.assertTrue(review["supersede_older_reviews"])
        comments = self.config["create_pull_request_review_comment"]
        self.assertEqual(comments["max"], 6)
        self.assertEqual(comments["target"], "triggering")
        self.assertEqual(comments["side"], "RIGHT")
        self.assertEqual(self.config["noop"]["report-as-issue"], "false")
        self.assertIn("Deduplicate findings", self.prompt)
        self.assertIn("does not by itself configure merge enforcement", (
            ROOT / "docs/src/content/docs/patterns/spec-ops.md"
        ).read_text())


if __name__ == "__main__":
    unittest.main()
