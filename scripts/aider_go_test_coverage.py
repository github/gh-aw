"""Discover a small coverage gap and publish only a verified Go test improvement."""

import argparse
import json
import re
import subprocess
import tempfile
from pathlib import Path


def run(*args):
    return subprocess.run(
        args, check=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        timeout=180,
    ).stdout


def coverage(path):
    blocks = {}
    for line in path.read_text().splitlines()[1:]:
        location, statements, count = line.rsplit(" ", 2)
        blocks[location] = (int(statements), int(count) > 0)
    return blocks


def covered(blocks):
    return sum(statements for statements, hit in blocks.values() if hit)


def measure(package, profile):
    output = run("go", "test", "-count=1", "-timeout=90s", "-coverprofile="+str(profile), package)
    print(output, end="")
    return coverage(profile)


def pending_pr():
    pulls = json.loads(run(
        "gh", "pr", "list", "--state", "open", "--limit", "1",
        "--search", '"gh-aw-tracker-id: daily-go-test-stubs-aider" in:body',
        "--json", "number,url",
    ))
    return bool(pulls)


def discover(state):
    if run("git", "diff", "--name-only", "HEAD").strip():
        raise ValueError("Tracked workspace changes exist before discovery")
    state.mkdir(parents=True, exist_ok=True)
    metadata = state / "target.json"
    if metadata.exists():
        raise ValueError("Discovery already ran; do not replace the baseline")
    if pending_pr():
        metadata.write_text(json.dumps({"noop": True}))
        print("NO_CANDIDATE: A coverage PR is already awaiting review; do not create another.")
        return
    # Prefer recently edited, small library packages over tool entry points.
    recent = run("git", "log", "-50", "--format=", "--name-only", "--", "pkg")
    directories = list(dict.fromkeys(str(Path(p).parent) for p in recent.splitlines()
                                    if p.endswith(".go") and not p.endswith("_test.go")))
    listing = run("go", "list", "-f", '{{.ImportPath}}|{{.Dir}}|{{join .GoFiles ","}}', "./pkg/...")
    candidates = []
    root = Path.cwd().resolve()
    for row in listing.splitlines():
        package, directory, names = row.split("|")
        directory = Path(directory).resolve()
        relative = directory.relative_to(root).as_posix()
        if not relative.startswith("pkg/"):
            raise ValueError("Package is outside pkg/")
        files = [directory / name for name in names.split(",") if name]
        target = directory / "aider_coverage_test.go"
        if not files or len(files) > 6 or target.exists():
            continue
        if any("Code generated" in p.read_text() for p in files):
            continue
        if sum(p.stat().st_size for p in files) > 20000:
            continue
        rank = directories.index(relative) if relative in directories else len(directories)
        candidates.append((rank, package, directory, files))
    for _, package, directory, files in sorted(candidates)[:3]:
        baseline = measure(package, state / "before.out")
        if not baseline or covered(baseline) == sum(n for n, _ in baseline.values()):
            continue
        target = (directory / "aider_coverage_test.go").relative_to(root).as_posix()
        metadata.write_text(json.dumps({"package": package, "file": target}))
        print("TARGET:", target)
        print(run("go", "tool", "cover", "-func="+str(state / "before.out")))
        for path in files:
            print("\nSOURCE:", path.relative_to(root))
            print(path.read_text())
        for path in sorted(directory.glob("*_test.go"))[:2]:
            print("\nEXISTING TEST:", path.relative_to(root))
            print(path.read_text()[:10000])
        return
    metadata.write_text(json.dumps({"noop": True}))
    print("NO_CANDIDATE: No bounded coverage gap found in the three inspected packages.")


def mutation_witness(state, target, text, before, after, package):
    names = re.findall(r"\bfunc\s+(Test[A-Z]\w*)\s*\(", text)
    if not names:
        raise ValueError("No runnable behavioral tests found")
    selector = "^("+"|".join(names)+")$"
    # Only the new tests may supply the failure witness, not existing tests.
    run("go", "test", "-count=1", "-timeout=90s", "-run", selector, package)
    attempts = 0
    for location, (_, hit) in before.items():
        if hit or not after[location][1]:
            continue
        filename, coordinates = location.rsplit(":", 1)
        first, last = (int(point.split(".")[0]) for point in coordinates.split(","))
        source = target.parent / Path(filename).name
        original = source.read_text()
        lines = original.splitlines(keepends=True)
        segment = "".join(lines[first-1:last])
        literal = re.search(r'\breturn[ \t]+(true|false|-?\d+|"(?:[^"\\]|\\.)*")(?=[ \t]*[,;\n}])', segment)
        if not literal:
            continue
        value = literal[1]
        replacement = ({"true": "false", "false": "true"}.get(value)
                       or (json.dumps("gh-aw-mutated-return") if value.startswith('"') else str(int(value)+1)))
        start = sum(len(line) for line in lines[:first-1]) + literal.start(1)
        changed = original[:start] + replacement + original[start+len(value):]
        # A Go overlay changes compiler input, never production files on disk.
        with tempfile.TemporaryDirectory(prefix="mutation-", dir=state) as directory:
            directory = Path(directory)
            mutant = directory / source.name
            mutant.write_text(changed)
            overlay = directory / "overlay.json"
            overlay.write_text(json.dumps({"Replace": {str(source.resolve()): str(mutant)}}))
            try:
                run("go", "test", "-json", "-count=1", "-timeout=90s",
                    "-overlay="+str(overlay), "-run", selector, package)
            except subprocess.CalledProcessError as error:
                events = [json.loads(line) for line in error.stdout.splitlines() if line.startswith("{")]
                failures = {event["Test"].split("/")[0] for event in events
                            if event.get("Action") == "fail" and event.get("Test")}
                output = "".join(event.get("Output", "") for event in events)
                assertion = re.search(re.escape(target.name)+r":\d+(?::|\s*\n)", output)
                if failures.intersection(names) and assertion and "panic:" not in output:
                    return str(source), first
        attempts += 1
        if attempts == 3:
            break
    raise ValueError("No mutation witness: new tests must detect an incorrect production return value")


def verify(state):
    metadata = json.loads((state / "target.json").read_text())
    if metadata.get("noop"):
        raise ValueError("No candidate was selected; publication is forbidden")
    target = Path(metadata["file"])
    root = Path.cwd().resolve()
    if (target.is_absolute() or ".." in target.parts or not target.parts
            or target.parts[0] != "pkg" or target.name != "aider_coverage_test.go"
            or target.is_symlink() or not target.resolve().is_relative_to(root / "pkg")):
        raise ValueError("Invalid test target")
    if run("git", "ls-files", "--", str(target)).strip():
        raise ValueError("Only a new test file may be published")
    if run("git", "diff", "--name-only", "HEAD").strip():
        raise ValueError("Existing tracked files were changed")
    untracked = run("git", "ls-files", "--others", "--exclude-standard", "-z").split("\0")
    for name in filter(None, untracked):
        if name != str(target) and not name.startswith((".aider/", ".aider.")):
            raise ValueError("Unrelated untracked file: "+name)
    text = target.read_text()
    if re.search(r"\b(?:Skip|Skipf|SkipNow|TestPlaceholder_TODO)\b|//go:build|// \+build", text):
        raise ValueError("Skipped, placeholder, or build-excluded tests are forbidden")
    if not re.search(r"\b(?:Fatal|Fatalf|Error|Errorf)\s*\(|\b(?:assert|require)\.\w+\s*\(", text):
        raise ValueError("Tests must contain behavioral assertions")
    run("gofmt", "-w", str(target))
    before = coverage(state / "before.out")
    after = measure(metadata["package"], state / "after.out")
    if not before or before.keys() != after.keys() or any(
        after[key][0] != count or (hit and not after[key][1])
        for key, (count, hit) in before.items()
    ):
        raise ValueError("Production coverage blocks changed or existing coverage regressed")
    gain = covered(after) - covered(before)
    if gain <= 0:
        raise ValueError("No additional production statements covered")
    source, line = mutation_witness(state, target, text, before, after, metadata["package"])
    print(f"Mutation witness: new tests reject a changed return in {source}:{line}")
    total = sum(n for n, _ in before.values())
    return target, metadata["package"], covered(before), covered(after), total


def publish(state):
    if pending_pr():
        raise ValueError("A coverage PR is already awaiting review")
    target, package, before, after, total = verify(state)
    branch = "aider-go-coverage-"+run("git", "rev-parse", "--short", "HEAD").strip()
    body = (
        f"Add assertion-based tests for `{package}`.\n\n"
        f"Covered production statements: **{before}/{total} -> {after}/{total}** "
        f"({before/total:.1%} -> {after/total:.1%}); **+{after-before} statements**.\n\n"
        f"Validation: `go test -count=1 -timeout=90s -coverprofile=... {package}`. "
        "The new tests also reject a deliberately changed production return value "
        "through a temporary Go overlay; production files are never modified. "
        "Existing coverage is preserved; only the new test file is included."
    )
    run("git", "checkout", "-b", branch)
    run("git", "add", "--", str(target))
    staged = run("git", "diff", "--cached", "--name-only").splitlines()
    if staged != [str(target)]:
        raise ValueError("Staged changes are not exactly the verified test file")
    run("git", "commit", "-m", "Add behavioral coverage for "+package)
    print(run("safeoutputs", "create_pull_request",
              "--title", "Add behavioral coverage for "+package,
              "--body", body, "--branch", branch), end="")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("discover", "verify", "publish"))
    parser.add_argument("--state-dir", type=Path, default=Path("/tmp/gh-aw/agent/aider-coverage"))
    args = parser.parse_args()
    try:
        if args.action == "verify":
            print(verify(args.state_dir))
        else:
            {"discover": discover, "publish": publish}[args.action](args.state_dir)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        parser.exit(1, f"Coverage improvement failed: {error}\n"
                    + (error.stderr if isinstance(error, subprocess.CalledProcessError) else ""))


if __name__ == "__main__":
    main()
