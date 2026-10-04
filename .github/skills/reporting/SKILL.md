---
name: reporting
description: Format reports with HTML details/summary blocks for readable output.
---

# Report Format Guidelines

Use these rules to format reports with collapsible sections.

## Use HTML Details/Summary Tags

Reduce scrolling and improve readability: **wrap reports in HTML `<details>` and `<summary>` tags** so users can expand and collapse sections.

**Basic Structure:**

```markdown
<details>
<summary>📊 Report Title - [Date]</summary>

## Report Content

Your detailed report content goes here...

### Section 1

Content for section 1...

### Section 2

Content for section 2...

</details>
```
