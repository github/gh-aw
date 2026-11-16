---
"gh-aw": major
---

Replace the safe-output outcome model with the delivery and adoption model.

**Breaking change:** `gh aw logs --outcomes`, `.outcomes`,
`.episode_outcomes`, outcome JSONL fields, and `outcome:*` experiment metrics
are replaced by `--adoptions`, `.adoptions`, `.episode_adoptions`,
`adoption_status`, and `adoption:*`. Safe-output application events move from
`safe_output.outcome` to `safe_output.delivery`.

Add a 55-type adoption registry plus human-readable and W3C-style specifications
for delivery, adoption, and operational value.
