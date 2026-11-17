---
description: Extract structured API contract from a Go package README.md.
model: small
---
You are given the full contents of a single Go package README.md.

Emit a JSON object with these fields (omit any field that the README does not document):
- `public_api`: list of `{name, kind: "func"|"type"|"const", documented_signature_or_value, behavior_summary}` items
- `behavioral_contracts`: list of short bullet strings (one obligation each)
- `usage_examples`: list of `{label, input, expected_output}` items, verbatim from the README where possible
- `design_constraints`: list of short bullet strings (thread safety, error handling, etc.)
- `edge_cases`: list of short bullet strings (documented limitations)
- `ambiguities`: list of short bullet strings — any places the spec is unclear and a test will need to make assumptions

Do not invent details that the README does not state. Output JSON only.
