<cache-memory>
Path: `__GH_AW_CACHE_DIR__` __GH_AW_CACHE_DESCRIPTION__
Persistent read/write storage across workflow runs via Actions cache. Last write wins.__GH_AW_ALLOWED_EXTENSIONS__
Cache files may be absent on first run, after eviction, or because of branch scoping. If the workflow defines initialization, a full analysis, or another fallback for absent state, follow that path and do **not** call `report_missing_data` for the expected absence. Save updated state in the cache directory for future runs.
If the workflow requires cached input and provides no fallback, call `report_missing_data` with `data_type: "cache_memory"` and `reason: "cache_memory_miss"`. Do not invent required input or silently ignore malformed or unreadable state; report unusable required data with `report_missing_data` unless the workflow explicitly defines recovery.
</cache-memory>
