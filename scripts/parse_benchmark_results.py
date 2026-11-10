#!/usr/bin/env python3
"""Validate the daily CLI benchmark set before publishing metrics."""

import argparse
from datetime import datetime, timezone
import json
import math
from pathlib import Path
import re
import sys


EXPECTED_BENCHMARKS = (
    "CompileSimpleWorkflow",
    "CompileComplexWorkflow",
    "CompileMCPWorkflow",
    "CompileMemoryUsage",
    "ParseWorkflow",
    "Validation",
    "YAMLGeneration",
    "ExtractWorkflowNameFromFile",
    "FastParseTitleFromReader",
    "FindIncludesInContent",
)


def parse_results(text):
    benchmarks = {}
    for line in text.splitlines():
        fields = line.split()
        if fields and fields[0] == "FAIL":
            raise ValueError(f"Failed benchmark command: {line}")
        if not fields or not fields[0].startswith("Benchmark"):
            continue
        match = re.fullmatch(r"Benchmark([A-Za-z_]+)(?:-[0-9]+)?", fields[0])
        if match is None or match[1] not in EXPECTED_BENCHMARKS:
            raise ValueError(f"Unexpected benchmark result: {line}")
        name = match[1]
        if name in benchmarks:
            raise ValueError(f"Duplicate benchmark result: {name}")
        if len(fields) != 8 or fields[3::2] != ["ns/op", "B/op", "allocs/op"]:
            raise ValueError(f"Incomplete benchmark metrics: {name}")
        try:
            iterations = int(fields[1])
            ns_per_op = float(fields[2])
            bytes_per_op = int(fields[4])
            allocs_per_op = int(fields[6])
        except ValueError as error:
            raise ValueError(f"Invalid numeric benchmark metrics: {name}") from error
        if (
            iterations <= 0
            or not math.isfinite(ns_per_op)
            or ns_per_op <= 0
            or bytes_per_op < 0
            or allocs_per_op < 0
        ):
            raise ValueError(f"Out-of-range benchmark metrics: {name}")
        benchmarks[name] = {
            "ns_per_op": ns_per_op,
            "bytes_per_op": bytes_per_op,
            "allocs_per_op": allocs_per_op,
            "iterations": iterations,
        }
    missing = sorted(set(EXPECTED_BENCHMARKS) - benchmarks.keys())
    if missing:
        raise ValueError(f"Missing benchmark results: {', '.join(missing)}")
    now = datetime.now(timezone.utc)
    return {
        "timestamp": now.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "date": now.strftime("%Y-%m-%d"),
        "benchmarks": benchmarks,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    try:
        metrics = parse_results(args.input.read_text(encoding="utf-8"))
        args.output.write_text(
            json.dumps(metrics, indent=2, allow_nan=False) + "\n", encoding="utf-8"
        )
    except (OSError, ValueError) as error:
        print(f"Benchmark validation failed: {error}", file=sys.stderr)
        return 1
    print(f"Validated {len(metrics['benchmarks'])} benchmarks: {args.output}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
