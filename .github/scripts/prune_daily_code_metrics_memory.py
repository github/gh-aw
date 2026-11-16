#!/usr/bin/env python3
"""Keep only valid history entries from the last 90 days."""

import json
import os
import sys
import tempfile
from datetime import date, timedelta
from pathlib import Path


DEFAULT_HISTORY_PATH = Path("/tmp/gh-aw/repo-memory/default/history.jsonl")


def prune_history(history_path: Path, today: date | None = None) -> int:
    if not history_path.is_file():
        return 0

    cutoff = (today or date.today()) - timedelta(days=90)
    retained: list[str] = []
    with history_path.open(encoding="utf-8") as history:
        for line in history:
            try:
                row = json.loads(line)
                row_date = date.fromisoformat(row["date"])
            except (KeyError, TypeError, ValueError, json.JSONDecodeError):
                continue
            if row_date >= cutoff:
                retained.append(line)

    temporary_path: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            dir=history_path.parent,
            delete=False,
        ) as temporary:
            temporary.writelines(retained)
            temporary_path = Path(temporary.name)
        os.replace(temporary_path, history_path)
    finally:
        if temporary_path is not None:
            temporary_path.unlink(missing_ok=True)

    return len(retained)


def main(argv: list[str]) -> int:
    if len(argv) > 2:
        print(f"Usage: {Path(argv[0]).name} [history.jsonl]", file=sys.stderr)
        return 2

    history_path = Path(argv[1]) if len(argv) == 2 else DEFAULT_HISTORY_PATH
    retained = prune_history(history_path)
    print(f"Retained {retained} history entries in {history_path}.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
