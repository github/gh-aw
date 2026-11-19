#!/usr/bin/env python3
"""Generate a bounded, TLC-backed gateway threat model (no third-party Python packages)."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess


ROOT = Path(__file__).resolve().parent
REPO = ROOT.parent.parent
JAR_SHA256 = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88"
CONTROLS = [
    ("source-auth", "SourceAuthenticated", "Spoofed provider delivery",
     "Authenticate original provider evidence before durable admission.", "receiver"),
    ("source-binding", "SourceEnrolled", "Cross-tenant confused deputy",
     "Bind authenticated evidence to independently approved source enrollment.", "receiver"),
    ("predicates", "PredicatesMatched", "Unmatched event activates a workflow",
     "Evaluate complete original predicates before projection.", "receiver"),
    ("projection", "MinimalDispatch", "Native content or credentials leave ingress",
     "Persist and transmit only the declared minimal projection.", "receiver"),
    ("incomplete-scan", "CompleteDiscovery", "Incomplete inventory activates routes",
     "Publish only complete, validated repository snapshots.", "receiver"),
    ("stale-publication", "FencedPublication", "Slow refresh resurrects revoked routes",
     "Fence publication against approval and invalidation generations.", "receiver"),
    ("dispatch-recheck", "CurrentDispatchAuthorization", "Queued work dispatches after revocation",
     "Recheck current bindings at every local dispatch authorization point.", "receiver"),
    ("token-scope", "ScopedDispatchToken", "Overprivileged installation token",
     "Require the approved destination installation and minimum token scope.", "receiver"),
    ("worker-fence", "FencedWorker", "Stale worker transmits",
     "Durably fence competing worker leases before authorizing a send.", "receiver"),
    ("conflicting-replay", "ImmutableReceipt", "Same delivery identity overwrites content",
     "Bind the scoped receipt key to immutable authenticated content.", "receiver"),
    ("retry-uncertain", "AtMostOneAcceptance", "Lost response causes duplicate dispatch",
     "Quarantine uncertain outcomes instead of automatically retrying.", "receiver"),
    ("forget-receipt", "AtMostOneAcceptance", "Restart loses accepted-delivery tombstone",
     "Retain durable receipt and outcome records across restart.", "receiver"),
    ("app-provenance", "AppProvenance", "Human or unrelated App bypasses ingress",
     "Verify GitHub-owned original actor and the independently bound App/bot.", "destination"),
    ("oidc", "SignedAdmission", "Unverified OIDC identity gains continuation",
     "Verify issuer, signature, audience, time and signed run/job claims server-side.", "server"),
    ("hash", "HashBoundAdmission", "Stale workflow configuration gains continuation",
     "Require the signed audience hash to match independently verified configuration.", "server"),
    ("artifact", "ArtifactBoundAdmission", "Copied configuration hash blesses edited code",
     "Verify the actual artifact at the signed revision, not just its hash header.", "server"),
    ("response", "CorrelatedContinuation", "Wrong run or credential-bearing response continues",
     "Accept only the closed 200 response echoing every request field with allowed=true.", "destination"),
    ("envelope", "ValidatedActivation", "Malformed dispatch reaches the agent",
     "Validate the closed v2 envelope and compiled subscription before activation.", "destination"),
]
WITNESSES = ["NoAgent", "NoUncertainty", "NoKnownRetry", "NoDuplicate", "NoConflict", "NoCrash"]
INPUTS = [
    ROOT / "WebhookGateway.tla", ROOT / "WebhookGateway.cfg", Path(__file__),
    REPO / "docs/src/content/docs/specs/webhook-gateway-specification.md",
    *[REPO / "actions/setup/js" / name for name in [
        "check_gateway_provenance.cjs", "check_gateway_admission.cjs",
        "validate_webhook.cjs", "webhook_context.cjs",
    ]],
]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_hashes():
    return {str(path.relative_to(REPO)): digest(path) for path in INPUTS}


def check_result(status, output, expected=None):
    counts = re.search(
        r"(\d+) states generated, (\d+) distinct states found, (\d+) states left on queue\.",
        output,
    )
    violations = re.findall(r"Invariant (\w+) is violated\.", output)
    if expected is None:
        valid = (status == 0 and not violations
                 and "Model checking completed. No error has been found." in output
                 and counts is not None and counts[3] == "0")
    else:
        valid = (status == 12 and violations == [expected]
                 and re.search(r"^State 2: <", output, re.MULTILINE) is not None)
    if not valid or counts is None:
        raise RuntimeError(f"Unexpected TLC result: exit={status}, expected={expected!r}\n{output}")
    actions = re.findall(r"^State \d+: <([^>]+)>", output, re.MULTILINE)
    return {
        "generated": int(counts[1]), "distinct": int(counts[2]),
        "remaining": int(counts[3]),
        "trace": [action.split(" line ", 1)[0] for action in actions],
    }


def render_report(evidence):
    secure = evidence["cases"][0]
    lines = [
        "# Webhook gateway security threat model",
        "",
        "Generated by `check.py` from pinned TLC runs. Threat descriptions are human-authored; "
        + "TLC supplies bounded invariant checks and counterexample traces, not automatic "
        + "security discovery or a proof of implementation conformance.",
        "",
        "## Assets, adversaries and trust boundaries",
        "",
        "Assets are source confidentiality, dispatch authority, approved workflow configuration, "
        + "durable replay state, and runtime continuation. Adversaries include unauthenticated "
        + "senders, authenticated but unenrolled tenants, payload authors, unrelated Apps/human "
        + "dispatchers, and stale/concurrent workers. Network failure and restart are nonmalicious "
        + "faults with security consequences. Protected enrollment, GitHub-owned context, "
        + "provider/JWT verification, the compiler and durable storage primitives are trusted.",
        "",
        "The modeled path is provider -> authenticated enrollment/predicate evaluation -> "
        + "durable receipt and immutable projection -> fenced dispatch authorization -> GitHub "
        + "Actions -> App provenance -> server OIDC/hash/artifact admission -> correlated "
        + "continuation -> local envelope validation -> agent. Discovery is a separate, "
        + "generation-fenced control plane. Resource URLs and subscription selectors confer "
        + "no provider-read or dispatch authority.",
        "",
        "## Checked scope and result",
        "",
        f"TLC 2.19, official TLA+ Tools v1.7.4, SHA-256 `{JAR_SHA256}`. "
        + f"The secure model exhausted **{secure['distinct']:,} distinct states** "
        + f"({secure['generated']:,} generated; zero queued) with all 18 invariants satisfied.",
        "",
        "Bounds: 13 request profiles; one source/destination/subscription receipt key; two "
        + "possible content fingerprints represented by original/conflicting receipt actions; "
        + "two workers and fencing generations; at most two dispatch attempts; one irreversible "
        + "approval revocation and one restart. The initial table represents an independently "
        + "verified snapshot, except for the incomplete-discovery profile. A scan can overlap "
        + "revocation. Execution-version, signature, projection and response validity are "
        + "finite predicates, not implementations of parsers or cryptography.",
        "",
        "No fairness or liveness property is asserted. Terminal, denied and quarantined states "
        + "are intentional; deadlock checking is disabled. Counters track GitHub acceptance, "
        + "not successful workflow execution. The at-most-one-acceptance invariant depends on "
        + "quarantining every uncertain outcome and does not provide exactly-once delivery "
        + "or reconcile quarantined work. Every transmit represents a durably recorded attempt.",
        "",
        "## Threats and control sensitivity",
        "",
        "Each row disables exactly one modeled protection. An expected invariant failure "
        + "demonstrates control sensitivity, **not an exploitable defect in current code**. "
        + "Receiver and server rows describe external integration requirements; no such "
        + "service ships in this repository. Destination rows abstract shipped checks.",
        "",
        "| Control mutation | Threat | Required mitigation | Boundary | Expected violated invariant |",
        "| --- | --- | --- | --- | --- |",
    ]
    for fault, invariant, threat, mitigation, boundary in CONTROLS:
        lines.append(f"| `{fault}` | {threat} | {mitigation} | {boundary} | `{invariant}` |")
    lines += ["", "## TLC counterexamples", "",
              "Action sequences below are extracted from TLC's breadth-first counterexamples. "
              + "Full states, effective configs and complete logs are retained in the chosen "
              + "results directory; the adjacent JSON records their hashes and exact commands.", ""]
    for case in evidence["cases"][1:1 + len(CONTROLS)]:
        lines.append(f"- `{case['name']}`: " + " -> ".join(f"`{a}`" for a in case["trace"]) + ".")
    lines += [
        "", "## Nonvacuity witnesses", "",
        "The secure model also deliberately checks the negation of reachable behavior. "
        + "These are expected witness failures while all security invariants remain enabled.", "",
        "| Witness invariant | Meaning | Distinct states explored before witness |",
        "| --- | --- | --- |",
    ]
    meanings = [
        "A valid delivery reaches the agent.",
        "A transmitted dispatch can become uncertain.",
        "Known nonacceptance permits a second attempt that is accepted.",
        "A duplicate receipt is recognized without new admission.",
        "A conflicting duplicate is rejected without replacing its content.",
        "Restart is reachable without erasing durable state.",
    ]
    for case, meaning in zip(evidence["cases"][1 + len(CONTROLS):], meanings):
        lines.append(f"| `{case['name']}` | {meaning} | {case['distinct']:,} |")
    lines += [
        "", "## Implementation mapping and residual risk", "",
        "The normative mapping is the linked gateway specification's discovery, durability, "
        + "App provenance, getToken admission and envelope sections. Shipped destination checks "
        + "are `verifyGatewayProvenance` in `actions/setup/js/check_gateway_provenance.cjs`, "
        + "`verifyGatewayAdmission` in `check_gateway_admission.cjs`, `validateWebhook` in "
        + "`validate_webhook.cjs`, and `normalizeWebhookContext` in `webhook_context.cjs`. "
        + "Source hashes are observations, not a refinement proof or runtime regression test.",
        "",
        "The source-authentication predicate assumes a reviewed profile binds native "
        + "discriminators and stable delivery identity to authenticated evidence. HMAC over "
        + "body bytes alone need not authenticate event/delivery headers. This model does not "
        + "establish retry stability, multi-tenant namespace uniqueness, a safe tombstone "
        + "retention horizon, or collision resistance. Batch-local URL/item deduplication "
        + "cannot replace durable delivery replay protection.",
        "",
        "Known revocation prevents later local authorization; it cannot recall an already "
        + "authorized/transmitted request or an issued continuation. The model stores checks "
        + "at their ordering point rather than incorrectly requiring approvals to remain "
        + "true forever. GitHub selects the execution revision; discovery does not pin it. "
        + "Exact signed-revision verification and explicit older-attempt policy remain "
        + "server responsibilities; full refresh reconciliation and revision rollback "
        + "behavior are not modeled.",
        "",
        "Unmodeled risks requiring integration tests/review: real signature/JWKS and "
        + "canonical-URL parsing; exact byte/numeric/predicate budgets; multi-delivery batching "
        + "and source-isolation keys; repository transfers and listener collisions; stale "
        + "cache expiry and unavailable discovery; resource exhaustion/DoS; SSRF/redirect "
        + "and credential leakage; provider read authorization; adversarial prompt content; "
        + "cleanup/revocation of runtime-minted tokens; compromised central/App credentials; "
        + "arbitrary custom steps and abrupt runner termination. An approved App identifies "
        + "a principal, not a tenant or CAO process. A configuration hash is not a whole-file "
        + "digest. A GitHub 204 proves acceptance only. None of these omissions is evidence "
        + "of safety or a discovered implementation vulnerability.",
        "",
        "Reproduce with the commands in [README.md](README.md). "
        + "Measured input hashes, Java identity, run time, per-case verdicts, exit statuses "
        + "and log hashes are in [threat-model.json](threat-model.json).", "",
    ]
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--results", type=Path, required=True, help="New directory for full TLC evidence")
    parser.add_argument("--report", type=Path, help="Write generated Markdown and adjacent JSON")
    args = parser.parse_args()
    jar_env = os.environ.get("TLA2TOOLS_JAR")
    if not jar_env:
        raise RuntimeError("Set TLA2TOOLS_JAR to the official v1.7.4 jar")
    jar = Path(jar_env).resolve()
    if digest(jar) != JAR_SHA256:
        raise RuntimeError("TLA2TOOLS_JAR does not match the repository's pinned SHA-256")
    java = os.environ.get("JAVA_BIN", "java")
    version = subprocess.run([java, "-version"], capture_output=True, text=True, check=True)
    results = args.results.resolve()
    results.mkdir(parents=True, exist_ok=False)
    hashes = source_hashes()
    evidence = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "jar_sha256": JAR_SHA256, "java": (version.stdout + version.stderr).strip(),
        "source_sha256": hashes, "cases": [],
    }
    template = (ROOT / "WebhookGateway.cfg").read_text()
    cases = [("secure", "none", None, "exhaustive")]
    cases += [(fault, fault, invariant, "negative-control")
              for fault, invariant, *_ in CONTROLS]
    cases += [(invariant, "none", invariant, "witness") for invariant in WITNESSES]
    for name, fault, expected, kind in cases:
        directory = results / name
        directory.mkdir()
        shutil.copyfile(ROOT / "WebhookGateway.tla", directory / "WebhookGateway.tla")
        config = template.replace('Fault = "none"', f'Fault = "{fault}"')
        if kind == "witness":
            config += f"\nINVARIANT {expected}\n"
        (directory / "model.cfg").write_text(config)
        command = [
            java, "-XX:+UseParallelGC", "-Xmx1g", "-cp", str(jar),
            "tlc2.TLC", "-workers", "1", "-seed", "1", "-fp", "0",
            "-config", "model.cfg", "-metadir", "states", "WebhookGateway.tla",
        ]
        result = subprocess.run(command, cwd=directory, capture_output=True, text=True, timeout=120)
        output = result.stdout + result.stderr
        log = directory / "tlc.log"
        log.write_text(output)
        counts = check_result(result.returncode, output, expected)
        evidence["cases"].append({
            "name": name, "kind": kind, "expected_invariant": expected,
            "exit_status": result.returncode, **counts,
            "cwd": str(directory), "command": command,
            "config_sha256": digest(directory / "model.cfg"),
            "log_sha256": digest(log),
        })
        print(f"{name}: {kind}, {counts['distinct']} distinct states", flush=True)
    if source_hashes() != hashes:
        raise RuntimeError("Measured inputs changed during checking; rerun before publishing evidence")
    serialized = json.dumps(evidence, indent=2) + "\n"
    report = render_report(evidence)
    (results / "verification.json").write_text(serialized)
    (results / "threat-model.md").write_text(report)
    if args.report:
        args.report.write_text(report)
        args.report.with_suffix(".json").write_text(serialized)


if __name__ == "__main__":
    main()
