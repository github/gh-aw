#!/usr/bin/env python3
"""Regression tests for benchmark acceptance and Make failure propagation."""

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from parse_benchmark_results import EXPECTED_BENCHMARKS, parse_results


ROOT = Path(__file__).resolve().parent.parent


def result_lines():
    return [
        f"Benchmark{name}-2\t100\t12.75 ns/op\t64 B/op\t1 allocs/op"
        for name in EXPECTED_BENCHMARKS
    ]


class BenchmarkResultsTests(unittest.TestCase):
    def test_complete_fractional_results(self):
        metrics = parse_results("goos: linux\n" + "\n".join(result_lines()) + "\nPASS\n")
        self.assertEqual(set(metrics["benchmarks"]), set(EXPECTED_BENCHMARKS))
        self.assertEqual(metrics["benchmarks"]["Validation"]["ns_per_op"], 12.75)
        self.assertEqual(metrics["benchmarks"]["Validation"]["iterations"], 100)

    def test_cpu_suffix_optional_and_scientific_notation(self):
        lines = [line.replace("-2", "").replace("12.75", "1.25e+03") for line in result_lines()]
        self.assertEqual(parse_results("\n".join(lines))["benchmarks"]["Validation"]["ns_per_op"], 1250)

    def test_rejects_empty_missing_duplicate_and_unexpected_results(self):
        lines = result_lines()
        for text, error in [
            ("PASS\n", "Missing"),
            ("\n".join(lines[:-1]), "Missing"),
            ("\n".join(lines + [lines[0]]), "Duplicate"),
            ("\n".join(lines + ["BenchmarkOther-2 1 1 ns/op 0 B/op 0 allocs/op"]), "Unexpected"),
            ("\n".join(lines + ["FAIL\tgithub.com/github/gh-aw/pkg/workflow"]), "Failed"),
        ]:
            with self.subTest(error=error, text=text):
                with self.assertRaisesRegex(ValueError, error):
                    parse_results(text)

    def test_rejects_malformed_and_nonfinite_metrics(self):
        for row in [
            "100 12.75 ns/op",
            "100 NaN ns/op 64 B/op 1 allocs/op",
            "100 inf ns/op 64 B/op 1 allocs/op",
            "100 0 ns/op 64 B/op 1 allocs/op",
            "0 1 ns/op 64 B/op 1 allocs/op",
            "100 1 ns/op -1 B/op 1 allocs/op",
            "100 1 ns/op 64 B/op -1 allocs/op",
            "100 invalid ns/op 64 B/op 1 allocs/op",
            "100 1 us/op 64 B/op 1 allocs/op",
        ]:
            with self.subTest(row=row), self.assertRaises(ValueError):
                parse_results(f"BenchmarkValidation-2 {row}")

    def test_cli_writes_only_complete_metrics(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "bench.txt"
            output = Path(directory) / "metrics.json"
            command = [sys.executable, str(ROOT / "scripts/parse_benchmark_results.py"), str(source), str(output)]
            source.write_text("PASS\n", encoding="utf-8")
            result = subprocess.run(command, capture_output=True, text=True, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Missing benchmark results", result.stderr)
            self.assertFalse(output.exists())
            source.write_text("\n".join(result_lines()), encoding="utf-8")
            subprocess.run(command, capture_output=True, text=True, check=True)
            self.assertEqual(len(json.loads(output.read_text())["benchmarks"]), 10)

    def test_make_propagates_each_go_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            fake_go = binary_dir / "go"
            fake_go.write_text(
                "#!/bin/bash\n"
                "printf '%s\\n' \"$*\" >> calls.txt\n"
                "echo 'fixture benchmark output'\n"
                "case \"$*\" in\n"
                "  *\"$FAIL_PACKAGE\"*) exit 7 ;;\n"
                "esac\n",
                encoding="utf-8",
            )
            fake_go.chmod(0o755)
            for package, expected_calls in [("./pkg/workflow", 1), ("./pkg/cli", 2), ("no-failure", 2)]:
                with self.subTest(package=package):
                    (root / "calls.txt").write_text("", encoding="utf-8")
                    result = subprocess.run(
                        ["make", "-f", str(ROOT / "Makefile"), "bench-performance", "VERSION=fixture",
                         f"PATH={binary_dir}:/usr/bin:/bin", f"FAIL_PACKAGE={package}"],
                        cwd=root,
                        capture_output=True,
                        text=True,
                        check=False,
                        env={"PATH": "/usr/bin:/bin"},
                    )
                    self.assertEqual(result.returncode == 0, package == "no-failure", result.stderr)
                    self.assertEqual(len((root / "calls.txt").read_text().splitlines()), expected_calls)
                    self.assertIn("fixture benchmark output", (root / "bench_performance.txt").read_text())


if __name__ == "__main__":
    unittest.main()
