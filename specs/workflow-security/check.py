#!/usr/bin/env python3
"""Check the bounded model, negative controls, and representative executions."""

import argparse
from collections import Counter
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parent
JAR_SHA256 = "b490f45c1de08e4ff9753259a00338981b9cf464f01ca9e9cd5f19f33cf0bb92"
MUTATIONS = {
    "agent-write": "JobIsolation",
    "persist-credentials": "NoCredentialPersistence",
    "artifact-origin": "ArtifactProvenance",
    "skip-detection": "DetectionGate",
    "cross-repo-token": "GitAuthorization",
    "app-scope": "AppLeastPrivilege",
    "secret-channel": "SecretConfinement",
    "untrusted-execution": "TrustedExecution",
    "checkout-widen": "GitAuthorization",
    "expired-token": "TokenLifetime",
    "network-bypass": "NetworkPolicy",
    "untrusted-config": "TrustedConfiguration",
    "output-limit": "OutputLimit",
    "private-sink": "PrivateSinkPolicy",
    "target-authorization": "ValidatedEffects",
    "retained-push-token": "PrivilegedCheckoutIsolation",
    "ambient-agent-fetch": "NoImplicitFetch",
    "cleanup-fail-open": "NoCredentialPersistence",
    "artifact-invocation": "ArtifactProvenance",
    "policy-downgrade": "DetectionGate",
}
MUTATION_EFFECTS = {"retained-push-token": "pull-request"}
MUTATION_SETTINGS = {
    "cleanup-fail-open": {"checkout_mode": "force-clean"},
    "artifact-invocation": {"artifact_mode": "reusable"},
}
WITNESSES = {
    "successful-write": "NoSuccessfulWrite",
    "denied-request": "NoDeniedRequest",
    "missing-git-data": "NoMissingGitData",
    "cross-repo-checkout": "NoCrossRepoCheckout",
    "failed-job": "NoFailedJob",
    "privileged-push": "NoSuccessfulWrite",
    "cleanup-failure": "NoCleanupFailure",
    "temporary-credentials": "NoTemporaryCredentials",
}
WITNESS_EFFECTS = {"privileged-push": "pull-request"}
WITNESS_SETTINGS = {
    "cleanup-failure": {"checkout_mode": "force-clean"},
    "temporary-credentials": {"checkout_mode": "force-clean"},
}
DELTAS = {
    "agent-write": "Agent permissions include contents: write (strict compiler rejection expected).",
    "persist-credentials": "Compiler mutation: actions/checkout persists its entry token.",
    "artifact-origin": "Runtime mutation: accept a request artifact from another run.",
    "skip-detection": "Runtime mutation: publish an effect despite a deny verdict.",
    "cross-repo-token": "Compiler mutation: use the main-repository token for the dependency checkout.",
    "app-scope": "Compiler mutation: request administration permission outside the consumer's computed grant.",
    "secret-channel": "Runtime mutation: copy a write token to an agent-visible channel.",
    "untrusted-execution": "Runtime mutation: execute request bytes as code, rather than trusted handlers.",
    "checkout-widen": "Runtime mutation: implicitly fetch an unavailable sparse/partial-clone blob.",
    "expired-token": "Runtime mutation: reuse the app token after revocation.",
    "network-bypass": "Runtime mutation: allow egress outside the configured allowlist.",
    "untrusted-config": "Compiler mutation: load trusted activation configuration from an untrusted ref.",
    "output-limit": "Runtime mutation: execute more issue writes than create-issue.max permits.",
    "private-sink": "Runtime mutation: publish private-source content to a public issue without authorization.",
    "target-authorization": "Runtime mutation: validate requests to an undeclared repository.",
    "retained-push-token": "Runtime mutation: retain privileged git credentials after app token revocation.",
    "ambient-agent-fetch": "Runtime mutation: provide ambient git fetch credentials to the agent after checkout.",
    "cleanup-fail-open": "Historical compiler failure path: ignore failed checkout cleanup or residual credentials and start the agent.",
    "artifact-invocation": "Runtime mutation: accept another reusable invocation's artifact under the same run.",
    "policy-downgrade": "Compiler mutation: omit required detection without an authorized policy opt-out.",
}


def check_result(status, text, invariant=None):
    if invariant is None:
        if status != 0 or "Model checking completed. No error has been found." not in text:
            raise RuntimeError("Expected exhaustive success, not a TLC/tooling failure")
    elif status != 12 or f"Invariant {invariant} is violated." not in text:
        raise RuntimeError(f"Expected ONLY invariant {invariant} with TLC exit code 12")
    match = re.search(r"(\d+) states generated, (\d+) distinct states found", text)
    if not match:
        raise RuntimeError("TLC did not report state counts")
    return {"generated": int(match[1]), "distinct": int(match[2])}


def normalize_trace(raw):
    if not isinstance(raw, dict) or not isinstance(raw.get("counterexample"), dict):
        raise RuntimeError("Expected TLC counterexample object")
    states = raw["counterexample"].get("state")
    if not isinstance(states, list) or len(states) < 2:
        raise RuntimeError("Counterexample has no transition")
    normalized = []
    for entry in states:
        if (not isinstance(entry, list) or len(entry) != 2
                or not isinstance(entry[1], dict) or "s" not in entry[1]):
            raise RuntimeError("Unexpected TLC state encoding")
        normalized.append({"index": entry[0], **entry[1]})
    return {"states": normalized, "actions": raw["counterexample"].get("action", [])}


def concretize(fault, invariant, destination, trace):
    """Preserve a trace beside a source seed; never claim a mutation is a bug."""
    seed = (ROOT / "fixtures" / "cross-repo.md").read_text()
    if fault == "agent-write":
        seed = seed.replace("contents: read", "contents: write", 1)
    if fault == "cleanup-fail-open":
        seed = seed.replace("    fetch-depth: 1", "    force-clean-git-credentials: true\n    fetch-depth: 1", 1)
    if MUTATION_EFFECTS.get(fault) == "pull-request":
        seed = seed.replace("create-issue:", "create-pull-request:", 1)
        seed = seed.replace("issue write", "pull-request write")
        seed = seed.replace("Request an issue", "Request a pull request")
        seed = seed.replace("create-issue safe output", "create-pull-request safe output")
    header = (
        f"\n<!-- Synthetic negative control: {fault}; invariant: {invariant}.\n"
        f"{DELTAS[fault]}\n"
        "This is NOT a confirmed product vulnerability. Except agent-write, the\n"
        "source seed needs the specified compiler/runtime mutation to realize\n"
        "the model trace. Do not dispatch it or use real secrets.\n"
        f"Trace states: {len(trace['states'])}; see trace.json and TLC.log. -->\n"
    )
    (destination / "source.md").write_text(seed + header)
    (destination / "counterexample.json").write_text(
        json.dumps(
            {
                "classification": "synthetic-negative-control",
                "fault": fault,
                "invariant": invariant,
                "implementation_delta": DELTAS[fault],
                "source": "source.md",
                "trace": "trace.json",
                "confirmed_product_bug": False,
            },
            indent=2,
        ) + "\n"
    )


def run_case(java, jar, results, name, fault="none", profile="sparse",
             invariant=None, witness=False, effect="issue", detection_policy="required",
             detection_enabled=True, checkout_mode="transient", artifact_mode="plain"):
    destination = results / name
    destination.mkdir()
    config = (ROOT / "CompiledWorkflow.cfg").read_text()
    config = config.replace('Fault = "none"', f'Fault = "{fault}"')
    config = config.replace('Profile = "sparse"', f'Profile = "{profile}"')
    config = config.replace('Effect = "issue"', f'Effect = "{effect}"')
    config = config.replace('DetectionPolicy = "required"', f'DetectionPolicy = "{detection_policy}"')
    config = config.replace("DetectionEnabled = TRUE", f"DetectionEnabled = {'TRUE' if detection_enabled else 'FALSE'}")
    config = config.replace('CheckoutMode = "transient"', f'CheckoutMode = "{checkout_mode}"')
    config = config.replace('ArtifactMode = "plain"', f'ArtifactMode = "{artifact_mode}"')
    if witness:
        config += f"\nINVARIANT {invariant}\n"
    (destination / "model.cfg").write_text(config)
    (destination / "CompiledWorkflow.tla").write_text(
        (ROOT / "CompiledWorkflow.tla").read_text()
    )
    command = [
        java, "-XX:+UseParallelGC", "-Xmx1g", "-cp", str(jar), "tlc2.TLC",
        "-workers", "1", "-seed", "1", "-fp", "0",
        "-config", "model.cfg", "-metadir", "states",
    ]
    if invariant:
        command += ["-dumpTrace", "json", str(destination / "tlc-trace.json")]
    command += ["CompiledWorkflow.tla"]
    (destination / "command.json").write_text(
        json.dumps({"cwd": str(destination), "argv": command}, indent=2) + "\n"
    )
    try:
        result = subprocess.run(
            command, cwd=destination, capture_output=True, text=True, timeout=120
        )
    except subprocess.TimeoutExpired as error:
        (destination / "TLC.log").write_text(
            (error.stdout or b"").decode() + (error.stderr or b"").decode()
        )
        raise RuntimeError(f"{name}: TLC exceeded 120 seconds") from error
    text = result.stdout + result.stderr
    (destination / "TLC.log").write_text(text)
    try:
        counts = check_result(result.returncode, text, invariant)
        if invariant:
            trace = normalize_trace(json.loads((destination / "tlc-trace.json").read_text()))
            (destination / "trace.json").write_text(json.dumps(trace, indent=2) + "\n")
            (destination / "events.txt").write_text(
                "\n".join(state["s"]["lastEvent"] for state in trace["states"]) + "\n"
            )
            if fault != "none":
                concretize(fault, invariant, destination, trace)
    except (RuntimeError, ValueError, OSError) as error:
        raise RuntimeError(f"{name}: {error}; see {destination / 'TLC.log'}") from error
    print(f"{name}: expected result ({counts['distinct']} distinct states)", flush=True)
    return {"case": name, "fault": fault, "profile": profile,
            "effect": effect, "expected_violation": invariant,
            "detection_policy": detection_policy, "detection_enabled": detection_enabled,
            "checkout_mode": checkout_mode, "artifact_mode": artifact_mode,
            "model_sha256": hashlib.sha256((destination / "CompiledWorkflow.tla").read_bytes()).hexdigest(),
            "config_sha256": hashlib.sha256(config.encode()).hexdigest(), **counts}


def check_sources(compiler, results):
    command = [str(compiler.resolve()), "compile",
               str(ROOT / "fixtures" / "cross-repo.md"),
               str(results / "agent-write" / "source.md"),
               "--no-emit", "--approve", "--json"]
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=120)
    except subprocess.TimeoutExpired as error:
        raise RuntimeError("Source compilation exceeded 120 seconds") from error
    (results / "source-compilation.log").write_text(result.stdout + result.stderr)
    try:
        reports = json.loads(result.stdout)
        if not isinstance(reports, list) or len(reports) != 2:
            raise RuntimeError("Expected two compiler reports")
        by_name = {report["workflow"]: report for report in reports}
        rejected = by_name["source.md"]
        messages = " ".join(error["message"] for error in rejected["errors"])
        if (result.returncode != 1 or not by_name["cross-repo.md"]["valid"]
                or rejected["valid"]
                or "strict mode: write permission 'contents: write' is not allowed" not in messages):
            raise RuntimeError("Source seed must compile; strict write-grant mutation must be rejected")
    except (ValueError, KeyError, TypeError) as error:
        raise RuntimeError("Malformed compiler JSON; see source-compilation.log") from error
    version = subprocess.run([str(compiler.resolve()), "version"],
                             capture_output=True, text=True, timeout=30, check=True)
    (results / "source-compilation.json").write_text(json.dumps(
        {"command": command, "compiler_version": version.stdout.strip(),
         "compiler_sha256": hashlib.sha256(compiler.read_bytes()).hexdigest(),
         "seed_sha256": hashlib.sha256((ROOT / "fixtures" / "cross-repo.md").read_bytes()).hexdigest(),
         "mutation_sha256": hashlib.sha256((results / "agent-write" / "source.md").read_bytes()).hexdigest(),
         "reports": reports}, indent=2
    ) + "\n")
    print("Source seed compiled; concrete agent-write source rejected by strict compiler.")


def write_examples(results, cases):
    selected = list(WITNESSES) + [
        "persist-credentials", "skip-detection", "cross-repo-token", "private-sink",
    ]
    examples = []
    for name in selected:
        trace = json.loads((results / name / "trace.json").read_text())
        case = next(case for case in cases if case["case"] == name)
        examples.append({
            "case": name,
            "classification": "reachability-witness" if name in WITNESSES else "synthetic-negative-control",
            "expected_violation": case["expected_violation"],
            "events": [state["s"]["lastEvent"] for state in trace["states"]],
            "final_state": trace["states"][-1]["s"],
        })
    (ROOT / "examples.json").write_text(json.dumps(
        {"model_sha256": cases[0]["model_sha256"], "jar_sha256": JAR_SHA256,
         "confirmed_product_bug": False, "examples": examples}, indent=2
    ) + "\n")


def check_compiled_workflows(verifier, directory, results):
    locks = sorted(directory.glob("*.lock.yml"))
    if not locks:
        raise RuntimeError(f"No compiled workflows found in {directory}")
    reports = []
    for lock in locks:
        command = [str(verifier.resolve()), "--profile", "compiled", str(lock.resolve())]
        result = None
        try:
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            report = json.loads(result.stdout)
            conforms = report["conforms"]
            if report["detection_policy"]["mode"] not in {"enabled", "disabled", "conditional"}:
                raise RuntimeError("Verifier reported an unsupported detector policy")
            if not isinstance(conforms, bool) or result.returncode != (0 if conforms else 1):
                raise RuntimeError("Verifier status does not match its report")
            reports.append({"lock": str(lock), "sha256": hashlib.sha256(lock.read_bytes()).hexdigest(),
                            "command": command, "exit_code": result.returncode, "report": report})
        except (ValueError, KeyError, TypeError, subprocess.TimeoutExpired, RuntimeError) as error:
            reports.append({"lock": str(lock), "tool_error": str(error),
                            "stdout": result.stdout if result is not None else "",
                            "stderr": result.stderr if result is not None else ""})
    failed = [report for report in reports if "tool_error" in report or not report["report"]["conforms"]]
    policy_counts = Counter(report["report"]["detection_policy"]["mode"]
                            for report in reports if "tool_error" not in report)
    payload = {"total": len(reports), "passed": len(reports) - len(failed),
               "failed": len(failed), "policy_counts": dict(policy_counts), "workflows": reports}
    (results / "compiled-workflows.json").write_text(json.dumps(payload, indent=2) + "\n")
    if failed:
        raise RuntimeError(f"{len(failed)} compiled workflow(s) failed; see {results / 'compiled-workflows.json'}")
    print(f"Compiled workflow conformance: {len(reports)}/{len(reports)} pass; policies {dict(policy_counts)}.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--results", type=Path, help="New directory for all reports and traces")
    parser.add_argument("--compiler", type=Path, help="Local gh-aw binary for source acceptance/rejection checks")
    parser.add_argument("--write-examples", action="store_true", help="Regenerate checked-in representative traces")
    parser.add_argument("--verifier", type=Path, help="Built gh-aw-security-model binary")
    parser.add_argument("--compiled-workflows", type=Path, help="Directory of existing lock files to verify")
    args = parser.parse_args()
    if bool(args.verifier) != bool(args.compiled_workflows):
        parser.error("--verifier and --compiled-workflows must be supplied together")
    jar_env = os.environ.get("TLA2TOOLS_JAR")
    if not jar_env:
        parser.error("Set TLA2TOOLS_JAR to the official v1.8.0 tla2tools.jar")
    jar = Path(jar_env).resolve()
    if not jar.is_file() or hashlib.sha256(jar.read_bytes()).hexdigest() != JAR_SHA256:
        parser.error("TLA2TOOLS_JAR is missing or does not match the pinned SHA-256")
    results = args.results.resolve() if args.results else Path(
        tempfile.mkdtemp(prefix="workflow-security-tlc-")
    )
    if args.results:
        results.mkdir(parents=True, exist_ok=False)
    java = os.environ.get("JAVA_BIN", "java")
    print(f"Reports: {results}", flush=True)
    cases = [
        run_case(java, jar, results, "secure-sparse"),
        run_case(java, jar, results, "secure-full", profile="full"),
        run_case(java, jar, results, "secure-pr-sparse", effect="pull-request"),
        run_case(java, jar, results, "secure-pr-full", profile="full", effect="pull-request"),
        run_case(java, jar, results, "secure-force-clean", checkout_mode="force-clean"),
        run_case(java, jar, results, "secure-force-clean-pr", checkout_mode="force-clean", effect="pull-request"),
        run_case(java, jar, results, "secure-reusable", artifact_mode="reusable"),
        run_case(java, jar, results, "secure-detection-disabled", detection_policy="disabled"),
        run_case(java, jar, results, "secure-conditional-enabled", detection_policy="conditional"),
        run_case(java, jar, results, "secure-conditional-disabled", detection_policy="conditional", detection_enabled=False),
    ]
    for fault, invariant in MUTATIONS.items():
        cases.append(run_case(java, jar, results, fault, fault, invariant=invariant,
                              effect=MUTATION_EFFECTS.get(fault, "issue"),
                              **MUTATION_SETTINGS.get(fault, {})))
    for name, invariant in WITNESSES.items():
        cases.append(run_case(java, jar, results, name, invariant=invariant, witness=True,
                              effect=WITNESS_EFFECTS.get(name, "issue"),
                              **WITNESS_SETTINGS.get(name, {})))
    current_hash = hashlib.sha256((ROOT / "CompiledWorkflow.tla").read_bytes()).hexdigest()
    if any(case["model_sha256"] != current_hash for case in cases):
        raise RuntimeError("Model changed during checking; reports do not describe one snapshot")
    if args.compiler:
        check_sources(args.compiler, results)
    if args.verifier:
        check_compiled_workflows(args.verifier, args.compiled_workflows, results)
    (results / "results.json").write_text(
        json.dumps({"model": "CompiledWorkflow", "bounded": True,
                    "jar_sha256": JAR_SHA256, "cases": cases}, indent=2) + "\n"
    )
    if args.write_examples:
        write_examples(results, cases)
    print("All secure configurations exhausted; all negative controls and witnesses verified.")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.SubprocessError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
