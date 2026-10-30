#!/usr/bin/env python3
"""Publish explicitly selected gh-aw grader results as Harbor rewards."""

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import subprocess
import sys
import tempfile
import time


MAX_SESSION_BYTES = 50 * 1024 * 1024
MAX_OUTPUT_BYTES = 1024 * 1024
MAX_CONFIG_BYTES = 256 * 1024
GRADER_TIMEOUT = 130
ID = re.compile(r"[a-z][a-z0-9-]{0,63}\Z")


class VerificationError(ValueError):
    """The verifier could not compute a valid reward."""


class UnavailableError(VerificationError):
    """A required metric has no applicable evidence."""


def finite_number(value):
    return type(value) is int or (type(value) is float and math.isfinite(value))


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise VerificationError("duplicate JSON key: " + key)
        result[key] = value
    return result


def parse_json(content):
    def reject_constant(value):
        raise VerificationError("non-finite JSON number: " + value)

    def finite_float(value):
        number = float(value)
        if not math.isfinite(number):
            reject_constant(value)
        return number

    return json.loads(
        content, object_pairs_hook=unique_object,
        parse_constant=reject_constant, parse_float=finite_float,
    )


def read_bounded(path, limit):
    with path.open("rb") as stream:
        content = stream.read(limit + 1)
    if len(content) > limit:
        raise VerificationError(f"{path.name} exceeds {limit} bytes")
    return content


def load_config(path):
    content = read_bounded(path, MAX_CONFIG_BYTES)
    config = parse_json(content)
    if not isinstance(config, dict) or set(config) != {"version", "workflow", "metrics"}:
        raise VerificationError("config requires only version, workflow, and metrics")
    if type(config["version"]) is not int or config["version"] != 1:
        raise VerificationError("unsupported verifier config version")
    workflow = config["workflow"]
    if not isinstance(workflow, str) or Path(workflow).is_absolute() or not workflow.endswith(".md"):
        raise VerificationError("workflow must be a relative Markdown path")
    workflow_path = (path.parent / workflow).resolve()
    if not workflow_path.is_relative_to(path.parent.resolve()):
        raise VerificationError("workflow must stay within the trusted config directory")
    metrics = config["metrics"]
    if not isinstance(metrics, list) or not 1 <= len(metrics) <= 32:
        raise VerificationError("metrics must contain between 1 and 32 entries")
    graders, rewards = set(), set()
    for metric in metrics:
        if not isinstance(metric, dict) or set(metric) != {"grader", "reward", "mapping", "required"}:
            raise VerificationError("each metric requires grader, reward, mapping, and required")
        for field in ("grader", "reward"):
            if not isinstance(metric[field], str) or not ID.fullmatch(metric[field]):
                raise VerificationError(f"invalid {field} ID")
        if metric["grader"] in graders or metric["reward"] in rewards:
            raise VerificationError("duplicate grader or reward ID")
        graders.add(metric["grader"])
        rewards.add(metric["reward"])
        if metric["mapping"] not in ("raw", "threshold") or type(metric["required"]) is not bool:
            raise VerificationError("mapping must be raw or threshold; required must be boolean")
    if not any(metric["required"] for metric in metrics):
        raise VerificationError("at least one reward metric must be required")
    return config, workflow_path, content


def map_result(envelope, metric):
    if not isinstance(envelope, dict) or type(envelope.get("version")) is not int or envelope["version"] != 1:
        raise VerificationError("expected version 1 grader definition envelope")
    definition, result = envelope.get("definition"), envelope.get("result")
    if not isinstance(definition, dict) or not isinstance(result, dict):
        raise VerificationError("grader definition and result must be objects")
    if not {"id", "status", "value", "passed"}.issubset(result):
        raise VerificationError("grader result is missing required fields")
    if definition.get("id") != metric["grader"] or result.get("id") != metric["grader"]:
        raise VerificationError("grader result identity mismatch")
    status, value, passed = result.get("status"), result.get("value"), result.get("passed")
    if status == "unavailable":
        if value is not None or passed is not None:
            raise VerificationError("unavailable result must have null value and passed")
        if metric["required"]:
            raise UnavailableError("required grader is unavailable: " + metric["grader"])
        return None
    if status not in ("pass", "fail") or not finite_number(value):
        raise VerificationError("grader did not return a finite successful measurement")
    if passed is not None and type(passed) is not bool:
        raise VerificationError("passed must be boolean or null")
    if (status == "fail") != (passed is False):
        raise VerificationError("inconsistent grader status and threshold outcome")
    if metric["mapping"] == "raw":
        return value
    threshold, direction = definition.get("threshold"), definition.get("direction")
    if not finite_number(threshold) or direction not in ("higher_is_better", "lower_is_better"):
        raise VerificationError("threshold mapping requires a finite threshold and direction")
    expected = value >= threshold if direction == "higher_is_better" else value <= threshold
    if passed is not expected:
        raise VerificationError("grader threshold outcome does not match resolved acceptance metadata")
    return int(passed)


def run_grader(binary, workflow, grader_id, session):
    with tempfile.TemporaryFile() as source:
        source.write(session)
        source.seek(0)
        child = subprocess.Popen(
            [str(binary), "graders", "run", str(workflow), grader_id, "--include-definition"],
            stdin=source, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            cwd=workflow.parent, env={"PATH": os.environ.get("PATH", os.defpath)},
            start_new_session=True,
        )
        streams = {child.stdout: bytearray(), child.stderr: bytearray()}
        deadline = time.monotonic() + GRADER_TIMEOUT
        try:
            with selectors.DefaultSelector() as selector:
                for stream in streams:
                    selector.register(stream, selectors.EVENT_READ)
                while selector.get_map():
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise VerificationError("grader timed out")
                    for key, _ in selector.select(remaining):
                        chunk = os.read(key.fileobj.fileno(), 65536)
                        if not chunk:
                            selector.unregister(key.fileobj)
                            continue
                        streams[key.fileobj].extend(chunk)
                        if len(streams[key.fileobj]) > MAX_OUTPUT_BYTES:
                            raise VerificationError("grader output exceeds 1 MiB")
            child.wait(timeout=max(0, deadline - time.monotonic()))
        except subprocess.TimeoutExpired as error:
            raise VerificationError("grader timed out") from error
        finally:
            # The CLI can own Node/Bash workers; clean up the whole trial process group.
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            child.wait()
            for stream in streams:
                stream.close()
        output = streams[child.stdout]
        envelope = parse_json(output) if output else None
        return child.returncode, envelope, streams[child.stderr].decode("utf-8", errors="replace")


def atomic_json(path, value):
    descriptor, temporary = tempfile.mkstemp(prefix="." + path.name + ".", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, indent=2, allow_nan=False)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def file_digest(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify(config_path, session_path, output_dir, binary):
    output_dir.mkdir(parents=True, exist_ok=True)
    for name in ("reward.json", "reward.txt", "gh-aw-graders.json"):
        (output_dir / name).unlink(missing_ok=True)
    report = {"version": 1, "status": "error", "results": [], "coverage": {}}
    try:
        config, workflow, config_bytes = load_config(config_path)
        session = read_bounded(session_path, MAX_SESSION_BYTES)
        resolved_binary = shutil.which(str(binary))
        if resolved_binary is None:
            raise VerificationError("gh-aw executable not found")
        binary_path = Path(resolved_binary).resolve()
        report["provenance"] = {
            "session_sha256": hashlib.sha256(session).hexdigest(),
            "config_sha256": hashlib.sha256(config_bytes).hexdigest(),
            "workflow_sha256": file_digest(workflow),
            "binary_sha256": file_digest(binary_path),
        }
        rewards = {}
        unavailable = []
        for metric in config["metrics"]:
            code, envelope, stderr = run_grader(binary_path, workflow, metric["grader"], session)
            report["results"].append({
                "selection": metric, "output": envelope, "exit_code": code, "stderr": stderr,
            })
            if code != 0:
                raise VerificationError("grader process failed: " + metric["grader"])
            try:
                value = map_result(envelope, metric)
            except UnavailableError as error:
                unavailable.append(str(error))
                value = None
            report["coverage"][metric["reward"]] = value is not None
            if value is not None:
                rewards[metric["reward"]] = value
        if unavailable:
            raise UnavailableError("; ".join(unavailable))
        report["status"] = "success"
        atomic_json(output_dir / "gh-aw-graders.json", report)
        atomic_json(output_dir / "reward.json", rewards)
        return 0
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        report["status"] = "unavailable" if isinstance(error, UnavailableError) else "error"
        report["error"] = str(error)
        atomic_json(output_dir / "gh-aw-graders.json", report)
        print(f"gh-aw verifier {report['status']}: {error}", file=sys.stderr)
        return 2 if isinstance(error, UnavailableError) else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True, help="Trusted verifier mapping JSON")
    parser.add_argument("--session", type=Path, required=True, help="Redacted unified session JSONL or event array")
    parser.add_argument("--output-dir", type=Path, default=Path("/logs/verifier"))
    parser.add_argument("--gh-aw", default="gh-aw", help="Standalone gh-aw executable (not the gh launcher)")
    args = parser.parse_args()
    return verify(args.config.resolve(), args.session, args.output_dir, args.gh_aw)


if __name__ == "__main__":
    sys.exit(main())
