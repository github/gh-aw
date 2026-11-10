"""Regression checks for the Super Linter GO_MODULES directory selection."""

import json
import os
import re
import shlex
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path

from summarize_super_linter import summarize_log


ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / ".github/workflows/super-linter.md"
LOCK = ROOT / ".github/workflows/super-linter.lock.yml"
RUNNER = ROOT / "scripts/run_super_linter.sh"


class SuperLinterScopeTests(unittest.TestCase):
    def setUp(self):
        self.source = SOURCE.read_text()
        match = re.search(r'FILTER_REGEX_EXCLUDE: "([^"\n]+)"', self.source)
        self.assertIsNotNone(match)
        self.pattern = match.group(1)

    def excluded(self, path):
        # Pinned super-linter filters full file paths before queuing their
        # containing directories in file-array-GO_MODULES.
        return re.search(self.pattern, path) is not None

    def test_deliberate_fixtures_and_empty_manifest_are_excluded(self):
        paths = [
            ".github/workflows/go.mod",
            "pkg/linters/appendbytestring/testdata/src/appendbytestring/appendbytestring.go",
            "pkg/linters/bufioscannererunchecked/testdata/src/basic/bad.go",
            "pkg/linters/internal/resourcetracker/testdata/src/resourcetracker/bad.go",
            "pkg/linters/internal/analyzer/deep/testdata/src/basic/bad.go",
            "super-linter-output/report.go",
        ]
        for path in paths:
            for prefix in ("", "/github/workspace/"):
                with self.subTest(path=prefix + path):
                    self.assertTrue(self.excluded(prefix + path))
        fixtures = list((ROOT / "pkg/linters").glob("**/testdata/**/*.go"))
        self.assertGreater(len(fixtures), 0)
        for fixture in fixtures:
            self.assertTrue(self.excluded(str(fixture.relative_to(ROOT))), fixture)
        self.assertEqual(list((ROOT / ".github/workflows").rglob("*.go")), [])

    def test_real_code_tests_and_module_remain_included(self):
        paths = [
            "go.mod",
            "cmd/gh-aw/main.go",
            "pkg/logger/logger.go",
            "pkg/workflow/checkout_config_parser.go",
            "pkg/linters/appendbytestring/appendbytestring.go",
            "pkg/linters/appendbytestring/appendbytestring_test.go",
            ".github/scripts/purelock/purity_scan.go",
            "scripts/lint_error_messages_test.go",
            "pkg/linters/example/testdata_helpers.go",
            "pkg/linters/internal/resourcetracker/resourcetracker.go",
            "pkg/linters/internal/resourcetracker/resourcetracker_test.go",
            ".github/workflows/go.mod.backup",
        ]
        for path in paths:
            for prefix in ("", "/github/workspace/"):
                with self.subTest(path=prefix + path):
                    self.assertFalse(self.excluded(prefix + path))

    def test_generated_lock_preserves_scope_and_reports_failures(self):
        lock = LOCK.read_text()
        self.assertIn("FILTER_REGEX_EXCLUDE: " + self.pattern, lock)
        for text in (self.source, lock):
            self.assertIn('VALIDATE_GO_MODULES: "true"', text)
            self.assertIn("continue-on-error: true", text)
            self.assertIn("${{ steps.super-linter.outcome }}", text)
            self.assertIn("Super-linter did not produce a nonempty log", text)
            self.assertIn("/tmp/super-linter-result.txt", text)
            self.assertIn("if-no-files-found: error", text)
        self.assertNotIn("GO_CONFIG_FILE:", self.source)
        self.assertNotIn("LINTER_RULES_PATH:", self.source)
        self.assertIn("twenty examples total", self.source)
        self.assertIn("does not establish an exploitable vulnerability", self.source)
        self.assertNotIn("[Include the full linter output here]", self.source)
        runner = RUNNER.read_text()
        self.assertIn("golangci-lint@v2.15.0", runner)
        self.assertIn("GOTOOLCHAIN=\"$(go env GOVERSION)\"", runner)
        self.assertIn("exec /action/lib/linter.sh", runner)
        self.assertIn("super-linter:v9.0.0@sha256:", self.source)
        self.assertIn("max-retries: 1", self.source)
        self.assertIn("COPILOT_MODEL: mai-code-1.1-flash", lock)
        for text in (self.source, lock):
            self.assertIn("/tmp/super-linter-summary.json", text)
        self.assertIn("analysis incomplete", self.source)


class SuperLinterStartupTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.workspace = self.root / "workspace with spaces"
        self.workspace.mkdir()
        self.artifacts = self.root / "artifacts"
        self.artifacts.mkdir()
        self.source = SOURCE.read_text()
        mock = self.root / "mock.bash"
        mock.write_text(textwrap.dedent("""\
            go() {
              if [ "$*" = "env GOVERSION" ]; then
                echo go1.27.0
                return "${MOCK_GO_ENV_EXIT:-0}"
              fi
              [ "$*" = "install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0" ] || exit 99
              [ "$GOTOOLCHAIN" = go1.27.0 ] && [ "$CGO_ENABLED" = 0 ] || exit 98
              echo "analyzer installation" >&2
              return "${MOCK_INSTALL_EXIT:-0}"
            }
            exec() {
              [ "$*" = /action/lib/linter.sh ] || exit 97
              [ "$PWD" = "$GITHUB_WORKSPACE" ] || exit 96
              [ "$CREATE_LOG_FILE" = true ] || exit 95
              [ "${PATH%%:*}" = "$GOBIN" ] || exit 94
              echo "original linter output" >&2
              if [ "${MOCK_WRITE_LOG:-true}" = true ]; then
                printf '%s' "$MOCK_LOG" > "$GITHUB_WORKSPACE/$LOG_FILE"
              fi
              exit "${MOCK_LINTER_EXIT:-0}"
            }
        """))
        self.env = {
            "PATH": os.defpath,
            "BASH_ENV": str(mock),
            "GITHUB_WORKSPACE": str(self.workspace),
            "CREATE_LOG_FILE": "true",
            "LOG_FILE": re.search(r"^\s+LOG_FILE: (\S+)", self.source, re.MULTILINE).group(1),
            "MOCK_LOG": "[INFO] All files linted successfully\n",
        }

    def run_linter(self, **env):
        return subprocess.run(
            ["bash", str(RUNNER)], cwd=self.root, env={**self.env, **env},
            text=True, capture_output=True, timeout=10,
        )

    def summarize(self, outcome):
        script = re.search(
            r"      - name: Check for linting issues\n.*?        run: \|\n(.*?)\n      - name:",
            self.source, re.DOTALL,
        ).group(1)
        script = textwrap.dedent(script)
        script = script.replace("/tmp/super-linter", str(self.artifacts / "super-linter"))
        script = script.replace(
            "scripts/summarize_super_linter.py",
            shlex.quote(str(ROOT / "scripts/summarize_super_linter.py")),
        )
        return subprocess.run(
            ["bash", "-e", "-c", script], cwd=self.workspace,
            env={"PATH": os.defpath, "LINTER_OUTCOME": outcome},
            text=True, capture_output=True, timeout=10,
        )

    def test_action_passes_a_script_without_shell_quote_parsing(self):
        for text in (self.source, LOCK.read_text()):
            self.assertIn("args: /github/workspace/scripts/run_super_linter.sh", text)
            self.assertIn("entrypoint: /bin/bash", text)
            self.assertIn("/tmp/super-linter-startup.log", text)
            self.assertIn("for log in super-linter.log super-linter-output/super-linter-startup.log", text)
        self.assertIn("-e GITHUB_WORKSPACE=/tmp/gh-aw/agent", self.source)
        self.assertIn("/tmp/gh-aw/agent/scripts/run_super_linter.sh", self.source)

    def test_nonempty_workspace_log_reaches_deterministic_summary(self):
        result = self.run_linter()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.workspace / self.env["LOG_FILE"]).read_text(), self.env["MOCK_LOG"])
        summary_result = self.summarize("success")
        self.assertEqual(summary_result.returncode, 0, summary_result.stdout + summary_result.stderr)
        summary = json.loads((self.artifacts / "super-linter-summary.json").read_text())
        self.assertTrue(summary["coverage_complete"])

    def test_linter_failure_preserves_exit_output_and_incomplete_coverage(self):
        result = self.run_linter(
            MOCK_LINTER_EXIT="42",
            MOCK_LOG="pkg/example/example.go:5:2: unsafe conversion (gosec)\n",
        )
        self.assertEqual(result.returncode, 42)
        startup = self.workspace / "super-linter-output/super-linter-startup.log"
        self.assertIn("original linter output", startup.read_text())
        self.assertIn("original linter output", result.stdout)
        summary_result = self.summarize("failure")
        self.assertEqual(summary_result.returncode, 0, summary_result.stdout + summary_result.stderr)
        summary = json.loads((self.artifacts / "super-linter-summary.json").read_text())
        self.assertFalse(summary["coverage_complete"])
        self.assertEqual(summary["counts"]["source"], 1)

    def test_setup_failure_is_retained_but_not_used_as_lint_coverage(self):
        result = self.run_linter(MOCK_INSTALL_EXIT="23")
        self.assertEqual(result.returncode, 23)
        startup = self.workspace / "super-linter-output/super-linter-startup.log"
        self.assertIn("analyzer installation", startup.read_text())
        self.assertNotIn("original linter output", startup.read_text())
        self.assertFalse((self.workspace / self.env["LOG_FILE"]).exists())
        summary = self.summarize("failure")
        self.assertNotEqual(summary.returncode, 0)
        self.assertIn("analysis is unavailable", summary.stdout)

    def test_toolchain_lookup_failure_stops_before_installation(self):
        result = self.run_linter(MOCK_GO_ENV_EXIT="24")
        self.assertEqual(result.returncode, 24)
        self.assertNotIn("analyzer installation", result.stdout)

    def test_missing_and_empty_logs_remain_unavailable(self):
        for env in ({"MOCK_WRITE_LOG": "false"}, {"MOCK_LOG": ""}):
            with self.subTest(env=env):
                self.assertEqual(self.run_linter(**env).returncode, 0)
                self.assertNotEqual(self.summarize("success").returncode, 0)
                self.assertFalse((self.artifacts / "super-linter-summary.json").exists())


class SuperLinterSummaryTests(unittest.TestCase):
    def test_counts_all_diagnostics_and_nested_fixtures_once(self):
        typecheck = (
            "pkg/console/render.go:7:2: could not import reflect "
            "(export data version 5 is greater than maximum supported version 4) (typecheck)"
        )
        fixture = (
            "pkg/linters/internal/resourcetracker/testdata/src/basic/bad.go:11:2: "
            "Error return value is not checked (errcheck)"
        )
        source = "pkg/example/example.go:5:2: integer overflow conversion (gosec)"
        summary = summarize_log("\n".join([typecheck, fixture, source, typecheck]), "failure")
        self.assertEqual(summary["diagnostic_count"], 3)
        self.assertEqual(summary["counts"], {"typecheck": 1, "fixtures": 1, "source": 1})
        self.assertFalse(summary["coverage_complete"])
        self.assertEqual(summary["title_suffix"], "analysis incomplete")

    def test_uses_file_line_linter_message_identity_and_strips_ansi(self):
        diagnostic = "pkg/example/example.go:5:2: unsafe conversion (gosec)"
        summary = summarize_log(
            "\x1b[31m" + diagnostic + "\x1b[0m\n" + diagnostic.replace(":5:2:", ":5:9:"),
            "failure",
        )
        self.assertEqual(summary["diagnostic_count"], 1)
        self.assertEqual(summary["counts"]["source"], 1)
        self.assertFalse(summary["coverage_complete"])

    def test_source_findings_do_not_hide_unrecognized_analyzer_failure(self):
        summary = summarize_log(
            "pkg/example/example.go:5:2: unsafe conversion (gosec)\n"
            "[ERROR] can't run linter goanalysis_metalinter: unexpected analyzer failure",
            "failure",
        )
        self.assertEqual(summary["counts"]["source"], 1)
        self.assertFalse(summary["coverage_complete"])
        self.assertEqual(summary["title_suffix"], "analysis incomplete")

    def test_unknown_custom_linter_warning_prevents_clean_verdict(self):
        summary = summarize_log(
            'level=warning msg="Found unknown linters in //nolint directives: resourcetrackertest"',
            "success",
        )
        self.assertEqual(summary["tooling_message_count"], 1)
        self.assertFalse(summary["coverage_complete"])

    def test_failed_run_without_diagnostics_is_not_clean(self):
        summary = summarize_log("[ERROR] Super-linter failed before startup", "failure")
        self.assertFalse(summary["coverage_complete"])
        self.assertEqual(summary["title_suffix"], "analysis incomplete")

    def test_clean_run_requires_success_and_complete_coverage(self):
        summary = summarize_log("[INFO] All files linted successfully", "success")
        self.assertTrue(summary["coverage_complete"])
        self.assertEqual(summary["title_suffix"], "0 source findings")

    def test_examples_are_bounded_without_losing_counts(self):
        diagnostics = [
            f"pkg/example/example.go:{line}:2: diagnostic (linter{linter})"
            for linter in range(10)
            for line in range(1, 6)
        ]
        summary = summarize_log("\n".join(diagnostics), "failure")
        self.assertEqual(summary["diagnostic_count"], 50)
        self.assertEqual(sum(len(group["examples"]) for group in summary["groups"]), 20)
        self.assertTrue(all(len(group["examples"]) <= 3 for group in summary["groups"]))

    def test_rejects_empty_log_and_unknown_outcome(self):
        with self.assertRaisesRegex(ValueError, "must not be empty"):
            summarize_log(" \n", "success")
        with self.assertRaisesRegex(ValueError, "outcome"):
            summarize_log("[INFO] starting", "cancelled")


if __name__ == "__main__":
    unittest.main()
