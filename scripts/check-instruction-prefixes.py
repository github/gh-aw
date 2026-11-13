#!/usr/bin/env python3
"""Check byte budgets for truncation-safe instruction entry points."""

import argparse
import json
from pathlib import Path
import re
import sys


QUERY_BYTES = 8000
CONTRACT_BYTES = 7500
MARKER = b"<!-- instruction-prefix-end -->"
ROOT = Path(__file__).resolve().parents[1]
ENTRY_POINTS = (
    "AGENTS.md",
    "SKILL.md",
    ".github/skills/agentic-workflows/SKILL.md",
    "pkg/cli/data/agentic_workflows_skill.md",
    ".github/skills/prompt-token-efficiency/SKILL.md",
    ".github/agents/developer.instructions.md",
    ".github/aw/instructions.md",
    ".github/aw/github-agentic-workflows.md",
    ".github/aw/create-agentic-workflow.md",
    ".github/aw/create-agentic-workflow-design.md",
    ".github/aw/create-agentic-workflow-evaluation.md",
    ".github/aw/create-agentic-workflow-validation.md",
    ".github/aw/update-agentic-workflow.md",
    ".github/aw/debug-agentic-workflow.md",
    ".github/aw/create-shared-agentic-workflow.md",
)


def inspect(path):
    data = path.read_bytes()
    data.decode("utf-8")
    markers = list(re.finditer(rb"(?m)^" + re.escape(MARKER) + rb"\r?$", data))
    errors = []
    if len(markers) > 1:
        errors.append("use exactly one instruction-prefix-end marker")
    end = markers[0].end() if markers else len(data)
    if end > CONTRACT_BYTES:
        errors.append(
            f"contract ends at byte {end}; limit is {CONTRACT_BYTES}. "
            "Compress it or move optional detail after an early instruction-prefix-end marker"
        )
    contract = data[:end].decode("utf-8")
    lines = contract.splitlines()
    if lines and lines[0] == "---" and "---" not in lines[1:]:
        errors.append("close YAML frontmatter before the contract ends")
    fence = None
    for line in lines:
        match = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", line)
        if not match:
            continue
        token, suffix = match.groups()
        if fence is None:
            fence = token
        elif token[0] == fence[0] and len(token) >= len(fence) and not suffix.strip():
            fence = None
    if fence is not None:
        errors.append("close code fences before the contract ends; markers inside examples do not qualify")
    return {
        "path": str(path),
        "total_bytes": len(data),
        "visible_bytes": min(len(data), QUERY_BYTES),
        "omitted_bytes": max(0, len(data) - QUERY_BYTES),
        "contract_bytes": end,
        "errors": errors,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("paths", nargs="*", type=Path, help="files to check; default: maintained entry points")
    parser.add_argument("--json", action="store_true", help="emit byte counts and errors as JSON")
    args = parser.parse_args()
    results = []
    for path in args.paths or [ROOT / name for name in ENTRY_POINTS]:
        try:
            result = inspect(path)
        except (OSError, UnicodeError) as error:
            result = {"path": str(path), "errors": [str(error)]}
        results.append(result)
    if args.json:
        print(json.dumps(results, indent=2))
    else:
        for result in results:
            if result["errors"]:
                for error in result["errors"]:
                    print(f"ERROR: {result['path']}: {error}", file=sys.stderr)
            else:
                print(
                    f"{result['path']}: {result['total_bytes']} bytes; "
                    f"contract {result['contract_bytes']}/{CONTRACT_BYTES}; "
                    f"omitted {result['omitted_bytes']}"
                )
    return int(any(result["errors"] for result in results))


if __name__ == "__main__":
    sys.exit(main())
