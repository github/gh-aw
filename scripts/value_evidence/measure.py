"""Data producers for trusted default-branch Git snapshots (never called by gh aw value)."""

import collections
import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess


MODULE = "github.com/github/gh-aw"
TOOLCHAIN = "go1.27.4"
ANALYZER = "v0.52.0"
FIXTURE = Path("pkg/workflow/compiler_performance_benchmark_test.go")
PROTECTED = {"containsInNonCommentLines", "indexInNonCommentLines", "extractJobSection"}
TARGETS = ["./cmd/...", "./internal/tools/..."]


class Unavailable(ValueError):
    pass


def command(args, cwd, **overrides):
    env = dict(os.environ, GOTOOLCHAIN="local", GOWORK="off", CGO_ENABLED="0",
               GOFLAGS="-mod=readonly -buildvcs=false", GIT_NO_LAZY_FETCH="1", GIT_NO_REPLACE_OBJECTS="1")
    env.update(overrides)
    try:
        p = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True, timeout=600)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise Unavailable(f"{args[0]} could not complete: {exc}") from exc
    if p.returncode:
        raise Unavailable(f"{Path(args[0]).name} {args[1]} exited {p.returncode}: {(p.stderr or p.stdout)[-500:].strip()}")
    return p.stdout


def eligible_functions(root, packages):
    # A successful v0.52.0 empty inventory is the literal JSON null.
    if packages is None:
        return []
    if not isinstance(packages, list):
        raise Unavailable("expected the pinned analyzer's package array")
    candidates = {}
    for package in packages:
        pkg = package["Path"]
        if not (pkg == MODULE or pkg.startswith(MODULE + "/")):
            raise Unavailable("analyzer returned a package outside the selected module")
        for fn in package["Funcs"]:
            name, pos = fn["Name"], fn["Position"]
            if not re.fullmatch(r"[a-z][A-Za-z0-9_]*", name) or name in PROTECTED or fn["Generated"]:
                continue
            path = root / pos["File"]
            if not path.resolve().is_relative_to(root.resolve()) or path.is_symlink() or not path.is_file():
                raise Unavailable("analyzer candidate has no in-checkout source file")
            source = path.read_bytes()
            if b"//go:embed" not in source and b"//go:linkname" not in source:
                candidates[f"{pkg}.{name}"] = name
    counts = collections.Counter()
    names = set(candidates.values())
    if names:
        for path in root.rglob("*.go"):
            if path.name.endswith("_test.go"):
                continue
            if path.is_symlink():
                raise Unavailable("non-test Go source contains a symlink")
            counts.update(n for n in re.findall(r"[A-Za-z_][A-Za-z0-9_]*", path.read_text()) if n in names)
    return sorted(key for key, name in candidates.items() if counts[name] == 1)


def deadcode(root, binary):
    packages = command(["go", "list", "-f", "{{.Name}} {{.ImportPath}}", *TARGETS], root).splitlines()
    for prefix in ("cmd/", "internal/tools/"):
        if not any(pkg.startswith(f"main {MODULE}/{prefix}") for pkg in packages):
            raise Unavailable("both cmd and internal/tools must contribute main packages")
    command(["go", "build", "./..."], root)
    command(["go", "vet", "./..."], root)
    command(["go", "build", "-o", os.devnull, "./cmd/gh-aw-wasm"], root, GOOS="js", GOARCH="wasm")
    data = command([binary, "-json", "-filter", r"^github\.com/github/gh-aw(/|$)", *TARGETS], root)
    return {"candidate_ids": eligible_functions(root, json.loads(data)), "main_packages": sorted(packages)}


def benchmark(root, fixture_digest):
    fixture = hashlib.sha256((root / FIXTURE).read_bytes()).hexdigest()
    if fixture != fixture_digest:
        raise Unavailable("benchmark fixture differs from the frozen error-checking fixture")
    data = command(["go", "test", "./pkg/workflow", "-run", "^$", "-bench", "^BenchmarkCompileSimpleWorkflow$",
                    "-benchtime=1s", "-count=5", "-cpu=1"], root)
    rows = [line.split() for line in data.splitlines() if line.startswith("BenchmarkCompileSimpleWorkflow")]
    values = []
    for row in rows:
        if (len(row) < 4 or not re.fullmatch(r"BenchmarkCompileSimpleWorkflow(?:-1)?", row[0])
                or not row[1].isdigit() or int(row[1]) <= 0 or row[3] != "ns/op"):
            raise Unavailable("unexpected benchmark record")
        values.append(float(row[2]))
    if len(values) != 5 or any(not math.isfinite(v) or v <= 0 for v in values):
        raise Unavailable("five successful, finite, positive benchmark replicates are required")
    return {"ns_per_op": values, "fixture_sha256": fixture}
