#!/usr/bin/env python3
"""Capture and verify minimized model-routing audit fixtures."""

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path
from urllib.parse import urlparse


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
GOLDEN_ROOT = REPOSITORY_ROOT / "pkg/cli/testdata/model_routing_golden"
ALLOWED_FILES = (
    "aw_info.json",
    "agent/aw_info.json",
    "usage/aw_info.json",
    "agent/awf-routing-outcome.json",
    "agent_usage.json",
    "usage/agent_usage.json",
    "usage/aw_session.jsonl",
    "agent-session.jsonl",
    "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl",
    "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl",
)
CONTENT_KEYS = {
    "arguments",
    "body",
    "command",
    "content",
    "input",
    "message",
    "output",
    "payload",
    "prompt",
    "result",
    "task",
    "text",
}
SESSION_TYPES = (
    "workflow.info",
    "model_routing.outcome",
    "firewall.model_routing",
    "firewall.token_usage",
    "agent.execution",
)
IDENTITY_REPLACEMENTS = {
    "owner": "example-org",
    "repo": "routing-fixture",
    "user": "fixture-user",
}


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verify", action="store_true", help="verify committed fixtures and goldens without downloading")
    parser.add_argument("--run", help="workflow run ID or URL to download")
    parser.add_argument("--source-dir", type=Path, help="use an already-downloaded run directory (offline/testing)")
    parser.add_argument("--case", help="fixture case name")
    parser.add_argument("--repo", help="repository for numeric run IDs (defaults to GITHUB_REPOSITORY or origin)")
    parser.add_argument("--output-root", type=Path, default=GOLDEN_ROOT, help=argparse.SUPPRESS)
    parser.add_argument("--replace", action="store_true", help="replace an existing fixture case")
    return parser.parse_args()


def validate_case(case):
    if not case or not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", case):
        raise ValueError("case names must use lowercase letters, digits, and single hyphens")


def collect_identities(value, parent_key="", found=None):
    if found is None:
        found = {"owner": set(), "repo": set(), "user": set()}
    if isinstance(value, dict):
        for key, child in value.items():
            normalized = re.sub(r"[^a-z]", "", key.lower())
            if isinstance(child, str):
                if normalized in {"owner", "organization", "organizationlogin", "repositoryowner", "org"}:
                    found["owner"].add(child)
                elif normalized in {"repo", "reponame"} or (normalized in {"name", "fullname"} and parent_key in {"repo", "repository"}):
                    found["repo"].add(child)
                elif normalized in {"actor", "actorlogin", "author", "createdby", "login", "user", "username"}:
                    found["user"].add(child)
                elif normalized == "repository" and "/" in child:
                    owner, repo = child.split("/", 1)
                    found["owner"].add(owner)
                    found["repo"].add(repo)
            collect_identities(child, normalized, found)
    elif isinstance(value, list):
        for child in value:
            collect_identities(child, parent_key, found)
    return found


def sensitive_key(key):
    normalized = re.sub(r"[^a-z]", "", key.lower())
    if normalized in CONTENT_KEYS:
        return True
    if normalized.startswith("task") and normalized != "tasktype":
        return True
    if normalized.startswith("tool") and any(part in normalized for part in ("content", "input", "output", "result", "argument", "command")):
        return True
    if normalized in {"cwd", "gitroot", "conversationhash", "signature", "encrypted"}:
        return True
    if any(part in normalized for part in ("conversationhash", "signature", "encrypted")):
        return True
    if any(part in normalized for part in ("prompt", "message", "content")) and not normalized.endswith("tokens"):
        return True
    return False


def redact_string(value, key, identities):
    normalized = re.sub(r"[^a-z]", "", key.lower())
    if normalized in {"owner", "organization", "organizationlogin", "repositoryowner", "org"}:
        return IDENTITY_REPLACEMENTS["owner"]
    if normalized in {"repo", "reponame"}:
        return IDENTITY_REPLACEMENTS["repo"]
    if normalized in {"actor", "actorlogin", "author", "createdby", "login", "user", "username"}:
        return IDENTITY_REPLACEMENTS["user"]
    if normalized == "repository" and "/" in value:
        return f"{IDENTITY_REPLACEMENTS['owner']}/{IDENTITY_REPLACEMENTS['repo']}"

    protected = any(part in normalized for part in ("model", "effort", "endpoint", "provider", "engine", "schema"))
    if not protected:
        replacements = []
        for identity_type, names in identities.items():
            for name in names:
                if name:
                    replacements.append((name, IDENTITY_REPLACEMENTS[identity_type]))
        for name, replacement in sorted(replacements, key=lambda item: len(item[0]), reverse=True):
            value = re.sub(rf"(?<![A-Za-z0-9]){re.escape(name)}(?![A-Za-z0-9])", replacement, value, flags=re.IGNORECASE)
    value = re.sub(r"\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b", "[redacted-email]", value, flags=re.IGNORECASE)
    value = re.sub(r"(?:(?:/home|/Users)/)[^/\\\s]+", "[redacted]", value)
    return value


def redact(value, identities, key="", parent_key=""):
    if sensitive_key(key):
        return "[redacted]"
    if isinstance(value, dict):
        return {child_key: redact(child, identities, child_key, re.sub(r"[^a-z]", "", key.lower())) for child_key, child in value.items()}
    if isinstance(value, list):
        return [redact(child, identities, key, parent_key) for child in value]
    if isinstance(value, str):
        if re.sub(r"[^a-z]", "", key.lower()) in {"name", "fullname"} and parent_key in {"repo", "repository"}:
            return IDENTITY_REPLACEMENTS["repo"]
        return redact_string(value, key, identities)
    return value


def event_type(record):
    return record.get("type", record.get("event", "")) if isinstance(record, dict) else ""


def is_session_event_allowed(name, pi_session):
    return (
        name.startswith("session.")
        or name in SESSION_TYPES
        or name.startswith("subagent.")
        or name.startswith("pi.subagent_")
        or (pi_session and name.startswith("tool.execution_"))
    )


def read_jsonl(path):
    records = []
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        if not line.strip():
            continue
        try:
            records.append(json.loads(line))
        except json.JSONDecodeError as error:
            raise ValueError(f"{path}:{number}: invalid JSON: {error}") from error
    return records


def write_json(path, value, identities):
    path.parent.mkdir(parents=True, exist_ok=True)
    cleaned = redact(value, identities)
    path.write_text(json.dumps(cleaned, indent=2, ensure_ascii=False, allow_nan=False) + "\n", encoding="utf-8")


def write_jsonl(path, records, identities, session=False):
    if session:
        pi_session = any(event_type(record).startswith("pi.subagent_") for record in records)
        for record in records:
            if isinstance(record, dict):
                data = record.get("data", {})
                if isinstance(data, dict) and any(
                    key.lower() in {"sourceengine", "engine", "producer"} and value == "pi"
                    for key, value in data.items()
                ):
                    pi_session = True
        records = [record for record in records if is_session_event_allowed(event_type(record), pi_session)]
    if not records:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="\n") as output:
        for record in records:
            output.write(json.dumps(redact(record, identities), separators=(",", ":"), ensure_ascii=False, allow_nan=False))
            output.write("\n")


def safe_source_file(root, relative):
    path = root / relative
    if not path.exists():
        return None
    current = root
    for part in Path(relative).parts:
        current = current / part
        if current.is_symlink():
            raise ValueError(f"refusing symlink in source path: {relative}")
    resolved = path.resolve()
    if not resolved.is_relative_to(root.resolve()):
        raise ValueError(f"source path escapes downloaded run: {relative}")
    if not resolved.is_file():
        return None
    return resolved


def prepare_fixture(source, destination):
    source = source.resolve()
    info_values = []
    for rel in ("aw_info.json", "agent/aw_info.json", "usage/aw_info.json"):
        path = safe_source_file(source, rel)
        if path is not None:
            info_values.append(json.loads(path.read_text(encoding="utf-8")))
    identities = {"owner": set(), "repo": set(), "user": set()}
    for value in info_values:
        identities = collect_identities(value, found=identities)

    copied = []
    for rel in ALLOWED_FILES:
        source_path = safe_source_file(source, rel)
        if source_path is None:
            continue
        destination_path = destination / rel
        if rel.endswith(".jsonl"):
            write_jsonl(destination_path, read_jsonl(source_path), identities, session="session" in Path(rel).name)
        else:
            write_json(destination_path, json.loads(source_path.read_text(encoding="utf-8")), identities)
        if destination_path.exists():
            copied.append(rel)
    if not copied:
        raise ValueError(f"no routing or token-usage fixture files found under {source}")
    return copied


def find_downloaded_run(output_root):
    candidates = {output_root.resolve()}
    for rel in ALLOWED_FILES:
        for path in output_root.rglob(Path(rel).name):
            if path.is_file():
                candidates.add(path.parent)
                candidates.update(path.parents)
    candidates = [path for path in candidates if path == output_root.resolve() or path.is_relative_to(output_root.resolve())]
    scores = [(sum((candidate / rel).is_file() for rel in ALLOWED_FILES), candidate) for candidate in candidates]
    best_score, best = max(scores, default=(0, output_root.resolve()), key=lambda item: item[0])
    if best_score == 0:
        raise ValueError(f"no relevant downloaded artifacts found under {output_root}")
    return best


def verify_fixture(case=None):
    if case and not (GOLDEN_ROOT / case / "expected.json").is_file():
        raise ValueError(f"no committed golden fixture found for case {case!r}")
    env = os.environ.copy()
    if case:
        validate_case(case)
        test_pattern = f"^TestModelRoutingGoldenAudit/{case}$"
    else:
        test_pattern = "^TestModelRoutingGolden"
    subprocess.run(
        ["go", "test", "./pkg/cli", "-run", test_pattern, "-count=1"],
        cwd=REPOSITORY_ROOT,
        env=env,
        check=True,
    )


def infer_repository():
    repository = os.environ.get("GITHUB_REPOSITORY")
    if repository:
        return repository
    remote = subprocess.run(
        ["git", "remote", "get-url", "origin"],
        cwd=REPOSITORY_ROOT,
        text=True,
        capture_output=True,
        check=True,
    ).stdout.strip()
    parsed = urlparse(remote)
    path = parsed.path if "://" in remote else remote.rsplit(":", 1)[-1]
    parts = [part for part in path.strip("/").removesuffix(".git").split("/") if part]
    if len(parts) < 2:
        raise ValueError("cannot infer repository from origin; pass --repo owner/repo")
    return "/".join(parts[-2:])


def download_run(run_reference, destination, repository):
    binary = destination.parent / "gh-aw"
    subprocess.run(
        ["go", "build", "-o", str(binary), "./cmd/gh-aw"],
        cwd=REPOSITORY_ROOT,
        check=True,
    )
    command = [
        str(binary),
        "logs",
        "--stdin",
        "--artifacts",
        "all",
        "--output",
        str(destination),
        "--summary-file",
        "",
    ]
    if repository:
        command.extend(["--repo", repository])
    subprocess.run(command, cwd=destination, input=run_reference + "\n", text=True, check=True)


def validate_capture(full_download, case, output_root, replace):
    destination = output_root / case
    if destination.exists() and not replace:
        raise ValueError(f"{destination} already exists; pass --replace to overwrite it")
    output_root.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(prefix="model-routing-golden-", dir=output_root.parent) as temporary:
        temporary_root = Path(temporary)
        staged_fixture = temporary_root / "fixture"
        copied = prepare_fixture(full_download, staged_fixture)
        env = os.environ.copy()
        env["MODEL_ROUTING_GOLDEN_FULL_DIR"] = str(full_download.resolve())
        env["MODEL_ROUTING_GOLDEN_FIXTURE_DIR"] = str(staged_fixture.resolve())
        subprocess.run(
            ["go", "test", "./pkg/cli", "-run", "^TestModelRoutingGoldenCaptureMatchesDownload$", "-count=1"],
            cwd=REPOSITORY_ROOT,
            env=env,
            check=True,
        )

        backup = temporary_root / "previous"
        if destination.exists():
            os.replace(destination, backup)
        try:
            os.replace(staged_fixture, destination)
        except Exception:
            if backup.exists():
                os.replace(backup, destination)
            raise
    print(f"Captured {len(copied)} sanitized files in {destination}")
    print("Analysis matches the full download. Add or update the case in model_routing_golden_test.go, then run make update-model-routing-golden.")


def main():
    args = parse_args()
    try:
        if args.verify:
            if args.run or args.source_dir or args.replace:
                raise ValueError("--verify cannot be combined with download or capture options")
            verify_fixture(args.case)
            return 0
        if not args.case:
            raise ValueError("--case is required when capturing a fixture")
        validate_case(args.case)
        if bool(args.run) == bool(args.source_dir):
            raise ValueError("provide exactly one of --run or --source-dir")
        if args.source_dir:
            full_download = args.source_dir.resolve()
            if not full_download.is_dir():
                raise ValueError(f"source directory does not exist: {full_download}")
            validate_capture(full_download, args.case, args.output_root.resolve(), args.replace)
        else:
            with tempfile.TemporaryDirectory(prefix="model-routing-download-") as temporary:
                download_root = Path(temporary) / "download"
                download_root.mkdir()
                repository = args.repo
                if not repository and args.run.isdecimal():
                    repository = infer_repository()
                download_run(args.run, download_root, repository)
                downloaded_run = find_downloaded_run(download_root)
                validate_capture(downloaded_run, args.case, args.output_root.resolve(), args.replace)
        return 0
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"model-routing-golden: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
