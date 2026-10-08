#!/usr/bin/env python3
"""Compare the closed queue schemas with sealed official TypeSpec JSON output.

This is a fail-closed equivalence check for the contract's schema subset, not
a general JSON Schema equivalence solver. References and annotations are
resolved before comparing validation constraints, including custom extensions.
"""
import argparse
import json
from pathlib import Path
import sys
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[2]
CUSTOM = ROOT / "pkg/workqueue/schema"
ANNOTATIONS = {"$schema", "$id", "$defs", "title", "description", "$comment"}
EXTENSIONS = {
    "x-utf8-max-bytes", "x-forbid-ascii-controls", "x-key-utf8-max-bytes",
    "x-key-min-utf8-bytes", "x-forbid-key-ascii-controls",
}
KEYWORDS = {
    "type", "const", "enum", "properties", "required", "additionalProperties",
    "unevaluatedProperties", "items", "anyOf", "oneOf", "minimum", "maximum",
    "minLength", "maxLength", "minItems", "maxItems", "uniqueItems", "pattern",
} | EXTENSIONS
LOWER_BOUNDS = {"minimum", "minLength", "minItems", "x-key-min-utf8-bytes"}
UPPER_BOUNDS = {"maximum", "maxLength", "maxItems", "x-utf8-max-bytes", "x-key-utf8-max-bytes"}


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def no_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate schema key: " + key)
        result[key] = value
    return result


def conjoin(left, right):
    if left is False or right is False:
        return False
    result = dict(left)
    for key, value in right.items():
        if key not in result or result[key] == value:
            result[key] = value
        elif key in LOWER_BOUNDS:
            result[key] = max(result[key], value)
        elif key in UPPER_BOUNDS:
            result[key] = min(result[key], value)
        elif key == "type" and {result[key], value} == {"integer", "number"}:
            result[key] = "integer"
        else:
            raise ValueError("unsupported intersecting reference constraint: " + key)
    return result


def constant_has_type(value, expected):
    if expected == "string":
        return isinstance(value, str)
    if expected == "boolean":
        return isinstance(value, bool)
    if expected == "null":
        return value is None
    if expected in {"integer", "number"}:
        return type(value) is int or expected == "number" and type(value) is float
    if expected == "array":
        return isinstance(value, list)
    if expected == "object":
        return isinstance(value, dict)
    raise ValueError("unsupported schema type: " + str(expected))


class SchemaSet:
    def __init__(self, directory):
        self.directory = Path(directory).resolve()
        self.documents = {
            path.name: json.loads(path.read_text(), object_pairs_hook=no_duplicate_keys)
            for path in sorted(self.directory.glob("*.json"))
        }
        if not self.documents:
            raise ValueError("no JSON schemas in " + str(self.directory))
        for name, document in self.documents.items():
            if document.get("$schema") != "https://json-schema.org/draft/2020-12/schema":
                raise ValueError(name + ": expected JSON Schema draft 2020-12")

    def normalize(self, schema, document, stack=()):
        if schema is True:
            return {}
        if schema is False or schema == {"not": {}}:
            return False
        if not isinstance(schema, dict):
            raise ValueError("expected a schema object or boolean")
        if "$ref" in schema:
            reference = schema["$ref"]
            filename, separator, pointer = reference.partition("#")
            filename = filename or document
            if filename not in self.documents:
                raise ValueError("unsupported or missing schema reference: " + reference)
            identity = (filename, pointer)
            if identity in stack:
                raise ValueError("recursive schema reference requires separate proof: " + reference)
            target = self.documents[filename]
            if separator and pointer:
                if not pointer.startswith("/"):
                    raise ValueError("unsupported schema anchor: " + reference)
                for segment in pointer[1:].split("/"):
                    target = target[unquote(segment).replace("~1", "/").replace("~0", "~")]
            resolved = self.normalize(target, filename, stack + (identity,))
            siblings = {key: value for key, value in schema.items() if key != "$ref"}
            return conjoin(resolved, self.normalize(siblings, document, stack))
        unknown = set(schema) - KEYWORDS - ANNOTATIONS
        if unknown:
            raise ValueError("unsupported schema keywords: " + ", ".join(sorted(unknown)))
        result = {}
        for key, value in schema.items():
            if key in ANNOTATIONS:
                continue
            if key == "properties":
                result[key] = {name: self.normalize(child, document, stack) for name, child in value.items()}
            elif key in {"items", "additionalProperties", "unevaluatedProperties"}:
                result[key] = self.normalize(value, document, stack)
            elif key in {"anyOf", "oneOf"}:
                result[key] = sorted(
                    [self.normalize(child, document, stack) for child in value], key=canonical)
            elif key in {"required", "enum"}:
                result[key] = sorted(value, key=canonical)
            else:
                result[key] = value
        if "unevaluatedProperties" in result:
            if result.get("type") != "object" or any(
                    key in result for key in ("anyOf", "oneOf", "additionalProperties")):
                raise ValueError("unevaluatedProperties equivalence requires a plain object")
            result["additionalProperties"] = result.pop("unevaluatedProperties")
        if result.get("properties") == {}:
            del result["properties"]
        if result.get("required") == []:
            del result["required"]
        if "const" in result and "type" in result:
            if not constant_has_type(result["const"], result["type"]):
                return False
            del result["type"]
        return result

    def roots(self):
        return {name: self.normalize(document, name)
                for name, document in self.documents.items()}


def verify(official_directory, custom_directory=CUSTOM):
    custom = SchemaSet(custom_directory)
    official = SchemaSet(official_directory)
    if set(custom.documents) != set(official.documents):
        missing = sorted(set(custom.documents) - set(official.documents))
        extra = sorted(set(official.documents) - set(custom.documents))
        raise ValueError(f"schema file sets differ: missing official={missing}, extra official={extra}")
    left, right = custom.roots(), official.roots()
    differences = [name for name in left if left[name] != right[name]]
    if differences:
        raise ValueError("schema validation constraints differ: " + ", ".join(differences))
    return {"schemas": len(left), "equivalent": True, "files": sorted(left)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--official-dir", required=True, type=Path)
    parser.add_argument("--custom-dir", type=Path, default=CUSTOM)
    args = parser.parse_args()
    try:
        result = verify(args.official_dir, args.custom_dir)
    except (ValueError, KeyError, TypeError, OSError) as error:
        print("official queue schema comparison failed: " + str(error), file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
