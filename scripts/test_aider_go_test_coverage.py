"""Isolated regressions for the Aider coverage/publication gate."""

import importlib.util
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location(
    "coverage_gate", Path(__file__).with_name("aider_go_test_coverage.py")
)
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)

SOURCE = """package actualname

func Classify(n int) string {
    if n < 0 {
        return "negative"
    }
    return "nonnegative"
}
"""
TEST = """package actualname

import "testing"

func TestClassify(t *testing.T) {
    for _, tc := range []struct{ n int; want string }{{-1, "negative"}, {0, "nonnegative"}} {
        if got := Classify(tc.n); got != tc.want {
            t.Errorf("Classify(%d) = %q, want %q", tc.n, got, tc.want)
        }
    }
}
"""


class CoverageGateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="aider coverage ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.old_cwd = Path.cwd()
        os.chdir(self.root)
        self.addCleanup(os.chdir, self.old_cwd)
        self.env = patch.dict(os.environ, {
            "GOWORK": "off", "GOTOOLCHAIN": "local", "GOPROXY": "off",
            "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.test",
            "GIT_COMMITTER_NAME": "Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.test",
        })
        self.env.start()
        self.addCleanup(self.env.stop)
        self.state = self.root / "private-state"
        # State and generated agent files must not become repository changes.
        self.write(".gitignore", "private-state/\n")
        self.write("go.mod", "module example.test/coverage\n\ngo 1.20\n")
        self.write("pkg/hyphenated/source.go", SOURCE)
        self.target = self.root / "pkg/hyphenated/aider_coverage_test.go"
        gate.run("git", "init", "-q")
        gate.run("git", "add", ".")
        gate.run("git", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "Fixture")
        self.pending = patch.object(gate, "pending_pr", return_value=False)
        self.pending.start()
        self.addCleanup(self.pending.stop)

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def prepare(self, text=TEST):
        gate.discover(self.state)
        self.target.write_text(text)

    def test_real_gain_and_only_test_file_published(self):
        self.prepare()
        self.write(".aider.chat.history.md", "must not be staged")
        self.write(".aider/skills/reporting/SKILL.md", "must not be staged")
        target, package, before, after, total = gate.verify(self.state)
        self.assertEqual(str(target), "pkg/hyphenated/aider_coverage_test.go")
        self.assertEqual(package, "example.test/coverage/pkg/hyphenated")
        self.assertEqual(before, 0)
        self.assertEqual(after, total)
        self.assertGreater(after, 0)
        real_run = gate.run
        publications = []

        def capture(*args):
            if args[0] == "safeoutputs":
                publications.append(args)
                return "PR staged\n"
            return real_run(*args)

        with patch.object(gate, "run", side_effect=capture):
            gate.publish(self.state)
        self.assertEqual(len(publications), 1)
        self.assertIn("0/"+str(total)+" -> "+str(total)+"/"+str(total), publications[0][5])
        self.assertEqual(real_run("git", "show", "--format=", "--name-only", "HEAD").strip(), str(target))
        self.assertIn(".aider.chat.history.md", real_run("git", "ls-files", "--others"))

    def test_skipped_empty_and_non_asserting_tests_rejected(self):
        gate.discover(self.state)
        for text in (
            TEST.replace('t.Errorf(', 't.Skipf('),
            'package actualname\nimport "testing"\nfunc TestPlaceholder_TODO(t *testing.T) { t.Skip("TODO") }\n',
            'package actualname\nimport "testing"\nfunc TestEmpty(t *testing.T) { Classify(0) }\n',
            '//go:build ignore\n'+TEST,
        ):
            with self.subTest(text=text):
                self.target.write_text(text)
                with self.assertRaises(ValueError):
                    gate.verify(self.state)

    def test_assertions_without_new_coverage_rejected(self):
        self.prepare('package actualname\nimport "testing"\nfunc TestEmpty(t *testing.T) { if 1 != 1 { t.Fatal("no") } }\n')
        with self.assertRaisesRegex(ValueError, "No additional"):
            gate.verify(self.state)

    def test_new_coverage_with_disconnected_assertions_rejected(self):
        self.prepare("""package actualname
import "testing"
func TestVacuous(t *testing.T) {
    Classify(-1)
    Classify(0)
    if false { t.Fatal("disconnected assertion") }
}
""")
        with self.assertRaisesRegex(ValueError, "No mutation witness"):
            gate.verify(self.state)
        self.assertEqual((self.root / "pkg/hyphenated/source.go").read_text(), SOURCE)

    def test_existing_tests_cannot_supply_mutation_witness(self):
        self.write("pkg/hyphenated/source_test.go", """package actualname
import "testing"
func TestExisting(t *testing.T) {
    if Classify(0) != "nonnegative" { t.Fatal("wrong") }
}
""")
        gate.run("git", "add", ".")
        gate.run("git", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "Existing test")
        self.prepare("""package actualname
import "testing"
func TestVacuous(t *testing.T) {
    Classify(-1)
    if false { t.Fatal("disconnected assertion") }
}
""")
        with self.assertRaisesRegex(ValueError, "No mutation witness"):
            gate.verify(self.state)

    def test_failing_test_and_production_edit_rejected(self):
        self.prepare(TEST.replace('"negative"},', '"wrong"},'))
        with self.assertRaises(subprocess.CalledProcessError):
            gate.verify(self.state)
        self.target.write_text(TEST)
        self.write("pkg/hyphenated/source.go", SOURCE+"\n// unrelated\n")
        with self.assertRaisesRegex(ValueError, "tracked files"):
            gate.verify(self.state)

    def test_unrelated_file_and_pre_staged_changes_rejected(self):
        self.prepare()
        self.write("unrelated.txt", "noise")
        with self.assertRaisesRegex(ValueError, "Unrelated"):
            gate.verify(self.state)
        gate.run("git", "add", "unrelated.txt")
        with self.assertRaisesRegex(ValueError, "tracked files"):
            gate.verify(self.state)

    def test_discovery_errors_and_existing_target_are_not_success(self):
        self.write("pkg/hyphenated/source.go", "invalid Go\n")
        gate.run("git", "add", ".")
        gate.run("git", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "Broken fixture")
        with self.assertRaises(subprocess.CalledProcessError):
            gate.discover(self.state)
        self.assertFalse((self.state / "target.json").exists())

    def test_open_pr_suppresses_discovery_and_publication(self):
        with patch.object(gate, "pending_pr", return_value=True):
            gate.discover(self.state)
            self.assertIn('"noop": true', (self.state / "target.json").read_text())
            with self.assertRaisesRegex(ValueError, "awaiting review"):
                gate.publish(self.state)

    def test_existing_external_tests_preserved_and_real_gain_required(self):
        self.write("pkg/hyphenated/source_test.go", """package actualname_test
import (
    "testing"
    subject "example.test/coverage/pkg/hyphenated"
)
func TestPositive(t *testing.T) {
    if subject.Classify(1) != "nonnegative" { t.Fatal("wrong") }
}
""")
        gate.run("git", "add", ".")
        gate.run("git", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "Existing external test")
        self.prepare()
        _, _, before, after, total = gate.verify(self.state)
        self.assertGreater(before, 0)
        self.assertGreater(after, before)
        self.assertEqual(after, total)
        self.assertEqual(gate.run("git", "diff", "--name-only", "HEAD"), "")

    def test_at_most_three_packages_measured(self):
        for name in ("b", "c", "d", "e"):
            self.write("pkg/"+name+"/source.go", "package sample\nfunc Value() int { return 1 }\n")
        gate.run("git", "add", ".")
        gate.run("git", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "More packages")
        real_measure = gate.measure
        measured = []

        def all_covered(package, profile):
            measured.append(package)
            blocks = real_measure(package, profile)
            return {key: (count, True) for key, (count, _) in blocks.items()}

        with patch.object(gate, "measure", side_effect=all_covered):
            gate.discover(self.state)
        self.assertEqual(len(measured), 3)
        self.assertIn('"noop": true', (self.state / "target.json").read_text())

    def test_pr_lookup_failure_is_not_noop(self):
        with patch.object(gate, "pending_pr", side_effect=subprocess.CalledProcessError(
            1, ["gh"], stderr="permission denied"
        )):
            with self.assertRaises(subprocess.CalledProcessError):
                gate.discover(self.state)
        self.assertFalse((self.state / "target.json").exists())

    def test_coverage_regression_rejected_even_with_net_gain(self):
        self.prepare()
        real_measure = gate.measure
        before = gate.coverage(self.state / "before.out")
        keys = list(before)
        self.assertGreaterEqual(len(keys), 2)
        before[keys[0]] = (before[keys[0]][0], True)
        profile = "mode: set\n"+"".join(
            f"{key} {count} {int(hit)}\n" for key, (count, hit) in before.items()
        )
        (self.state / "before.out").write_text(profile)

        def lost_branch(package, path):
            after = real_measure(package, path)
            after[keys[0]] = (after[keys[0]][0], False)
            return after

        with patch.object(gate, "measure", side_effect=lost_branch):
            with self.assertRaisesRegex(ValueError, "regressed"):
                gate.verify(self.state)

    def test_no_candidate_and_baseline_cannot_be_overwritten(self):
        self.target.write_text(TEST)
        gate.run("git", "add", ".")
        gate.run("git", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "Existing target")
        gate.discover(self.state)
        self.assertIn('"noop": true', (self.state / "target.json").read_text())
        with self.assertRaisesRegex(ValueError, "already ran"):
            gate.discover(self.state)
        with self.assertRaisesRegex(ValueError, "forbidden"):
            gate.verify(self.state)


if __name__ == "__main__":
    unittest.main()
