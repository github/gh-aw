"""Compose the pinned GenAI/core registries with gh-aw's telemetry extensions."""

import re
import shutil
import sys
from pathlib import Path

import yaml
from opentelemetry.conformance import provision

GENAI_REF = "e6658f14c9d9ea0760825f029cb82e7673f48fab"


def prepare(output: Path) -> None:
    source = provision("open-telemetry/semantic-conventions-genai", GENAI_REF) / "model"
    if output.exists():
        shutil.rmtree(output)
    shutil.copytree(source, output)
    manifest_path = output / "manifest.yaml"
    manifest = yaml.safe_load(manifest_path.read_text())
    for dependency in manifest["dependencies"]:
        match = re.fullmatch(
            r"https://github.com/(open-telemetry/semantic-conventions)\.git@([^[]+)\[model\]",
            dependency["registry_path"],
        )
        if not match:
            raise ValueError(f"Unsupported pinned registry dependency: {dependency}")
        dependency["registry_path"] = str(
            (provision(match[1], match[2]) / "model").resolve()
        )
    manifest_path.write_text(yaml.safe_dump(manifest, sort_keys=False))
    shutil.copyfile(Path(__file__).with_name("extensions.yaml"), output / "gh-aw.yaml")


if __name__ == "__main__":
    prepare(Path(sys.argv[1]).resolve())
