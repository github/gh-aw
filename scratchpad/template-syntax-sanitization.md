# Template Syntax Sanitization (T24)

## Overview

This document describes the template syntax sanitization feature implemented to address security concern T24: Template Injection Pattern Bypass.

## Problem Statement

Template injection patterns (Jinja2, Liquid, ERB, JavaScript template literals) were not explicitly detected or escaped by the sanitization logic. While GitHub's markdown rendering doesn't process template syntax, the lack of explicit sanitization represented a defense gap if downstream systems use template engines.

## Solution

The `neutralizeTemplateDelimiters` function in `actions/setup/js/sanitize_content_core.cjs` now detects and escapes template syntax delimiters to prevent potential template injection if content is processed by downstream template engines.

### Template Patterns Detected

1. **Jinja2/Liquid**: `{{ ... }}`
   - Example: `{{ secrets.TOKEN }}`
   - Escaped to: `\{\{ secrets.TOKEN }}`

2. **ERB**: `<%= ... %>`
   - Example: `<%= config %>`
   - Escaped to: `\<%= config %>`

3. **JavaScript Template Literals**: `${ ... }`
   - Example: `${ expression }`
   - Escaped to: `\$\{ expression }`

4. **Jinja2 Comments**: `{# ... #}`
   - Example: `{# comment #}`
   - Escaped to: `\{# comment #}`

5. **Jekyll/Liquid Directives**: `{% ... %}`
   - Example: `{% raw %}{{code}}{% endraw %}`
   - Escaped to: `\{\% raw %}\{\{code}}\{\% endraw %}`

## Implementation Details

### Function Location

- **File**: `actions/setup/js/sanitize_content_core.cjs`
- **Function**: `neutralizeTemplateDelimiters(s)`
- **Integration**: Called in the `sanitizeContentCore` function pipeline, after bot trigger neutralization and before markdown code region balancing

### Escaping Strategy

The function uses backslash escaping to neutralize template delimiters:
- `{{` → `\{\{`
- `<%=` → `\<%=`
- `${` → `\$\{`
- `{#` → `\{#`
- `{%` → `\{%`

This escaping prevents template engines from recognizing and evaluating these patterns while preserving the original content for human readability.

### Logging

When template patterns are detected:
1. **Info logs** are generated for each pattern type detected (e.g., "Template syntax detected: Jinja2/Liquid double braces {{")
2. A **warning log** is generated summarizing the defense-in-depth approach

Example warning message:
```
Template-like syntax detected and escaped. This is a defense-in-depth measure 
to prevent potential template injection if content is processed by downstream 
template engines. GitHub's markdown rendering does not evaluate template syntax.
```

## Defense-in-Depth Rationale

This is a **defense-in-depth** security measure:

### Current State
- **GitHub's markdown rendering** does NOT evaluate template syntax
- **No direct risk** in GitHub's current architecture
- Content with template patterns is rendered as-is in markdown

### Future-Proofing
- Protects against potential future integration scenarios
- Prevents issues if content is:
  - Processed by downstream template engines
  - Exported to systems using Jinja2, Liquid, ERB, or other template engines
  - Used in contexts where template evaluation might occur

### Best Practice
- Aligns with security best practices of sanitizing potentially dangerous patterns
- Reduces attack surface for template injection vulnerabilities
- Documents that these patterns are intentionally neutralized

## Test Coverage

Comprehensive tests in `actions/setup/js/sanitize_content.test.cjs` cover:

1. **Individual template types**: Each pattern type tested separately
2. **Multiple occurrences**: Multiple instances of the same pattern
3. **Mixed patterns**: Multiple different template types in the same text
4. **Multi-line content**: Template patterns across multiple lines
5. **Edge cases**:
   - Already escaped patterns (double-escaping behavior)
   - Single curly braces (not escaped)
   - Dollar signs without braces (not escaped)
   - Template patterns in code blocks (still escaped)
   - GitHub Actions expressions like `${{` (only `{{` pattern matched)
   - Nested template patterns
   - Templates combined with other sanitization (mentions, URLs)

### Test Results

All 233 tests pass, including 17 new tests specifically for template delimiter neutralization.

## Security Impact

### Risk Level
- **Severity**: MEDIUM
- **Status**: MITIGATED

### Before Fix
- Template patterns passed through unmodified (except ERB partially mitigated by `<` conversion in XML tag sanitization)
- Defense gap if downstream systems use template engines
- Potential for template injection in future integration scenarios

### After Fix
- All template delimiters explicitly detected and escaped
- Defense-in-depth protection against template injection
- Clear logging when template patterns are detected
- Documented security measure with test coverage

## Usage

The sanitization is automatic and applies to all content processed through:
- `sanitizeIncomingText()` - Used for compute_text
- `sanitizeContentCore()` - Core sanitization without mention filtering
- `sanitizeContent()` - Full sanitization with mention filtering

No configuration or opt-in required - template neutralization is always active.

## Related Files

- **Implementation**: `actions/setup/js/sanitize_content_core.cjs`
- **Tests**: `actions/setup/js/sanitize_content.test.cjs`
- **Issue**: T24 - Template Syntax Not Explicitly Sanitized

## References

- [OWASP: Server-Side Template Injection](https://owasp.org/www-project-web-security-testing-guide/latest/4-Web_Application_Security_Testing/07-Input_Validation_Testing/18-Testing_for_Server-side_Template_Injection)
- [Jinja2 Documentation](https://jinja.palletsprojects.com/)
- [Liquid Template Language](https://shopify.github.io/liquid/)
- [ERB (Embedded Ruby)](https://docs.ruby-lang.org/en/master/ERB.html)
- [JavaScript Template Literals](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Template_literals)
