#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  set -- .github/workflows/cgo.yml .github/workflows/cjs.yml
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

failed=0
for workflow in "$@"; do
  echo "Checking $workflow"
  if [ ! -f "$workflow" ]; then
    echo "Missing workflow file: $workflow"
    failed=1
    continue
  fi

  # These workflows should stay pure test workflows: allow only the built-in
  # GITHUB_TOKEN.
  disallowed_secrets_file="$tmp_dir/disallowed-secrets.txt"
  if ! python3 - "$workflow" >"$disallowed_secrets_file" <<'PY'; then
import re
import sys

workflow = sys.argv[1]
allowed = {"GITHUB_TOKEN"}

with open(workflow, encoding="utf-8") as f:
    content = f.read()

expression_re = re.compile(r"\$\{\{(.*?)\}\}", re.DOTALL)
secret_re = re.compile(
    r"\bsecrets\b\s*(?:"
    r"\.\s*([A-Za-z_][A-Za-z0-9_]*)"
    r"|\[\s*(['\"])(.*?)\2\s*\]"
    r"|\[([^\]]*)\]"
    r")",
    re.DOTALL,
)

for expression in expression_re.finditer(content):
    expression_text = expression.group(1)
    expression_line = content.count("\n", 0, expression.start()) + 1
    for secret in secret_re.finditer(expression_text):
        property_name = secret.group(1)
        literal_name = secret.group(3)
        computed_key = secret.group(4)
        if property_name is not None:
            name = property_name
            display = f"secrets.{name}"
        elif literal_name is not None:
            name = literal_name
            display = f"secrets[{secret.group(2)}{name}{secret.group(2)}]"
        else:
            key = (computed_key or "").strip()
            print(f"{workflow}:{expression_line}: computed secrets key secrets[{key}]")
            continue

        if name not in allowed:
            print(f"{workflow}:{expression_line}: {display}")
PY
    echo "Failed to scan secrets expressions in $workflow"
    failed=1
    continue
  fi
  if [ -s "$disallowed_secrets_file" ]; then
    echo "Disallowed secrets expressions found in $workflow:"
    cat "$disallowed_secrets_file"
    failed=1
  fi

  write_permissions_file="$tmp_dir/write-permissions.txt"
  if ! python3 - "$workflow" >"$write_permissions_file" <<'PY'; then
import re
import sys

workflow = sys.argv[1]

# `actions: write` is required by the cleanup jobs that delete the shared
# checkout cache at the end of a run; it grants no access to repository content.
allowed_write_scopes = {"actions"}


def strip_comment(line):
    quote = None
    escaped = False
    for index, char in enumerate(line):
        if escaped:
            escaped = False
            continue
        if char == "\\" and quote == '"':
            escaped = True
            continue
        if quote:
            if char == quote:
                quote = None
            continue
        if char in {"'", '"'}:
            quote = char
            continue
        if char == "#":
            return line[:index]
    return line


def normalize(value):
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        value = value[1:-1]
    return value.strip()


def flow_pairs(value):
    value = value.strip()
    if not (value.startswith("{") and value.endswith("}")):
        return []
    body = value[1:-1]
    parts = []
    start = 0
    quote = None
    escaped = False
    for index, char in enumerate(body):
        if escaped:
            escaped = False
            continue
        if char == "\\" and quote == '"':
            escaped = True
            continue
        if quote:
            if char == quote:
                quote = None
            continue
        if char in {"'", '"'}:
            quote = char
            continue
        if char == ",":
            parts.append(body[start:index])
            start = index + 1
    parts.append(body[start:])
    pairs = []
    for part in parts:
        if ":" not in part:
            continue
        key, pair_value = part.split(":", 1)
        pairs.append((key.strip(), normalize(pair_value)))
    return pairs


with open(workflow, encoding="utf-8") as f:
    lines = f.readlines()

in_permissions = False
permissions_indent = -1

for line_number, line in enumerate(lines, start=1):
    without_comment = strip_comment(line).rstrip()
    stripped = without_comment.strip()
    if in_permissions:
        if not stripped:
            continue
        indent = len(without_comment) - len(without_comment.lstrip(" "))
        if indent <= permissions_indent:
            in_permissions = False
        else:
            match = re.match(r"([A-Za-z0-9_-]+)\s*:\s*(.+)$", stripped)
            if (
                match
                and normalize(match.group(2)) == "write"
                and match.group(1) not in allowed_write_scopes
            ):
                print(f"{workflow}:{line_number}: {line.rstrip()}")
            continue

    match = re.match(r"^(\s*)permissions\s*:\s*(.*)$", without_comment)
    if not match:
        continue

    permissions_indent = len(match.group(1))
    value = match.group(2).strip()
    if not value:
        in_permissions = True
        continue

    normalized = normalize(value)
    if normalized == "write-all":
        print(f"{workflow}:{line_number}: {line.rstrip()}")
        continue
    for pair_key, pair_value in flow_pairs(value):
        if pair_value == "write" and pair_key not in allowed_write_scopes:
            print(f"{workflow}:{line_number}: {line.rstrip()}")
            break
PY
    echo "Failed to scan permissions in $workflow"
    failed=1
    continue
  fi
  if [ -s "$write_permissions_file" ]; then
    echo "Write permissions found in $workflow:"
    cat "$write_permissions_file"
    failed=1
  fi
done

exit "$failed"
