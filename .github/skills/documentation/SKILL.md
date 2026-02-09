---
name: documentation
description: Documentation Brief description for SEO and navigation
---

### Documentation

The documentation for this project is available in the `docs/` directory. It uses GitHub-flavored markdown with Astro Starlight for rendering and follows the Diátaxis framework for systematic documentation.

## Diátaxis Framework

Documentation must be organized into four distinct types, each serving a specific purpose:

### 1. Tutorials (Learning-Oriented)
**Purpose**: Guide beginners through achieving a specific outcome to build confidence.

- Start with what the user will build or achieve
- Provide a clear, step-by-step path from start to finish
- Include concrete examples and working code
- Assume minimal prior knowledge
- Focus on the happy path (avoid edge cases and alternatives)
- End with a working result the user can see and use
- Use imperative mood: "Create a file", "Run the command"

**Avoid**: Explaining concepts in depth, multiple options, troubleshooting

### 2. How-to Guides (Goal-Oriented)
**Purpose**: Show how to solve a specific real-world problem or accomplish a particular task.

- Title format: "How to [accomplish specific goal]"
- Assume the user knows the basics
- Focus on practical steps to solve one problem
- Include necessary context but stay focused
- Show multiple approaches only when genuinely useful
- End when the goal is achieved
- Use imperative mood: "Configure the setting", "Add the following"

**Avoid**: Teaching fundamentals, explaining every detail, being exhaustive

### 3. Reference (Information-Oriented)
**Purpose**: Provide accurate, complete technical descriptions of the system.

- Organized by structure (CLI commands, configuration options, API endpoints)
- Comprehensive and authoritative
- Consistent format across all entries
- Technical accuracy is paramount
- Include all parameters, options, and return values
- Use descriptive mood: "The command accepts", "Returns a string"
- Minimal narrative or explanation

**Avoid**: Instructions, tutorials, opinions on usage

### 4. Explanation (Understanding-Oriented)
**Purpose**: Clarify and illuminate topics to deepen understanding.

- Discuss why things are the way they are
- Explain design decisions and tradeoffs
- Provide context and background
- Connect concepts to help form mental models
- Discuss alternatives and their implications
- Use indicative mood: "This approach provides", "The engine uses"

**Avoid**: Step-by-step instructions, exhaustive reference material

## General Style Guidelines

- **Tone**: Neutral, technical, not promotional
- **Voice**: Avoid "we", "our", "us" (use "the tool", "this command")
- **Headings**: Use markdown heading syntax, not bold text as headings
- **Lists**: Avoid long bullet point lists; prefer prose with structure
- **Code samples**: Minimal and focused; exclude optional fields unless relevant
- **Language tag**: Use `aw` for agentic workflow snippets with YAML frontmatter

**Example workflow code block**:
```aw wrap
on: push
# Your workflow steps here
```

## GitHub-Flavored Markdown Syntax

Documentation files use GitHub-flavored markdown with Astro Starlight for rendering. Key syntax elements:

### Frontmatter
Every documentation page must have frontmatter:
```markdown
title: Page Title
description: Brief description for SEO and navigation
```

### GitHub Alerts
Use GitHub's alert syntax for notes, tips, warnings, and cautions:
```markdown
> [!NOTE]
> Important information the reader should notice.

> [!TIP]
> Helpful advice for the reader.

> [!WARNING]
> Warning about potential issues or pitfalls.

> [!CAUTION]
> Critical warning about dangerous operations.

> [!IMPORTANT]
> Key information users need to know.
```

### Code Blocks
- Use syntax highlighting with language tags
- Add `title` attribute for file names: ` ```yaml title=".github/workflows/example.yml" `
- Use `aw` language for agentic workflow files with YAML frontmatter
- Add `wrap` for line wrapping: ` ```aw wrap `

### Links
- Internal links: Use relative paths between documentation pages
- External links: Open in new tab automatically
- Link text: Use descriptive text, avoid "click here"

### Tabs
Use tabs for showing alternatives (e.g., different languages, platforms):
```markdown
import { Tabs, TabItem } from '@astrojs/starlight/components';

<Tabs>
  <TabItem label="npm">
    ```bash
    npm install package
    ```
  </TabItem>
  <TabItem label="yarn">
    ```bash
    yarn add package
    ```
  </TabItem>
</Tabs>
```

### Cards
Use cards for navigation or highlighting multiple options:
```markdown
import { Card, CardGrid } from '@astrojs/starlight/components';

<CardGrid>
  <Card title="Getting Started" icon="rocket">
    Quick introduction to the basics.
  </Card>
  <Card title="Advanced Usage" icon="setting">
    Deep dive into advanced features.
  </Card>
</CardGrid>
```

**Remember**: Keep components minimal. Prefer standard markdown when possible.

## Content to Avoid

- "Key Features" sections
- Marketing language or selling points
- Excessive bullet points (prefer structured prose)
- Overly verbose examples with all optional parameters
- Mixing documentation types (e.g., tutorials that become reference)

## Avoiding Documentation Bloat

Documentation bloat reduces clarity and makes content harder to navigate. Common types of bloat include:

### Types of Documentation Bloat

1. **Duplicate content**: Same information repeated in different sections
2. **Excessive bullet points**: Long lists that could be condensed into prose or tables
3. **Redundant examples**: Multiple examples showing the same concept
4. **Verbose descriptions**: Overly wordy explanations that could be more concise
5. **Repetitive structure**: The same "What it does" / "Why it's valuable" pattern overused

### Writing Concise Documentation

When editing documentation, focus on:

**Consolidate bullet points**: 
- Convert long bullet lists into concise prose or tables
- Remove redundant points that say the same thing differently

**Eliminate duplicates**:
- Remove repeated information
- Consolidate similar sections

**Condense verbose text**:
- Make descriptions more direct and concise
- Remove filler words and phrases
- Keep technical accuracy while reducing word count

**Standardize structure**:
- Reduce repetitive "What it does" / "Why it's valuable" patterns
- Use varied, natural language

**Simplify code samples**:
- Remove unnecessary complexity from code examples
- Focus on demonstrating the core concept clearly
- Eliminate boilerplate or setup code unless essential for understanding
- Keep examples minimal yet complete
- Use realistic but simple scenarios

### Example: Before and After

**Before (Bloated)**:
```markdown
### Tool Name
Description of the tool.

- **What it does**: This tool does X, Y, and Z
- **Why it's valuable**: It's valuable because A, B, and C
- **How to use**: You use it by doing steps 1, 2, 3, 4, 5
- **When to use**: Use it when you need X
- **Benefits**: Gets you benefit A, benefit B, benefit C
- **Learn more**: [Link](url)
```

**After (Concise)**:
```markdown
### Tool Name
Description of the tool that does X, Y, and Z to achieve A, B, and C.

Use it when you need X by following steps 1-5. [Learn more](url)
```

### Documentation Quality Guidelines

1. **Preserve meaning**: Never lose important information
2. **Be surgical**: Make precise edits, don't rewrite everything
3. **Maintain tone**: Keep the neutral, technical tone
4. **Test locally**: Verify links and formatting are still correct

## Structure by File Type

- **Getting Started**: Tutorial format
- **How-to Guides**: Goal-oriented, one task per guide
- **CLI Reference**: Reference format, complete command documentation
- **Concepts**: Explanation format, building understanding
- **API Reference**: Reference format, complete API documentation
