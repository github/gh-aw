# Serena Tools Usage - Quick Reference

**Workflow:** Sergo - Serena Go Expert  
**Run ID:** [21560089409](https://github.com/github/gh-aw/actions/runs/21560089409/job/62122702303#step:33:1)

## At a Glance

| Metric | Value | Status |
|--------|-------|--------|
| Total Tool Calls | 44 | ✓ |
| Serena Tool Calls | 9 (20.45%) | ⚠️ Low |
| Response Rate | 100% | ✓ |
| Tools Registered | 23 | - |
| Tools Used | 6 (26.09%) | ⚠️ Low adoption |
| Most Used Tool | Bash (17 calls) | - |
| Most Used Serena Tool | search_for_pattern (3 calls) | - |

## Tool Call Breakdown

```text
Builtin:      ████████████████████████████████████ 34 (77.27%)
Serena:       █████████ 9 (20.45%)
SafeOutputs:  █ 1 (2.27%)
GitHub:       0 (0.00%)
```

## Serena Tools - Used vs Unused

### ✅ Used (6 tools, 9 calls)

1. **search_for_pattern** - 3 calls → Code pattern searching
2. **find_symbol** - 2 calls → Symbol lookup
3. **get_current_config** - 1 call → Configuration retrieval
4. **initial_instructions** - 1 call → Workflow setup
5. **check_onboarding_performed** - 1 call → Initialization check
6. **list_memories** - 1 call → Memory listing

### ❌ Unused (17 tools, 0 calls)

**File Operations (2):**
- list_dir, find_file

**Symbol Analysis (2):**
- get_symbols_overview, find_referencing_symbols

**Code Modification (4):**
- replace_symbol_body, insert_after_symbol, insert_before_symbol, rename_symbol

**Memory Management (4):**
- write_memory, read_memory, delete_memory, edit_memory

**Project (2):**
- activate_project, onboarding

**Meta-Cognitive (3):**
- think_about_collected_information, think_about_task_adherence, think_about_whether_you_are_done

## Key Insights

### 🎯 Usage Patterns

- **Builtin Dominance:** 77% of calls use standard file operations (Bash, Read, Write)
- **Selective Serena Use:** Only language-specific tasks trigger Serena tools
- **Search Focus:** Pattern searching is the primary Serena use case
- **No Code Modification:** Zero calls to code editing tools

### ⚡ Performance

- **100% Success Rate:** All 44 requests received responses
- **No Failures:** Zero timeout or error conditions
- **Stable Connection:** Reliable MCP gateway ↔ Serena communication

### 📦 Request/Response Size Metrics

**Overall Data Transfer:**
- **Total Data:** 425.69 KB (72.60 KB requests + 353.09 KB responses)
- **Response Amplification:** 4.86x average (responses 4.86x larger than requests)

**By Category:**
- **Bash:** 181.17 KB (42.56% of all data) - largest consumer
- **Serena Tools:** 12.32 KB (2.89% of all data)
- **SafeOutputs:** 30.58 KB (7.18% of all data) - single large request

**Serena Efficiency:**
- **Compact requests:** 700-840 bytes average per call
- **Compact responses:** 386-771 bytes average per call
- **Bandwidth efficient:** <1x response amplification vs. 11.8x for Bash
- **Structured data:** Returns precise, formatted results vs. verbose text

**Key Insight:** Serena tools are **bandwidth-efficient** despite lower usage - they transfer 10x less data per call than Bash operations.

### 📊 Efficiency Opportunities

1. **Tool Registration Overhead:** 17/23 tools (74%) unused → consider lazy loading
2. **Underutilized Capabilities:** Symbol overview, code refactoring tools never called
3. **Memory Tools:** Not used despite being designed for cross-run learning
4. **Meta-Cognitive Tools:** Reflection tools available but ignored by agent

## Recommendations

### 🔧 Immediate Actions

1. **Update Agent Prompts:** Encourage Serena tool usage for Go-specific analysis
2. **Add Tool Examples:** Show when to use `get_symbols_overview` vs `Read`
3. **Enable Memory:** Configure agent to use `write_memory`/`read_memory` for persistence

### 📈 Long-term Improvements

1. **Tool Subsets:** Create workflow-specific tool collections
2. **Usage Analytics:** Track tool latency and success rates per tool
3. **Agent Training:** Demonstrate value of language-aware vs text-based operations
4. **Cost Optimization:** Reduce unused tool registration overhead

## Related Documents

- 📄 [Full Statistical Analysis](./serena-tools-analysis.md) - Complete statistical analysis with all metrics
- 🔗 [Workflow Run](https://github.com/github/gh-aw/actions/runs/21560089409/job/62122702303) - Original workflow execution

---

**Last Updated:** 2026-02-01  
**Analysis Type:** Statistical Tool Usage Report  
**Confidence:** High (100% response rate, clean log data)
