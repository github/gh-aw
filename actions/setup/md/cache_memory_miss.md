> [!WARNING]
> <details>
> <summary>Missing Cache Data: cache miss detected after cache restore succeeded.</summary>
> 
> The agent reported a cache miss (`report_missing_data` with `reason: cache_memory_miss`) after the workflow successfully restored cache-memory for this run. A restore match does not guarantee that every required file exists. Check the workflow's required inputs, cache paths, and key consistency before diagnosing a configuration problem.
> 
> This warning is shown only when a cache restore matched an existing key. Cache misses can be expected on first runs and on branches where `actions/cache` has no visible entries due to branch scoping.
> 
> Review the [cache-memory configuration](https://github.github.com/gh-aw/reference/cache-memory/). If the workflow defines initialization or another fallback for absent state, follow that path without reporting expected absence as missing data. Missing required inputs without a fallback should still be reported.
> 
> **File naming convention:** Cache files are stored at `$GH_AW_RUNTIME_DIR/cache-memory/` (default) or `$GH_AW_RUNTIME_DIR/cache-memory-<id>/` for additional caches. Replace `<id>` with the configured cache ID. Expand GH_AW_RUNTIME_DIR in shell commands. Use descriptive file and directory names with subdirectories for organization.
> 
> </details>
