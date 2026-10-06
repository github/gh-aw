// Package parser — sub_agent_extractor.go
//
// This file provides inline sub-agent parsing for workflow markdown files.
//
// # Inline Sub-Agents
//
// A sub-agent is a secondary agent definition embedded directly in the same
// markdown file as the primary workflow. Each sub-agent has its own frontmatter
// block plus a prompt body. Sub-agents appear after the main workflow body and
// are delimited by level-2 Markdown headings:
//
//	## agent: `name`        ← opens a sub-agent block
//	## end agent: `name`    ← optional, explicitly closes the block
//
// An agent block ends at a matching "## end agent: `name`" marker if one is
// present, or otherwise at the next level-2 Markdown heading (## ...) or end
// of file. The name must be a lowercase identifier (letters, digits, hyphens,
// underscores; must start with a letter). The explicit end marker is useful
// when a sub-agent block is embedded in the middle of a document (for
// example via an import) so that unrelated content following it is not
// swallowed into the block.
//
// Both the agent marker and any subsequent H2 section heading render as visible
// section headings in any Markdown preview (GitHub, VS Code, etc.).
//
// # Sub-Agent Frontmatter
//
// Sub-agent frontmatter keys and their order are preserved without filtering;
// boundary whitespace is trimmed.
//
// # Example
//
//	---
//	engine: copilot
//	on:
//	  issues:
//	    types: [opened]
//	---
//	# Handle issue
//	Triage the issue and delegate work to sub-agents.
//
//	## agent: `planner`
//	---
//	model: claude-haiku-4.5
//	description: Plans the work for the issue
//	---
//	You are a planning specialist.
//
//	## agent: `executor`
//	---
//	description: Executes the plan
//	---
//	You are an execution specialist.
//
// # Compilation Output
//
// During compilation the extracted sub-agents are written to the repository:
//   - Copilot engine: .github/agents/<name>.md
//   - Other engines: handled by the engine-specific compiler path
//
// # Wire-Up
//
// ExtractInlineSubAgents is called early in processToolsAndMarkdown so that
// the main workflow content (returned as mainMarkdown) is used for all
// subsequent prompt generation, while the sub-agent files are written at
// runtime by interpolate_prompt.cjs after runtime imports are inlined.

package parser

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var subAgentLog = logger.New("parser:sub_agent_extractor")

// ValidateInlineSubAgentsFrontmatter performs best-effort frontmatter validation
// on every inline sub-agent section found in markdown.
//
// markdown should be the full content of a workflow file (including any
// top-level frontmatter block). The function strips the top-level frontmatter
// before scanning for ## agent: `name` markers so that the file-level
// frontmatter is not mistaken for sub-agent content.
//
// For each detected sub-agent the function attempts to parse its embedded
// frontmatter block (--- … ---).
//
// All issues are returned as human-readable warning strings. Callers must not
// fail compilation based on these messages — they are advisory only (best effort).
// If no sub-agents are found, or if no issues are detected, nil is returned.
func ValidateInlineSubAgentsFrontmatter(markdown string) []string {
	// Strip the top-level frontmatter to obtain only the markdown body.
	var body string
	if parsed, err := ExtractFrontmatterFromContent(markdown); err == nil {
		body = parsed.Markdown
	} else {
		body = markdown
	}
	return ValidateInlineSubAgentsInBody(body)
}

// ValidateInlineSubAgentsInBody performs best-effort frontmatter validation on
// inline sub-agent sections found in an already-stripped markdown body.
// Unlike ValidateInlineSubAgentsFrontmatter, it does not strip a top-level
// frontmatter block, making it suitable for callers that have already parsed
// the file and hold the markdown body separately.
//
// All issues are returned as human-readable warning strings (best-effort,
// never abort compilation). If no sub-agents are found or no issues are
// detected, nil is returned.
func ValidateInlineSubAgentsInBody(body string) []string {
	_, subAgents, err := ExtractInlineSubAgents(body)
	if err != nil {
		// Surface extraction errors (e.g. duplicate agent names) as a warning
		// rather than silently skipping validation.
		return []string{fmt.Sprintf("could not extract inline sub-agents: %v", err)}
	}
	if len(subAgents) == 0 {
		return nil
	}

	var warnings []string
	for _, agent := range subAgents {
		warnings = append(warnings, validateSubAgentFrontmatterSyntax(agent)...)
	}
	return warnings
}

// validateSubAgentFrontmatterSyntax parses the frontmatter block embedded in a
// single InlineSubAgent.Content and returns warning messages for parse errors only.
func validateSubAgentFrontmatterSyntax(agent InlineSubAgent) []string {
	if _, err := ExtractFrontmatterFromContent(agent.Content); err != nil {
		return []string{fmt.Sprintf("sub-agent %q: could not parse frontmatter: %v", agent.Name, err)}
	}
	return nil
}

// GetEngineSubAgentExt returns the file extension used for inline sub-agent files
// for a given engine.
//
//	claude / codex / gemini → .md
//	others                  → .agent.md  (Copilot default)
func GetEngineSubAgentExt(engineID string) string {
	switch strings.ToLower(engineID) {
	case "claude", "codex", "gemini":
		return ".md"
	default:
		return ".agent.md"
	}
}

// InlineSubAgent holds a single sub-agent definition extracted from a workflow
// markdown file's body using the ## agent: `name` syntax.
type InlineSubAgent struct {
	// Name is the identifier taken from the ## agent: `name` line.
	// It is lowercase and safe to use as a filename.
	Name string

	// Content is the raw text between the ## agent: `name` line and the next
	// level-2 Markdown heading (## ...) or EOF. It typically includes a YAML
	// frontmatter block (---...---) followed by the sub-agent's prompt body,
	// but the format is not enforced — it varies by engine.
	Content string
}

// SubAgentModel identifies a declared model and the agent requesting it.
type SubAgentModel struct {
	Name  string
	Model string
}

// ExtractSubAgentModels reads model declarations from inline agent frontmatter.
// Malformed frontmatter is handled by the existing advisory validation.
func ExtractSubAgentModels(body string) []SubAgentModel {
	_, agents, err := ExtractInlineSubAgents(body)
	if err != nil {
		return nil
	}
	var models []SubAgentModel
	for _, agent := range agents {
		parsed, err := ExtractFrontmatterFromContent(agent.Content)
		if err != nil {
			continue
		}
		if model, ok := parsed.Frontmatter["model"].(string); ok && strings.TrimSpace(model) != "" {
			models = append(models, SubAgentModel{Name: agent.Name, Model: strings.TrimSpace(model)})
		}
	}
	return models
}

// subAgentSeparatorRegex matches the inline sub-agent start marker line.
//
// Format (anchored to line boundaries via (?m)):
//
// ## agent: `name`
//
// Rules:
//   - A level-2 Markdown heading (##)
//   - One or more whitespace characters between "##" and "agent:"
//   - One or more whitespace characters between "agent:" and the backtick-enclosed name
//   - Agent name: starts with a lowercase letter, followed by lowercase letters,
//     digits, hyphens, or underscores
//   - Optional trailing whitespace
var subAgentSeparatorRegex = regexp.MustCompile("(?m)^##[ \t]+agent:[ \t]+`([a-z][a-z0-9_-]*)`[ \t]*$")

// subAgentBoundaryRegex is applied to a substring that starts at an implicit H2
// boundary. Keep it start-anchored without multiline mode or capture groups so
// it only recognizes an agent marker exactly at that boundary.
var subAgentBoundaryRegex = regexp.MustCompile("^##[ \t]+agent:[ \t]+`[a-z][a-z0-9_-]*`[ \t]*(?:\n|$)")

// subAgentEndRegex matches the optional explicit end marker for an inline
// sub-agent block: "## end agent: `name`". It mirrors the start marker's name
// rules. When present, it closes the sub-agent block exactly at that heading
// instead of at the next H2 heading or EOF.
var subAgentEndRegex = regexp.MustCompile("(?m)^##[ \t]+end[ \t]+agent:[ \t]+`([a-z][a-z0-9_-]*)`[ \t]*$")

// ExtractInlineSubAgents splits markdown into the main workflow section and any
// inline sub-agent definitions.
//
// It scans the markdown body for ## agent: `name` start markers. Content before
// the first start marker is returned as mainMarkdown (trimmed of trailing
// newlines). Each start marker opens a sub-agent whose content spans to a
// matching ## end agent: `name` marker if one is present, or otherwise to the
// next level-2 Markdown heading (## ...) or EOF — whichever comes first. When
// an explicit end marker closes a sub-agent, any text following it (up to the
// next start marker or EOF) is preserved as main markdown.
//
// If no start markers are found the original markdown is returned unchanged and
// agents is nil.
func ExtractInlineSubAgents(markdown string) (mainMarkdown string, agents []InlineSubAgent, err error) {
	subAgentLog.Printf("Extracting inline sub-agents from markdown (length: %d)", len(markdown))
	allStarts := subAgentSeparatorRegex.FindAllStringSubmatchIndex(markdown, -1)
	if len(allStarts) == 0 {
		if err := validateNoInlineSectionEndMarkers(markdown, subAgentEndRegex); err != nil {
			return "", nil, fmt.Errorf("inline sub-agent end marker should reference a valid sub-agent name: %w", err)
		}
		subAgentLog.Print("No inline sub-agent markers found")
		return markdown, nil, nil
	}

	subAgentLog.Printf("Found %d inline sub-agent marker(s)", len(allStarts))
	if err := validateUniqueSubAgentNames(markdown, allStarts); err != nil {
		return "", nil, err
	}

	mainMarkdown, agents, err = extractInlineSections(markdown, allStarts, subAgentEndRegex, inlineSkillBoundaryRegex, func(name, content string) InlineSubAgent {
		subAgentLog.Printf("Extracted sub-agent %q (content length: %d)", name, len(content))
		return InlineSubAgent{Name: name, Content: content}
	})
	if err != nil {
		return "", nil, fmt.Errorf("inline sub-agent end marker should reference a valid sub-agent name: %w", err)
	}
	subAgentLog.Printf("Extraction complete: %d sub-agent(s), main markdown length: %d", len(agents), len(mainMarkdown))
	return mainMarkdown, agents, nil
}

func validateUniqueSubAgentNames(markdown string, allStarts [][]int) error {
	return validateUniqueInlineSectionNames(markdown, allStarts, func(name string) error {
		subAgentLog.Printf("Duplicate sub-agent name: %q", name)
		return NewValidationError(
			"sub-agents",
			name,
			"duplicate name already defined",
			fmt.Sprintf("Rename one of the duplicate sub-agents or remove the extra `%s` definition.", name),
		)
	})
}
