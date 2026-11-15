import json
import tempfile
import unittest
from datetime import date, timedelta
from pathlib import Path

from prune_daily_code_metrics_memory import prune_history


class PruneHistoryTests(unittest.TestCase):
    def test_retains_recent_and_cutoff_rows_and_drops_invalid_rows(self):
        today = date(2026, 11, 15)
        cutoff = today - timedelta(days=90)
        rows = [
            {"date": today.isoformat(), "value": "recent"},
            {"date": cutoff.isoformat(), "value": "boundary"},
            {"date": (cutoff - timedelta(days=1)).isoformat(), "value": "expired"},
            {"value": "missing date"},
            {"date": "not-a-date"},
        ]
        lines = [*(json.dumps(row) + "\n" for row in rows), "invalid json\n"]

        with tempfile.TemporaryDirectory() as directory:
            history_path = Path(directory) / "history.jsonl"
            history_path.write_text("".join(lines), encoding="utf-8")

            retained = prune_history(history_path, today)

            self.assertEqual(retained, 2)
            self.assertEqual(
                history_path.read_text(encoding="utf-8"),
                "".join(lines[:2]),
            )

    def test_missing_history_file_is_unchanged(self):
        with tempfile.TemporaryDirectory() as directory:
            history_path = Path(directory) / "missing.jsonl"

            self.assertEqual(prune_history(history_path, date(2026, 11, 15)), 0)
            self.assertFalse(history_path.exists())


if __name__ == "__main__":
    unittest.main()
