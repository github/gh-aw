"""Failure-detection contracts for the formal-methods example harness."""

import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from check import ROOT, check_contract, run


class VerificationTests(unittest.TestCase):
    def test_pinned_setup_and_workflow_paths_agree(self):
        repository = ROOT.parents[1]
        shared = (repository / ".github/workflows/shared/fstar.md").read_text()
        ci = (repository / ".github/workflows/formal-methods.yml").read_text()
        installer = (repository / "scripts/install-fstar.sh").read_text()
        for path in [
            "/tmp/gh-aw/agent/fstar-tools/fstar/bin/fstar.exe",
            "/tmp/gh-aw/agent/fstar-tools/fstar/lib/fstar/z3-5.3.0/bin/z3",
        ]:
            self.assertIn(path, shared)
            self.assertIn(path, ci)
        for source in [shared, ci]:
            self.assertIn('bash scripts/install-fstar.sh "$FSTAR_SHA256"', source)
        for source in [shared, ci]:
            self.assertIn(
                "a716153960545bc26b9d093f55614fedc716a202e4daa1665e5c63e4a2fb04e4",
                source,
            )
        for pin in [
            "fstar-v2026.10.25-Linux-x86_64.tar.gz",
            'sha256sum --check --strict', "--smt", "--z3version 5.3.0",
        ]:
            self.assertIn(pin, installer)
        self.assertIn('expected_sha256="${1:-}"', installer)
        tla = (repository / ".github/workflows/shared/tla-plus.md").read_text()
        for line in tla.splitlines():
            if "https://github.com/" in line or len(line.strip().rstrip(" \\")) == 64:
                self.assertIn(line.strip(), ci)
        lock = (repository / ".github/workflows/formal-proof-repair.lock.yml").read_text()
        for permission in ["issues: write", "contents: write", "pull-requests: write"]:
            self.assertNotIn(permission, lock)

    def test_contract_accepts_body_repair_only(self):
        source = (ROOT / "CounterBroken.fst").read_text()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "CounterBroken.fst"
            for body in [source, source.replace("n + 2\n", "n + 1\n")]:
                path.write_text(body)
                check_contract(path)
            for mutation in [
                source.replace("r == n + 1", "True"),
                source.replace("n < limit", "n + 2 < limit"),
                source + "\nassume val bypass: unit\n",
                source.replace("n + 2\n", "admit ()\n"),
                source.replace("n + 2\n", '#set-options "--lax"\nn + 1\n'),
            ]:
                path.write_text(mutation)
                with self.assertRaises(ValueError):
                    check_contract(path)

    def test_negative_control_requires_exact_failure_and_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            results = Path(directory)
            for status, output in [
                (0, "could not prove"), (1, "parse error"), (127, "missing tool"),
                (1, "could not prove: timeout"), (1, "could not prove: unknown"),
            ]:
                with patch("check.subprocess.run", return_value=subprocess.CompletedProcess([], status, output)):
                    with self.assertRaises(RuntimeError):
                        run(["verifier"], results, "negative", 1, "could not prove")
            with patch("check.subprocess.run", return_value=subprocess.CompletedProcess([], 1, "could not prove")):
                run(["verifier"], results, "negative", 1, "could not prove")

    def test_timeout_and_missing_tool_are_not_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            for error in [
                FileNotFoundError("missing"),
                subprocess.TimeoutExpired("verifier", 120, output=b"partial proof output"),
            ]:
                with patch("check.subprocess.run", side_effect=error):
                    with self.assertRaises(RuntimeError):
                        run(["verifier"], Path(directory), "failure")
                self.assertTrue((Path(directory) / "failure.log").exists())
            self.assertIn("partial proof output", (Path(directory) / "failure.log").read_text())


if __name__ == "__main__":
    unittest.main()
