"""Exercise the Node image publisher's actual decision script."""

import os
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


WORKFLOW = (
    Path(__file__).resolve().parent.parent
    / ".github/workflows/publish-safe-outputs-node.yml"
)


class PublishSafeOutputsNodeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = WORKFLOW.read_text()
        cls.decision = cls.source.split(
            "      - name: Decide whether to publish\n", 1
        )[1].split("\n      - name:", 1)[0]
        cls.script = textwrap.dedent(cls.decision.split("        run: |\n", 1)[1])

    def decide(self, event="push", **overrides):
        env = {
            **os.environ,
            "COOLDOWN_DAYS": "1",
            "CURRENT_BASE_DIGEST": "sha256:base",
            "CURRENT_DOCKERFILE_HASH": "source",
            "EXISTING_BASE_DIGEST": "sha256:base",
            "EXISTING_DOCKERFILE_HASH": "source",
            "IMAGE_EXISTS": "true",
            "IN_COOLDOWN": "false",
            "TRIGGER_EVENT": event,
            **overrides,
        }
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "output"
            env["GITHUB_OUTPUT"] = str(output)
            result = subprocess.run(
                ["bash", "-c", self.script],
                env=env,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            return dict(line.split("=", 1) for line in output.read_text().splitlines())

    def test_event_is_wired_from_github_context(self):
        self.assertIn("TRIGGER_EVENT: ${{ github.event_name }}", self.decision)

    def test_unchanged_push_skips_publication(self):
        result = self.decide()
        self.assertEqual(result["should_push"], "false")
        self.assertEqual(result["force_refresh"], "false")

    def test_scheduled_and_manual_runs_refresh_unchanged_packages(self):
        for event in ("schedule", "workflow_dispatch"):
            with self.subTest(event=event):
                result = self.decide(event)
                self.assertEqual(result["should_push"], "true")
                self.assertEqual(result["force_refresh"], "true")
                self.assertIn("package", result["reason"])

    def test_changed_base_or_source_and_missing_image_still_publish(self):
        for event in ("push", "schedule", "workflow_dispatch"):
            for overrides in (
                {"EXISTING_BASE_DIGEST": "sha256:old"},
                {"EXISTING_DOCKERFILE_HASH": "old"},
                {"IMAGE_EXISTS": "false", "EXISTING_BASE_DIGEST": ""},
            ):
                with self.subTest(event=event, overrides=overrides):
                    result = self.decide(event, **overrides)
                    self.assertEqual(result["should_push"], "true")
                    self.assertEqual(
                        result["force_refresh"],
                        "false" if event == "push" else "true",
                    )

    def test_cooldown_blocks_new_base_on_all_events(self):
        for event in ("push", "schedule", "workflow_dispatch"):
            for exists in ("true", "false"):
                with self.subTest(event=event, exists=exists):
                    result = self.decide(
                        event,
                        IN_COOLDOWN="true",
                        IMAGE_EXISTS=exists,
                        EXISTING_BASE_DIGEST="sha256:old",
                    )
                    self.assertEqual(result["should_push"], "false")
                    self.assertEqual(
                        result["force_refresh"],
                        "false" if event == "push" else "true",
                    )
                    self.assertIn("cooldown", result["reason"])

    def test_cooldown_allows_refresh_of_already_published_base(self):
        for event in ("schedule", "workflow_dispatch"):
            with self.subTest(event=event):
                self.assertEqual(
                    self.decide(event, IN_COOLDOWN="true")["should_push"], "true"
                )

    def test_build_refreshes_layers_and_preserves_both_platforms(self):
        build = self.source.split("      - name: Build and push gh-aw-node\n", 1)[
            1
        ].split("\n      - name:", 1)[0]
        self.assertIn(
            "no-cache: ${{ steps.decision.outputs.force_refresh == 'true' }}",
            build,
        )
        self.assertRegex(build, r"(?m)^\s+pull: true$")
        self.assertIn("platforms: linux/amd64,linux/arm64", build)
        self.assertIn("if: steps.decision.outputs.should_push == 'true'", build)

    def test_publish_tags_are_unique_across_runs_and_retries(self):
        build = self.source.split("      - name: Build and push gh-aw-node\n", 1)[
            1
        ].split("\n      - name:", 1)[0]
        tags = textwrap.dedent(
            build.split("          tags: |\n", 1)[1].split(
                "          labels: |\n", 1
            )[0]
        ).splitlines()
        self.assertEqual(
            tags,
            [
                "${{ env.IMAGE_NAME }}:latest",
                (
                    "${{ env.IMAGE_NAME }}:node-lts-alpine-"
                    + "${{ steps.upstream.outputs.short_digest }}-"
                    + "${{ github.run_id }}-${{ github.run_attempt }}"
                ),
            ],
        )

    def test_suite_is_registered_in_test_scripts(self):
        makefile = (WORKFLOW.parents[2] / "Makefile").read_text()
        recipe = makefile.split("test-scripts: build\n", 1)[1].split(
            "\n# Test all", 1
        )[0]
        self.assertIn("python3 scripts/test_publish_safe_outputs_node.py", recipe)

    def test_npm_patches_require_reported_fixed_versions(self):
        dockerfile = (
            WORKFLOW.parents[2] / "actions/setup/js/Dockerfile.safe-outputs-mcp"
        ).read_text()
        for package, version in (
            ("brace-expansion", "5.0.11"),
            ("undici", "6.29.1"),
            ("tar", "7.5.22"),
            ("ip-address", "10.3.1"),
        ):
            with self.subTest(package=package):
                self.assertIn(f'"{package}@^{version}"', dockerfile)


if __name__ == "__main__":
    unittest.main()
