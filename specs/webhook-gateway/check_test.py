"""Fail-closed classification of bounded TLC evidence."""

import unittest

from check import check_result


class ResultTests(unittest.TestCase):
    def test_requires_exhausted_success(self):
        output = ("Model checking completed. No error has been found.\n"
                  "30 states generated, 20 distinct states found, 0 states left on queue.")
        self.assertEqual(check_result(0, output)["distinct"], 20)
        for status, text in [(1, output), (0, output.replace("0 states left", "1 states left")),
                             (0, "Model checking completed. No error has been found.")]:
            with self.subTest(status=status, text=text), self.assertRaises(RuntimeError):
                check_result(status, text)

    def test_requires_exact_control_and_trace(self):
        output = ("Error: Invariant ImmutableReceipt is violated.\n"
                  "State 1: <Initial predicate>\nState 2: <ConflictingDuplicate line 1>\n"
                  "30 states generated, 20 distinct states found, 5 states left on queue.")
        result = check_result(12, output, "ImmutableReceipt")
        self.assertEqual(result["trace"], ["Initial predicate", "ConflictingDuplicate"])
        for status, text, expected in [
            (12, output, "SourceAuthenticated"),
            (1, output, "ImmutableReceipt"),
            (12, output.replace("State 2:", "Missing:"), "ImmutableReceipt"),
            (12, output + "\nInvariant TypeOK is violated.", "ImmutableReceipt"),
        ]:
            with self.subTest(status=status, expected=expected), self.assertRaises(RuntimeError):
                check_result(status, text, expected)


if __name__ == "__main__":
    unittest.main()
