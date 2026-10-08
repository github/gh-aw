import json
from pathlib import Path
import tempfile
import unittest

from verify_official_contract import verify


DRAFT = "https://json-schema.org/draft/2020-12/schema"


class OfficialContractTests(unittest.TestCase):
    def compare(self, custom, official):
        with tempfile.TemporaryDirectory(prefix="gh-aw-schema-equivalence-") as directory:
            root = Path(directory)
            for name, schemas in (("custom", custom), ("official", official)):
                target = root / name
                target.mkdir()
                for filename, schema in schemas.items():
                    document = {"$schema": DRAFT, "$id": filename, **schema}
                    (target / filename).write_text(json.dumps(document))
            return verify(root / "official", root / "custom")

    def test_root_refs_and_sealed_objects(self):
        custom = {
            "$ref": "#/$defs/Actor",
            "$defs": {"Actor": {
                "type": "object", "properties": {"id": {"type": "string"}},
                "required": ["id"], "additionalProperties": False,
            }},
        }
        official = {
            "type": "object", "properties": {"id": {"type": "string"}},
            "required": ["id"], "unevaluatedProperties": {"not": {}},
        }
        self.assertEqual(self.compare({"Actor.json": custom}, {"Actor.json": official})["schemas"], 1)

    def test_local_and_external_references(self):
        actor = {"type": "string", "minLength": 1, "x-utf8-max-bytes": 256}
        custom = {
            "Actor.json": actor,
            "Queue.json": {
                "$ref": "#/$defs/Queue",
                "$defs": {
                    "Queue": {"type": "array", "items": {"$ref": "#/$defs/Actor"}},
                    "Actor": actor,
                },
            },
        }
        official = {
            "Actor.json": actor,
            "Queue.json": {"type": "array", "items": {"$ref": "Actor.json"}},
        }
        self.assertTrue(self.compare(custom, official)["equivalent"])

    def test_typed_records(self):
        custom = {"type": "object", "additionalProperties": {"type": "integer", "minimum": 1}}
        official = {
            "type": "object", "properties": {},
            "unevaluatedProperties": {"type": "integer", "minimum": 1},
        }
        self.assertTrue(self.compare({"Record.json": custom}, {"Record.json": official})["equivalent"])

    def test_ref_siblings_intersect_bounds(self):
        custom = {
            "$ref": "#/$defs/Count", "maximum": 256,
            "$defs": {"Count": {"type": "integer", "minimum": 1, "maximum": 4096}},
        }
        official = {"type": "integer", "minimum": 1, "maximum": 256}
        self.assertTrue(self.compare({"Count.json": custom}, {"Count.json": official})["equivalent"])

    def test_const_type_and_order_are_redundant(self):
        custom = {"anyOf": [{"const": 3}, {"const": "worker"}]}
        official = {"anyOf": [{"type": "string", "const": "worker"}, {"type": "number", "const": 3}]}
        self.assertTrue(self.compare({"Choice.json": custom}, {"Choice.json": official})["equivalent"])

    def test_required_order_is_not_semantic(self):
        custom = {
            "type": "object", "properties": {"a": {}, "b": {}},
            "required": ["a", "b"], "additionalProperties": False,
        }
        official = {**custom, "required": ["b", "a"]}
        self.assertTrue(self.compare({"Record.json": custom}, {"Record.json": official})["equivalent"])

    def test_optional_only_model(self):
        custom = {"type": "object", "properties": {"trace": {}}, "required": [], "additionalProperties": False}
        official = {"type": "object", "properties": {"trace": {}}, "unevaluatedProperties": {"not": {}}}
        self.assertTrue(self.compare({"Trace.json": custom}, {"Trace.json": official})["equivalent"])

    def test_annotations_do_not_change_validation(self):
        custom = {"type": "string", "title": "custom", "description": "one"}
        official = {"type": "string", "title": "official", "$comment": "two"}
        self.assertTrue(self.compare({"Identity.json": custom}, {"Identity.json": official})["equivalent"])

    def test_validation_differences_are_not_hidden(self):
        pairs = [
            ({"type": "integer", "maximum": 256}, {"type": "integer", "maximum": 255}),
            ({"type": "string", "pattern": "^[1-9][0-9]*$"}, {"type": "string", "pattern": "^[0-9]*$"}),
            ({"type": "string", "x-utf8-max-bytes": 256}, {"type": "string", "x-utf8-max-bytes": 128}),
            ({"type": "object", "additionalProperties": False}, {"type": "object"}),
            ({"type": "object", "properties": {"id": {}}, "required": ["id"]},
             {"type": "object", "properties": {"id": {}}}),
            ({"type": "array", "items": {}, "uniqueItems": True}, {"type": "array", "items": {}}),
            ({"const": 1}, {"type": "boolean", "const": 1}),
        ]
        for custom, official in pairs:
            with self.subTest(custom=custom, official=official):
                with self.assertRaisesRegex(ValueError, "validation constraints differ"):
                    self.compare({"Test.json": custom}, {"Test.json": official})

    def test_unknown_keywords_fail_closed(self):
        with self.assertRaisesRegex(ValueError, "unsupported schema keywords"):
            self.compare({"Test.json": {"type": "string", "format": "uri"}},
                         {"Test.json": {"type": "string", "format": "uri"}})

    def test_composed_unevaluated_properties_fail_closed(self):
        schema = {"type": "object", "anyOf": [{"properties": {"id": {}}}], "unevaluatedProperties": False}
        with self.assertRaisesRegex(ValueError, "plain object"):
            self.compare({"Test.json": schema}, {"Test.json": schema})

    def test_conflicting_reference_properties_fail_closed(self):
        schema = {
            "$ref": "#/$defs/Base", "properties": {"b": {}},
            "$defs": {"Base": {"type": "object", "properties": {"a": {}}}},
        }
        with self.assertRaisesRegex(ValueError, "intersecting reference constraint"):
            self.compare({"Test.json": schema}, {"Test.json": schema})

    def test_recursive_references_need_separate_proof(self):
        schema = {"$ref": "#/$defs/Node", "$defs": {"Node": {"$ref": "#/$defs/Node"}}}
        with self.assertRaisesRegex(ValueError, "recursive schema"):
            self.compare({"Node.json": schema}, {"Node.json": schema})

    def test_missing_external_reference_fails(self):
        schema = {"$ref": "Missing.json"}
        with self.assertRaisesRegex(ValueError, "missing schema reference"):
            self.compare({"Node.json": schema}, {"Node.json": schema})

    def test_schema_file_sets_must_match(self):
        with self.assertRaisesRegex(ValueError, "file sets differ"):
            self.compare({"One.json": {}}, {"Two.json": {}})


if __name__ == "__main__":
    unittest.main()
