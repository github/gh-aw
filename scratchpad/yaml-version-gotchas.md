# YAML Version Gotchas: 1.1 vs 1.2 Parser Compatibility

This document explains the critical differences between YAML 1.1 and YAML 1.2 parsers and their impact on GitHub Agentic Workflows validation and compatibility.

## Table of Contents

- [Overview](#overview)
- [The Core Issue](#the-core-issue)
- [How gh-aw Handles This](#how-gh-aw-handles-this)
- [Affected Keywords](#affected-keywords)
- [Code Examples](#code-examples)
- [Impact on Validation](#impact-on-validation)
- [Recommendations](#recommendations)
- [Workarounds](#workarounds)
- [References](#references)

## Overview

YAML has two major versions with incompatible boolean parsing behavior:

- **YAML 1.1** (2005): Treats keywords like `on`, `off`, `yes`, `no` as boolean values
- **YAML 1.2** (2009): Treats these keywords as regular strings (only `true` and `false` are booleans)

This difference has critical implications for GitHub Agentic Workflows, particularly around the `on:` trigger key in workflow frontmatter.

## The Core Issue

### YAML 1.1 Boolean Parsing Problem

In YAML 1.1, certain plain (unquoted) strings are automatically converted to boolean values. This means the workflow trigger key `on:` can be misinterpreted as the boolean `true` instead of the string `"on"`.

**Example of the Problem:**

```python
# Python yaml.safe_load (YAML 1.1 parser)
import yaml

content = """
on:
  issues:
    types: [opened]
"""

result = yaml.safe_load(content)
print(result)
# Output: {True: {'issues': {'types': ['opened']}}}
#          ^^^^ The key is boolean True, not string "on"!
```text

This creates a **false positive** when validating workflows with Python-based tools, making it appear that the YAML is invalid when it's actually correct.

### YAML 1.2 Correct Behavior

YAML 1.2 parsers treat `on`, `off`, `yes`, and `no` as regular strings, not booleans. Only the explicit boolean literals `true` and `false` are treated as booleans.

**Example of Correct Behavior:**

```go
// Go goccy/go-yaml (YAML 1.2 parser) - Used by gh-aw
package main

import (
    "fmt"
    "github.com/goccy/go-yaml"
)

func main() {
    content := `
on:
  issues:
    types: [opened]
`
    var result map[string]interface{}
    yaml.Unmarshal([]byte(content), &result)
    
    fmt.Printf("%+v\n", result)
    // Output: map[on:map[issues:map[types:[opened]]]]
    //         ^^^ The key is string "on" ✓
}
```text

## How gh-aw Handles This

GitHub Agentic Workflows uses **`goccy/go-yaml` v1.18.0**, which is a **YAML 1.2 compliant parser**. This means:

✅ **`on:` is correctly parsed as a string key**, not a boolean  
✅ **Workflow frontmatter validation works correctly**  
✅ **GitHub Actions YAML is compatible** (GitHub Actions also uses YAML 1.2 parsing)

The compiler in `pkg/workflow/compiler.go` uses this parser to process workflow frontmatter, ensuring consistent and correct behavior.

## Affected Keywords

The following keywords are treated differently between YAML 1.1 and YAML 1.2:

### YAML 1.1 Boolean Keywords (Parsed as `true`)

```yaml
on:   # → true
yes:  # → true
y:    # → true
Y:    # → true
YES:  # → true
Yes:  # → true
ON:   # → true
On:   # → true
```text

### YAML 1.1 Boolean Keywords (Parsed as `false`)

```yaml
off:  # → false
no:   # → false
n:    # → false
N:    # → false
NO:   # → false
No:   # → false
OFF:  # → false
Off:  # → false
```text

### YAML 1.2 Behavior

In YAML 1.2, **all of the above are parsed as strings**. Only these explicit literals are booleans:

```yaml
true:   # → true
false:  # → false
True:   # → true (case-insensitive in some parsers)
False:  # → false (case-insensitive in some parsers)
```text

## Code Examples

### Example 1: Workflow Trigger Key

**Workflow Frontmatter:**
```yaml
---
on:
  issues:
    types: [opened]
permissions:
  issues: write
---
```text

**YAML 1.1 Parser (Python):**
```python
import yaml
content = open('workflow.md').read().split('---')[1]
data = yaml.safe_load(content)
print(type(list(data.keys())[0]))  # <class 'bool'>
print(list(data.keys())[0])        # True
```text

**YAML 1.2 Parser (gh-aw / goccy/go-yaml):**
```go
var data map[string]interface{}
yaml.Unmarshal([]byte(content), &data)
fmt.Printf("%T\n", "on")     // string
fmt.Printf("%v\n", data["on"]) // map[issues:...]
```text

### Example 2: Configuration Value

**Configuration:**
```yaml
settings:
  enabled: yes
  disabled: no
  mode: on
```text

**YAML 1.1 Parser Output:**
```python
{
  'settings': {
    'enabled': True,    # Boolean
    'disabled': False,  # Boolean
    'mode': True        # Boolean (the string "on" became True!)
  }
}
```text

**YAML 1.2 Parser Output:**
```go
map[string]interface{}{
  "settings": map[string]interface{}{
    "enabled": "yes",     // String
    "disabled": "no",     // String
    "mode": "on",         // String
  },
}
```text

### Example 3: Issue Labels

**Problematic with YAML 1.1:**
```yaml
labels:
  - bug
  - on hold       # Might be interpreted as "on: hold" with boolean key
  - off topic     # Might be interpreted as "off: topic" with boolean key
```text

**Safe Approach:**
```yaml
labels:
  - bug
  - "on hold"     # Quote to force string interpretation
  - "off topic"   # Quote to force string interpretation
```text

## Impact on Validation

### False Positives with Python Tools

Many developers use Python-based YAML validation tools during local development. These tools will report errors for valid gh-aw workflows:

**❌ False Positive Example:**

```bash
$ python -c "import yaml; yaml.safe_load(open('workflow.md'))"
# Error: Invalid structure - key is boolean True instead of string "on"
```text

**This is NOT a real error!** The workflow is valid and will work correctly with gh-aw.

### True Validation with YAML 1.2 Tools

To validate workflows locally, use YAML 1.2 compatible tools:

**✅ Correct Validation:**

```bash
# Use gh-aw's built-in compiler
$ gh aw compile workflow.md

# Or use a YAML 1.2 validator
$ yamllint --version  # Check if it supports YAML 1.2
```text

## Recommendations

### For Workflow Authors

1. **Use gh-aw's compiler for validation**
   ```bash
   gh aw compile workflow.md
   ```
   This ensures your workflow is validated with the same parser that will be used in production.

2. **Don't trust Python yaml.safe_load for validation**
   - It will give false positives for the `on:` trigger key
   - It may misinterpret other configuration values

3. **Quote ambiguous keywords when in doubt**
   ```yaml
   # If using YAML 1.1 tools for other purposes:
   'on':        # Force string interpretation
     issues:
       types: [opened]
   ```

4. **Use explicit booleans when you mean boolean values**
   ```yaml
   enabled: true      # ✓ Explicit boolean
   disabled: false    # ✓ Explicit boolean
   
   # Avoid these for boolean values:
   enabled: yes       # Might be confusing across parsers
   disabled: no       # Might be confusing across parsers
   ```

### For Tool Developers

1. **Use YAML 1.2 parsers for gh-aw integration**
   - Go: `github.com/goccy/go-yaml`
   - Python: `ruamel.yaml` (with YAML 1.2 mode)
   - JavaScript: `yaml` package v2+ (YAML 1.2 by default)
   - Ruby: `Psych` (YAML 1.2 by default in Ruby 2.6+)

2. **Document parser version in your tool**
   - Make it clear which YAML version your tool uses
   - Warn users if using YAML 1.1

3. **Consider adding compatibility mode**
   - Allow users to switch between YAML 1.1 and 1.2 parsing
   - Default to YAML 1.2 for gh-aw workflows

### For CI/CD Validation

If you want to validate workflows in CI/CD pipelines:

```yaml
# .github/workflows/validate.yml
name: Validate Workflows
on: pull_request

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      
      # Install gh-aw
      - run: gh extension install github/gh-aw
        env:
          GH_TOKEN: ${{ github.token }}
      
      # Validate all workflows
      - run: gh aw compile
```text

## Workarounds

### If You Must Use YAML 1.1 Tools

If you need to use YAML 1.1 tools (like Python's `yaml.safe_load`) for some reason, you can quote the `on` key:

**Option 1: Quote the Key**
```yaml
---
'on':          # Quoted key forces string interpretation
  issues:
    types: [opened]
---
```text

**Option 2: Use Alternative Trigger Names**
```yaml
---
# Not applicable - "on" is required by GitHub Actions
# This workaround doesn't actually work for workflows
---
```yaml

**Recommendation:** Don't use YAML 1.1 tools for gh-aw workflows. Use gh-aw's compiler instead.

### Migrating from YAML 1.1 to 1.2

If you have existing YAML 1.1 code that needs to work with YAML 1.2:

1. **Audit boolean usage**
   - Search for unquoted `yes`, `no`, `on`, `off` values
   - Determine if they should be booleans or strings

2. **Explicit boolean values**
   - Replace `yes` → `true`
   - Replace `no` → `false`
   - Keep `on` and `off` as strings (quote if needed)

3. **Test with both parsers**
   - Validate with YAML 1.2 parser (gh-aw)
   - Verify behavior matches expectations

## References

### YAML Specifications

- **YAML 1.1 Spec**: https://yaml.org/spec/1.1/
  - Boolean type: https://yaml.org/type/bool.html
  - Section 2.4 defines `yes`, `no`, `on`, `off` as boolean aliases

- **YAML 1.2 Spec**: https://yaml.org/spec/1.2/spec.html
  - Section 10.3.2: Only `true` and `false` are boolean literals
  - Plain scalars like `on`, `off`, `yes`, `no` are strings

### Parser Documentation

- **goccy/go-yaml**: https://github.com/goccy/go-yaml
  - YAML 1.2 compliant Go parser (used by gh-aw)
  
- **PyYAML**: https://pyyaml.org/wiki/PyYAMLDocumentation
  - YAML 1.1 parser (causes false positives)
  
- **ruamel.yaml**: https://yaml.readthedocs.io/
  - Python YAML 1.2 parser (recommended alternative to PyYAML)

### Related Issues

- Discussion: github/gh-aw#2489
- Schema consistency audit revealed this issue

### GitHub Actions Compatibility

GitHub Actions uses YAML 1.2 parsing for workflow files, which is why the `on:` trigger key works correctly. gh-aw's use of YAML 1.2 ensures full compatibility with GitHub Actions semantics.

## Conclusion

The YAML version difference between 1.1 and 1.2 is a critical gotcha that affects workflow validation and compatibility. gh-aw correctly uses YAML 1.2 parsing (via `goccy/go-yaml`), which:

1. ✅ Allows `on:` to be used as a trigger key (as a string, not boolean)
2. ✅ Maintains compatibility with GitHub Actions
3. ✅ Follows modern YAML standards (1.2 is from 2009)

However, this means that **Python yaml.safe_load and other YAML 1.1 tools will give false positives** when validating workflows. Always use gh-aw's compiler (`gh aw compile`) for validation.

---

**Last Updated**: 2025-10-26  
**Related**: Schema consistency audit, Issue github/gh-aw#2489
