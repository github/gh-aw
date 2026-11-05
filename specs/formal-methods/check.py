#!/usr/bin/env python3
"""Verify the counter contracts and negative controls with pinned external tools."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parent
CONTRACT = """let advance (limit:pos) (n:nat{n < limit})
  : Tot (r:nat{r == n + 1 /\\ r <= limit}) ="""


def check_contract(path):
    source = path.read_text()
    # This deliberately narrow tutorial accepts a body repair, not a new contract.
    prefix = f"module {path.stem}\n\n{CONTRACT}\n"
    if not source.startswith(prefix):
        raise ValueError("The original module and acceptance contract must be preserved")
    body = source[len(prefix):]
    if not re.fullmatch(r"[ \t\n()n+0-9]+", body):
        raise ValueError("Only a pure arithmetic body repair is accepted in this example")


def run(command, results, name, expected=0, diagnostic=None):
    record = {"command": command, "expected_exit": expected}
    try:
        completed = subprocess.run(
            command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            text=True, timeout=120, check=False,
        )
        output = completed.stdout
        record["exit_status"] = completed.returncode
    except (OSError, subprocess.TimeoutExpired) as error:
        partial = getattr(error, "stdout", None) or ""
        if isinstance(partial, bytes):
            partial = partial.decode(errors="replace")
        output = partial + "\n" + str(error)
        record["error"] = type(error).__name__
        (results / f"{name}.log").write_text(output)
        (results / f"{name}.json").write_text(json.dumps(record, indent=2))
        raise RuntimeError(f"{name}: tool failure or timeout, not verification evidence") from error
    (results / f"{name}.log").write_text(output)
    (results / f"{name}.json").write_text(json.dumps(record, indent=2))
    inconclusive = re.search(r"\b(unknown|timeout|timed out|canceled|cancelled)\b", output, re.I)
    if completed.returncode != expected or inconclusive or (diagnostic and diagnostic not in output):
        raise RuntimeError(f"{name}: unexpected verifier result; see {results / (name + '.log')}")
    return output


def verify(results, repair=None, tla=False):
    results.mkdir(parents=True, exist_ok=False)
    for path in ROOT.glob("*.fst"):
        check_contract(path)
    sources = list(ROOT.glob("*.fst")) + list(ROOT.glob("*.tla")) + list(ROOT.glob("*.cfg"))
    sources += [Path(__file__)]
    if repair:
        check_contract(repair)
        sources.append(repair)
    manifest = {
        str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in sources
    }
    (results / "inputs.json").write_text(json.dumps(manifest, indent=2))
    fstar = os.environ["FSTAR_BIN"]
    z3 = os.environ["FSTAR_Z3"]
    tool_hashes = {
        str(Path(tool).resolve()): hashlib.sha256(Path(tool).read_bytes()).hexdigest()
        for tool in [fstar, z3]
    }
    if tla:
        jar = Path(os.environ["TLA2TOOLS_JAR"])
        tool_hashes[str(jar.resolve())] = hashlib.sha256(jar.read_bytes()).hexdigest()
    (results / "tools.json").write_text(json.dumps(tool_hashes, indent=2))
    run(["git", "-C", str(ROOT), "rev-parse", "HEAD"], results, "revision")
    run([fstar, "--version"], results, "fstar-version")
    run([z3, "-version"], results, "z3-version")
    options = [
        fstar, "--smt", z3, "--z3version", "5.3.0", "--cache_off",
        "--z3rlimit", "50", "--message_format", "human",
    ]
    with tempfile.TemporaryDirectory(prefix="fstar-check-") as work:
        for path, negative in [(ROOT / "Counter.fst", False), (ROOT / "CounterBroken.fst", True)]:
            command = options + [str(path)]
            run(command, results, path.stem, expected=1 if negative else 0,
                diagnostic="Failed to prove: n + 2 == n + 1" if negative
                else "All verification conditions discharged successfully")
        repaired = Path(work) / "CounterBroken.fst"
        repaired.write_text((ROOT / "CounterBroken.fst").read_text().replace("n + 2\n", "n + 1\n"))
        check_contract(repaired)
        (results / "CounterRepaired.fst").write_text(repaired.read_text())
        run(options + [str(repaired)], results, "reference-repair",
            diagnostic="All verification conditions discharged successfully")
        if repair:
            run(options + [str(repair)], results, "repaired",
                diagnostic="All verification conditions discharged successfully")
    if tla:
        java = os.environ["JAVA_BIN"]
        jar = os.environ["TLA2TOOLS_JAR"]
        run([java, "-version"], results, "java-version")
        # TLC can generate trace specifications beside its input module.
        for name in ["Counter.tla", "Counter.cfg", "CounterNegative.cfg"]:
            (results / name).write_bytes((ROOT / name).read_bytes())
        for config, expected, diagnostic in [
            ("Counter.cfg", 0, "Model checking completed. No error has been found."),
            ("CounterNegative.cfg", 12, "Invariant NeverAtLimit is violated."),
        ]:
            run([java, "-cp", jar, "tlc2.TLC", "-workers", "1",
                 "-config", str(results / config),
                 "-metadir", str(results / config.removesuffix(".cfg")),
                 str(results / "Counter.tla")],
                results, config, expected=expected, diagnostic=diagnostic)
    print(f"Verified counter contracts and expected negative controls; evidence: {results}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--results", required=True, type=Path, help="New evidence directory")
    parser.add_argument("--repair", type=Path, help="Repaired CounterBroken.fst with unchanged contract")
    parser.add_argument("--tla", action="store_true", help="Also run the complementary TLC checks")
    args = parser.parse_args()
    verify(args.results.resolve(), args.repair.resolve() if args.repair else None, args.tla)
