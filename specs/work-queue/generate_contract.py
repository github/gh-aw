#!/usr/bin/env python3
"""Emit closed JSON schemas from this contract's intentionally small TypeSpec subset.

No installation or generation dependency is needed. Unsupported declarations
fail rather than being silently omitted. Run with --check to detect drift.
"""
import argparse
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
SOURCE = ROOT / "specs/work-queue/transactions.tsp"
OUTPUT = ROOT / "pkg/workqueue/schema"


def generate():
    text = re.sub(r"//[^\n]*", "", SOURCE.read_text())
    text = re.sub(r'import "[^"]+";|using [\w.]+;', "", text)
    definitions = {}
    declarations = list(re.finditer(
        r"((?:@\w+(?:\([^)]*\))?\s*)*)(scalar|model|union)\s+(\w+)\s*"
        r"(?:extends\s+(\w+)\s*;|\{([^}]*)\})", text))

    def decorators(raw):
        constraints = {}
        names = {"minLength": "minLength", "maxLength": "maxLength",
                 "minValue": "minimum", "maxValue": "maximum",
                 "minItems": "minItems", "maxItems": "maxItems", "pattern": "pattern"}
        for name, arg in re.findall(r"@(\w+)(?:\(([^)]*)\))?", raw):
            if name == "jsonSchema":
                continue
            if name == "extension":
                key, value = json.loads("[" + arg + "]")
                if key not in ("x-utf8-max-bytes", "x-key-utf8-max-bytes", "x-key-min-utf8-bytes",
                               "x-forbid-ascii-controls", "x-forbid-key-ascii-controls", "uniqueItems"):
                    raise ValueError("unsupported extension " + key)
                constraints[key] = value
                continue
            if name not in names:
                raise ValueError("unsupported decorator " + name)
            constraints[names[name]] = json.loads(arg)
        return constraints

    def shape(value):
        value = value.strip()
        if "|" in value:
            return {"anyOf": [shape(part) for part in value.split("|")]}
        if value.endswith("[]"):
            return {"type": "array", "items": shape(value[:-2])}
        record = re.fullmatch(r"Record<(\w+)>", value)
        if record:
            return {"type": "object", "additionalProperties": shape(record[1])}
        primitives = {"string": {"type": "string"}, "boolean": {"type": "boolean"},
                      "safeint": {"type": "integer"}, "unknown": {},
                      "null": {"type": "null"}}
        if value in primitives:
            return primitives[value].copy()
        if value.startswith('"') or re.fullmatch(r"\d+", value):
            return {"const": json.loads(value)}
        if not re.fullmatch(r"\w+", value):
            raise ValueError("unsupported type " + value)
        return {"$ref": "#/$defs/" + value}

    consumed = text
    for match in reversed(declarations):
        consumed = consumed[:match.start()] + consumed[match.end():]
    if consumed.strip():
        raise ValueError("unsupported source: " + consumed.strip()[:100])
    for match in declarations:
        annotation, kind, name, base, body = match.groups()
        if kind == "scalar":
            definitions[name] = {**shape(base), **decorators(annotation)}
        elif kind == "union":
            definitions[name] = {"anyOf": [shape(v.strip()) for v in body.split(",") if v.strip()]}
        else:
            fields = re.findall(r"((?:@\w+(?:\([^)]*\))?\s*)*)(\w+)(\?)?\s*:\s*([^;]+);", body)
            remaining = re.sub(r"((?:@\w+(?:\([^)]*\))?\s*)*)(\w+)(\?)?\s*:\s*([^;]+);", "", body)
            if remaining.strip():
                raise ValueError("unsupported fields in " + name)
            definitions[name] = {
                "type": "object", "additionalProperties": False,
                "properties": {field: {**shape(value), **decorators(ann)}
                               for ann, field, optional, value in fields},
                "required": [field for ann, field, optional, value in fields if not optional],
            }
    outputs = {}
    for name in definitions:
        if definitions[name].get("type") not in ("object",) and "anyOf" not in definitions[name]:
            continue
        reachable = {}

        def visit(value):
            if isinstance(value, dict):
                if "$ref" in value:
                    target = value["$ref"].split("/")[-1]
                    if target not in reachable:
                        reachable[target] = definitions[target]
                        visit(definitions[target])
                for child in value.values():
                    visit(child)
            elif isinstance(value, list):
                for child in value:
                    visit(child)

        visit({"$ref": "#/$defs/" + name})
        schema = {"$schema": "https://json-schema.org/draft/2020-12/schema",
                  "$id": name + ".json", "title": name,
                  "$ref": "#/$defs/" + name, "$defs": reachable}
        outputs[name + ".json"] = json.dumps(schema, sort_keys=True, indent=2, ensure_ascii=False) + "\n"
    return outputs


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    outputs = generate()
    if args.check:
        existing = {p.name: json.loads(p.read_text()) for p in OUTPUT.glob("*.json")}
        if existing != {name: json.loads(content) for name, content in outputs.items()}:
            print("queue contract schema drift; run python3 specs/work-queue/generate_contract.py", file=sys.stderr)
            return 1
    else:
        OUTPUT.mkdir(parents=True, exist_ok=True)
        for path in OUTPUT.glob("*.json"):
            if path.name not in outputs:
                path.unlink()
        for name, content in outputs.items():
            (OUTPUT / name).write_text(content)
    return 0


if __name__ == "__main__":
    sys.exit(main())
