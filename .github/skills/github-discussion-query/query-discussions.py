#!/usr/bin/env python3
"""Query GitHub discussions with jq filtering support

Usage: ./query-discussions.py [OPTIONS]

Options:
  --repo OWNER/REPO    Repository to query (default: current repo)
  --limit N            Maximum number of discussions (default: 30)
  --jq EXPRESSION      jq filter expression to apply to output (REQUIRED for data)

Note: When --jq is not provided, returns schema and data size instead of full data.
      This prevents overwhelming responses. Use --jq '.' to get all data.
"""

import argparse
import json
import subprocess
import sys
from typing import Optional


def _run_subprocess(cmd: list, error_prefix: str, not_found_message: str, input_data: Optional[str] = None) -> str:
    """Run a subprocess command, printing a consistent error and exiting on failure."""
    try:
        result = subprocess.run(cmd, input=input_data, capture_output=True, text=True, check=True)
        return result.stdout
    except subprocess.CalledProcessError as e:
        print(f"{error_prefix}: {e.stderr}", file=sys.stderr)
        sys.exit(1)
    except FileNotFoundError:
        print(not_found_message, file=sys.stderr)
        sys.exit(1)


def run_gh_command(repo: Optional[str], limit: int) -> str:
    """Run gh discussion list command and return JSON output."""
    json_fields = "number,title,author,createdAt,updatedAt,body,category,labels,comments,answer,url"

    cmd = ["gh", "discussion", "list", "--limit", str(limit), "--json", json_fields]

    if repo:
        cmd.extend(["--repo", repo])

    return _run_subprocess(cmd, "Error running gh command", "Error: gh CLI not found. Please install GitHub CLI.")


def apply_jq_filter(data: str, jq_filter: str) -> str:
    """Apply jq filter to JSON data."""
    return _run_subprocess(
        ["jq", jq_filter],
        "Error applying jq filter",
        "Error: jq not found. Please install jq.",
        input_data=data,
    )


def generate_schema_response(data: str) -> str:
    """Generate schema and metadata response when no jq filter is provided."""
    try:
        parsed = json.loads(data)
        item_count = len(parsed) if isinstance(parsed, list) else 0
        data_size = len(data)
    except json.JSONDecodeError:
        item_count = 0
        data_size = len(data)

    schema = {
        "message": "No --jq filter provided. Use --jq to filter and retrieve data.",
        "item_count": item_count,
        "data_size_bytes": data_size,
        "schema": {
            "type": "array",
            "description": "Array of discussion objects",
            "item_fields": {
                "number": "integer - Discussion number",
                "title": "string - Discussion title",
                "author": "object - Author info with login field",
                "createdAt": "string - ISO timestamp of creation",
                "updatedAt": "string - ISO timestamp of last update",
                "body": "string - Discussion body content",
                "category": "object - Category info with name field",
                "labels": "array - Array of label objects with name field",
                "comments": "object - Comments info with totalCount field",
                "answer": "object|null - Accepted answer if exists",
                "url": "string - Discussion URL"
            }
        },
        "suggested_queries": [
            {"description": "Get all data", "query": "."},
            {"description": "Get discussion numbers and titles", "query": ".[] | {number, title}"},
            {"description": "Get discussions by author", "query": '.[] | select(.author.login == "USERNAME")'},
            {"description": "Get discussions in category", "query": '.[] | select(.category.name == "Ideas")'},
            {"description": "Get answered discussions", "query": ".[] | select(.answer != null)"},
            {"description": "Get unanswered discussions", "query": ".[] | select(.answer == null) | {number, title, category: .category.name}"},
            {"description": "Get discussions with labels", "query": ".[] | {number, title, labels: [.labels[].name]}"},
            {"description": "Count by category", "query": "group_by(.category.name) | map({category: .[0].category.name, count: length})"}
        ]
    }

    return json.dumps(schema, indent=2)


def main():
    parser = argparse.ArgumentParser(
        description="Query GitHub discussions with jq filtering support",
        formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--repo", help="Repository to query (default: current repo)")
    parser.add_argument("--limit", type=int, default=30, help="Maximum number of discussions (default: 30)")
    parser.add_argument("--jq", dest="jq_filter", help="jq filter expression to apply to output")

    args = parser.parse_args()

    # Get data from gh CLI
    output = run_gh_command(args.repo, args.limit)

    # Apply jq filter if provided, otherwise return schema
    if args.jq_filter:
        result = apply_jq_filter(output, args.jq_filter)
        print(result, end='')
    else:
        result = generate_schema_response(output)
        print(result)


if __name__ == "__main__":
    main()
