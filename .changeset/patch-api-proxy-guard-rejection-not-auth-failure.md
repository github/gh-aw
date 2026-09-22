---
"gh-aw": patch
---

AWF API proxy guardrail rejections (`max_cache_misses_exceeded`, `effective_tokens_limit_exceeded`, `permission_denied_limit_exceeded`, `model_policy_violation`) are no longer misclassified as `authentication_failed` by the Copilot harness. The proxy answers these with HTTP 403 and the Copilot CLI discards the structured body, printing only "Authentication failed with provider at ... (HTTP 403)", which previously caused a pointless fresh-run retry against a proxy whose counter had not reset. The harness now consults the proxy's own structured log, classifies the attempt as `api_proxy_guard_rejected`, stops retrying, and logs the guard name with its counters (for example `max_cache_misses_exceeded (consecutive_cache_misses=5, max_cache_misses=5)`). The threat-detection job reports the same guard details when it fails, so the guardrail is no longer invisible there. Genuine 401/403 credential failures keep their existing classification.
