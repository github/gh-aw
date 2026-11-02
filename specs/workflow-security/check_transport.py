#!/usr/bin/env python3
"""Check safe-output transport/Git profiles, controls, and reachability."""

import argparse
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile

from check import JAR_SHA256, ROOT, check_result


FAULTS = {
    "unqualified-producer": "QualifiedRepository",
    "ambiguous-selection": "UnambiguousResolution",
    "blocking-open": "NonblockingOpen",
    "reopen-original": "StableConsumption",
    "rebaseline": "OriginalBaseline",
    "skip-publication-check": "PublicationIntegrity",
    "retry-integrity": "PermanentFailure",
    "cleanup-blocked": "CleanupDisposition",
    "cleanup-error-hidden": "CleanupDisposition",
    "unsafe-diagnostics": "SafeDiagnostics",
    "redundant-hashing": "BoundedLocalCost",
}
WITNESSES = [
    "NoSuccessfulWrite", "NoRejectedMutation", "NoRateLimitRetry",
    "NoCleanupFailure", "NoConflictCleanup",
]
GIT_FAULTS = {
    "implicit-recovery": "RecoveryOptIn",
    "optional-fetch": "NoOptionalMaterialization",
    "lazy-fetch": "NoOptionalMaterialization",
    "wrong-recovery-token": "RecoveryAuthentication",
    "wrong-metadata-token": "MetadataAuthentication",
    "unbounded-local": "BoundedLocalInspection",
    "unbounded-history": "BoundedHistory",
    "unbounded-comparisons": "BoundedComparisons",
    "retry-global-error": "NoGlobalFailureRetry",
    "comparison-retry": "NoComparisonRetries",
    "unbounded-request": "BoundedRequests",
    "ancestor-exemption": "ProtectionProvenance",
    "unsafe-diagnostics": "SafeDiagnostics",
}
GIT_WITNESSES = [
    "NoAncestorBaseline", "NoExactTipBaseline", "NoRecovery",
    "NoConservativeFallback", "NoSafePublication", "NoProtectedRejection",
    "NoMissingParentContinuation",
]
GENERATION_FAULTS = {
    "explicit-fetch": "LocalGeneration",
    "lazy-fetch": "LocalGeneration",
    "config-override": "LocalGeneration",
    "caller-override": "LocalGeneration",
    "token-forwarding": "NoGenerationAuthentication",
    "implicit-recovery": "LocalGeneration",
    "incremental-fallback": "IncrementalBaseline",
    "generation-fallback": "StableGenerationRange",
    "config-mutation": "ConfigurationPreserved",
    "environment-mutation": "EnvironmentPreserved",
    "hidden-missing": "MissingDataGuidance",
}
GENERATION_WITNESSES = [
    "NoLocalArtifact", "NoFullFallback", "NoMissingBaselineRejection",
    "NoMissingObjectRejection", "NoHostileConfigSuccess", "NoGenerationFailure",
]
RECOVERY_FAULTS = {
    "agent-fetch": "NoAgentNetwork",
    "copied-config": "IsolatedRecovery",
    "untrusted-remote": "AuthorizedRemote",
    "moving-baseline": "ExactBaseline",
    "hidden-preview": "ScannedBytes",
    "skip-policy": "PublicationPolicy",
}
RECOVERY_WITNESSES = ["NoRecoveryPublication", "NoFastPublication", "NoRejection"]


def recovery_cases():
    for mode in ["full", "incremental"]:
        yield f"secure-{mode}", mode, "none", None
    for fault, invariant in RECOVERY_FAULTS.items():
        yield fault, "incremental", fault, invariant
    for invariant in RECOVERY_WITNESSES:
        yield invariant, "incremental", "none", invariant


def generation_cases():
    for transport in ["patch", "bundle"]:
        for mode in ["full", "incremental"]:
            for allow in [False, True]:
                yield (f"secure-{transport}-{mode}-recovery-{allow}",
                       transport, mode, allow, "none", None)
        for fault, invariant in GENERATION_FAULTS.items():
            mode = "incremental" if fault == "incremental-fallback" else "full"
            yield (f"{transport}-{fault}", transport, mode,
                   fault == "implicit-recovery", fault, invariant)
        for invariant in GENERATION_WITNESSES:
            mode = "full" if invariant == "NoFullFallback" else "incremental"
            yield (f"{transport}-{invariant}", transport, mode, True, "none", invariant)


def git_cases():
    for allow in [False, True]:
        for shallow in [False, True]:
            yield (f"secure-recovery-{allow}-shallow-{shallow}", allow, shallow, "none", None)
    for fault, invariant in GIT_FAULTS.items():
        yield fault, fault == "wrong-recovery-token", True, fault, invariant
    for invariant in GIT_WITNESSES:
        yield invariant, invariant == "NoRecovery", True, "none", invariant


def run_case(jar, temporary, module, name, config, invariant):
    directory = Path(temporary) / name
    directory.mkdir()
    (directory / f"{module}.tla").write_text((ROOT / f"{module}.tla").read_text())
    (directory / "model.cfg").write_text(config)
    result = subprocess.run(
        [os.environ.get("JAVA_BIN", "java"), "-XX:+UseParallelGC",
         "-Xmx1g", "-cp", str(jar), "tlc2.TLC", "-workers", "1",
         "-seed", "1", "-fp", "0", "-config", "model.cfg",
         "-metadir", "states", f"{module}.tla"],
        cwd=directory, capture_output=True, text=True, timeout=120,
    )
    try:
        counts = check_result(result.returncode, result.stdout + result.stderr, invariant)
    except RuntimeError as error:
        raise RuntimeError(
            f"{name}: {error}\n{result.stdout}{result.stderr}"
        ) from error
    print(f"{name}: expected result ({counts['distinct']} distinct states)", flush=True)


def main(model="all"):
    jar_env = os.environ.get("TLA2TOOLS_JAR")
    if not jar_env:
        raise RuntimeError("Set TLA2TOOLS_JAR to the official v1.8.0 tla2tools.jar")
    jar = Path(jar_env).resolve()
    if not jar.is_file() or hashlib.sha256(jar.read_bytes()).hexdigest() != JAR_SHA256:
        raise RuntimeError("TLA2TOOLS_JAR is missing or does not match the pinned SHA-256")
    template = (ROOT / "SafeOutputTransport.cfg").read_text()
    cases = [("secure", "none", None)]
    cases += [(fault, fault, invariant) for fault, invariant in FAULTS.items()]
    cases += [(name, "none", name) for name in WITNESSES]
    with tempfile.TemporaryDirectory(prefix="gh-aw-transport-tlc-") as temporary:
        if model in {"all", "transport"}:
            for transport in ["patch", "bundle"]:
                for name, fault, invariant in cases:
                    config = template.replace('Transport = "patch"', f'Transport = "{transport}"')
                    config = config.replace('Fault = "none"', f'Fault = "{fault}"')
                    if name in WITNESSES:
                        config += f"\nINVARIANT {name}\n"
                    run_case(jar, temporary, "SafeOutputTransport", f"{transport}-{name}",
                             config, invariant)
        if model in {"all", "git"}:
            template = (ROOT / "SafeOutputGit.cfg").read_text()
            for name, allow, shallow, fault, invariant in git_cases():
                config = template.replace('Fault = "none"', f'Fault = "{fault}"')
                config = config.replace("AllowFullHistoryFetch = FALSE",
                                        f"AllowFullHistoryFetch = {str(allow).upper()}")
                config = config.replace("Shallow = TRUE", f"Shallow = {str(shallow).upper()}")
                if name in GIT_WITNESSES:
                    config += f"\nINVARIANT {name}\n"
                run_case(jar, temporary, "SafeOutputGit", f"git-{name}", config, invariant)
        if model in {"all", "generation"}:
            template = (ROOT / "SafeOutputGeneration.cfg").read_text()
            for name, transport, mode, allow, fault, invariant in generation_cases():
                config = template.replace('Fault = "none"', f'Fault = "{fault}"')
                config = config.replace('Transport = "patch"', f'Transport = "{transport}"')
                config = config.replace('Mode = "full"', f'Mode = "{mode}"')
                config = config.replace("AllowFullHistoryFetch = FALSE",
                                        f"AllowFullHistoryFetch = {str(allow).upper()}")
                if invariant in GENERATION_WITNESSES:
                    config += f"\nINVARIANT {invariant}\n"
                run_case(jar, temporary, "SafeOutputGeneration",
                         f"generation-{name}", config, invariant)
        if model in {"all", "recovery"}:
            template = (ROOT / "SafeOutputObjectRecovery.cfg").read_text()
            for name, mode, fault, invariant in recovery_cases():
                config = template.replace('Mode = "full"', f'Mode = "{mode}"')
                config = config.replace('Fault = "none"', f'Fault = "{fault}"')
                if invariant in RECOVERY_WITNESSES:
                    config += f"\nINVARIANT {invariant}\n"
                run_case(jar, temporary, "SafeOutputObjectRecovery",
                         f"object-recovery-{name}", config, invariant)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", choices=["all", "transport", "git", "generation", "recovery"],
                        default="all")
    main(parser.parse_args().model)
