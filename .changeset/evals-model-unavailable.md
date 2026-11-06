---
"gh-aw": patch
---

Gracefully skip evals when the Decisions provider explicitly reports that the
configured model is unavailable or unsupported. Emit a warning and a
`not_evaluated` marker with reason code `model_unavailable`, without fabricated
answers, costs, retries, or a chat-model fallback. Other request failures remain
errors, including HTTP 404 responses that could indicate routing problems.
Daily accounting recognizes validated, run-matched unavailable-model skips
without weakening missing-usage checks for executed inference.
