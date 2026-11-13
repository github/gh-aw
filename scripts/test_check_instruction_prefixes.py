"""Regression tests for the actual UTF-8 byte budget and prefix boundary."""

import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("check-instruction-prefixes.py")
SPEC = importlib.util.spec_from_file_location("instruction_prefixes", SCRIPT)
CHECKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECKER)


class InstructionPrefixTests(unittest.TestCase):
    def check_bytes(self, data):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "instructions.md"
            path.write_bytes(data)
            return CHECKER.inspect(path)

    def test_contract_boundary_counts_utf8_bytes_not_characters(self):
        for size, accepted in ((7500, True), (7501, False)):
            with self.subTest(size=size):
                result = self.check_bytes("é".encode() * 3750 + b"x" * (size - 7500))
                self.assertEqual(result["total_bytes"], size)
                self.assertEqual(not result["errors"], accepted)

    def test_marker_must_end_within_budget_including_metadata(self):
        for end, accepted in ((7500, True), (7501, False)):
            with self.subTest(end=end):
                metadata = b"---\ndescription: test\n---\n"
                data = metadata + b"x" * (end - len(metadata) - len(CHECKER.MARKER) - 1)
                data += b"\n" + CHECKER.MARKER + b"\n" + b"optional detail\n" * 1000
                result = self.check_bytes(data)
                self.assertEqual(result["contract_bytes"], end)
                self.assertEqual(result["visible_bytes"], 8000)
                self.assertEqual(result["omitted_bytes"], len(data) - 8000)
                self.assertEqual(not result["errors"], accepted)

    def test_marker_inside_code_example_does_not_hide_required_tail(self):
        result = self.check_bytes(b"```markdown\n" + CHECKER.MARKER + b"\n```\n" + b"x" * 8000)
        self.assertTrue(any("code fences" in error for error in result["errors"]))

    def test_rejects_duplicate_markers_and_unclosed_frontmatter(self):
        for data in (
            CHECKER.MARKER + b"\n" + CHECKER.MARKER,
            b"---\ndescription: test\n" + CHECKER.MARKER,
        ):
            with self.subTest(data=data):
                self.assertTrue(self.check_bytes(data)["errors"])

    def test_missing_files_and_invalid_utf8_fail_explicitly(self):
        with tempfile.TemporaryDirectory() as directory:
            missing = Path(directory) / "missing.md"
            invalid = Path(directory) / "invalid.md"
            invalid.write_bytes(b"\xff")
            result = subprocess.run(
                [sys.executable, str(SCRIPT), str(missing), str(invalid)],
                capture_output=True, text=True, check=False,
            )
            self.assertEqual(result.returncode, 1)
            self.assertIn(str(missing), result.stderr)
            self.assertIn(str(invalid), result.stderr)


if __name__ == "__main__":
    unittest.main()
