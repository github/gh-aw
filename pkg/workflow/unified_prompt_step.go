package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var unifiedPromptLog = logger.New("workflow:unified_prompt_step")

// PromptSection represents a section of prompt text to be appended
type PromptSection struct {
	// Content is the actual prompt text or a reference to a file
	Content string
	// IsFile indicates if Content is a filename (true) or inline text (false)
	IsFile bool
	// ShellCondition is an optional bash condition (without 'if' keyword) to wrap this section
	// Example: "${{ github.event_name == 'issue_comment' }}" becomes a shell condition
	ShellCondition string
	// EnvVars contains environment variables needed for expressions in this section
	EnvVars map[string]string
}

// generateUnifiedPromptStep generates a single workflow step that appends all prompt sections.
// This consolidates what used to be multiple separate steps (temp folder, playwright, safe outputs,
// GitHub context, PR context, cache memory, repo memory) into one step.
func (c *Compiler) generateUnifiedPromptStep(yaml *strings.Builder, data *WorkflowData) {
	unifiedPromptLog.Print("Generating unified prompt step")

	// Get the heredoc delimiter for consistent usage
	delimiter := GenerateHeredocDelimiter("PROMPT")

	// Collect all prompt sections in order
	sections := c.collectPromptSections(data)

	if len(sections) == 0 {
		unifiedPromptLog.Print("No prompt sections to append, skipping unified step")
		return
	}

	unifiedPromptLog.Printf("Collected %d prompt sections", len(sections))

	// Collect all environment variables from all sections
	// Only include GitHub Actions expressions in the prompt creation step
	// Static values should only be in the substitution step
	allEnvVars := make(map[string]string)
	for _, section := range sections {
		for key, value := range section.EnvVars {
			// Only add GitHub Actions expressions to the prompt creation step
			// Static values (not wrapped in ${{ }}) are for the substitution step only
			if strings.HasPrefix(value, "${{ ") && strings.HasSuffix(value, " }}") {
				allEnvVars[key] = value
			}
		}
	}

	// Generate the step
	yaml.WriteString("      - name: Create prompt with built-in context\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt\n")

	// Add all environment variables in sorted order for consistency
	var envKeys []string
	for key := range allEnvVars {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		fmt.Fprintf(yaml, "          %s: %s\n", key, allEnvVars[key])
	}

	yaml.WriteString("        run: |\n")

	// Track if we're inside a heredoc
	inHeredoc := false

	// Write each section's content
	for i, section := range sections {
		unifiedPromptLog.Printf("Writing section %d/%d: hasCondition=%v, isFile=%v",
			i+1, len(sections), section.ShellCondition != "", section.IsFile)

		if section.ShellCondition != "" {
			// Close heredoc if open, add conditional
			if inHeredoc {
				yaml.WriteString("          " + delimiter + "\n")
				inHeredoc = false
			}
			fmt.Fprintf(yaml, "          if %s; then\n", section.ShellCondition)

			if section.IsFile {
				// File reference inside conditional
				promptPath := fmt.Sprintf("%s/%s", promptsDir, section.Content)
				yaml.WriteString("            " + fmt.Sprintf("cat \"%s\" >> \"$GH_AW_PROMPT\"\n", promptPath))
			} else {
				// Inline content inside conditional - open heredoc, write content, close
				yaml.WriteString("            cat << '" + delimiter + "' >> \"$GH_AW_PROMPT\"\n")
				normalizedContent := normalizeLeadingWhitespace(section.Content)
				cleanedContent := removeConsecutiveEmptyLines(normalizedContent)
				contentLines := strings.Split(cleanedContent, "\n")
				for _, line := range contentLines {
					yaml.WriteString("            " + line + "\n")
				}
				yaml.WriteString("            " + delimiter + "\n")
			}

			yaml.WriteString("          fi\n")
		} else {
			// Unconditional section
			if section.IsFile {
				// Close heredoc if open
				if inHeredoc {
					yaml.WriteString("          " + delimiter + "\n")
					inHeredoc = false
				}
				// Cat the file
				promptPath := fmt.Sprintf("%s/%s", promptsDir, section.Content)
				yaml.WriteString("          " + fmt.Sprintf("cat \"%s\" >> \"$GH_AW_PROMPT\"\n", promptPath))
			} else {
				// Inline content - open heredoc if not already open
				if !inHeredoc {
					yaml.WriteString("          cat << '" + delimiter + "' >> \"$GH_AW_PROMPT\"\n")
					inHeredoc = true
				}
				// Write content directly to open heredoc
				normalizedContent := normalizeLeadingWhitespace(section.Content)
				cleanedContent := removeConsecutiveEmptyLines(normalizedContent)
				contentLines := strings.Split(cleanedContent, "\n")
				for _, line := range contentLines {
					yaml.WriteString("          " + line + "\n")
				}
			}
		}
	}

	// Close heredoc if still open
	if inHeredoc {
		yaml.WriteString("          " + delimiter + "\n")
	}

	unifiedPromptLog.Print("Unified prompt step generated successfully")
}

// normalizeLeadingWhitespace removes consistent leading whitespace from all lines
// This handles content that was generated with indentation for heredocs
func normalizeLeadingWhitespace(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}

	// Find minimum leading whitespace (excluding empty lines)
	minLeadingSpaces := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue // Skip empty lines
		}
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		if minLeadingSpaces == -1 || leadingSpaces < minLeadingSpaces {
			minLeadingSpaces = leadingSpaces
		}
	}

	// If no content or no leading spaces, return as-is
	if minLeadingSpaces <= 0 {
		return content
	}

	// Remove the minimum leading whitespace from all lines
	var result strings.Builder
	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}
		if strings.TrimSpace(line) == "" {
			// Keep empty lines as empty
			result.WriteString("")
		} else if len(line) >= minLeadingSpaces {
			// Remove leading whitespace
			result.WriteString(line[minLeadingSpaces:])
		} else {
			result.WriteString(line)
		}
	}

	return result.String()
}

// removeConsecutiveEmptyLines removes consecutive empty lines, keeping only one
func removeConsecutiveEmptyLines(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}

	var result []string
	lastWasEmpty := false

	for _, line := range lines {
		isEmpty := strings.TrimSpace(line) == ""

		if isEmpty {
			// Only add if the last line wasn't empty
			if !lastWasEmpty {
				result = append(result, line)
				lastWasEmpty = true
			}
			// Skip consecutive empty lines
		} else {
			result = append(result, line)
			lastWasEmpty = false
		}
	}

	return strings.Join(result, "\n")
}

// collectPromptSections collects all prompt sections in the order they should be appended
func (c *Compiler) collectPromptSections(data *WorkflowData) []PromptSection {
	var sections []PromptSection

	// 1. Temporary folder instructions (always included)
	unifiedPromptLog.Print("Adding temp folder section")
	sections = append(sections, PromptSection{
		Content: tempFolderPromptFile,
		IsFile:  true,
	})

	// 2. Markdown generation instructions (always included)
	unifiedPromptLog.Print("Adding markdown section")
	sections = append(sections, PromptSection{
		Content: markdownPromptFile,
		IsFile:  true,
	})

	// 3. Playwright instructions (if playwright tool is enabled)
	if hasPlaywrightTool(data.ParsedTools) {
		unifiedPromptLog.Print("Adding playwright section")
		sections = append(sections, PromptSection{
			Content: playwrightPromptFile,
			IsFile:  true,
		})
	}

	// 4. Trial mode note (if in trial mode)
	if c.trialMode {
		unifiedPromptLog.Print("Adding trial mode section")
		trialContent := fmt.Sprintf("## Note\nThis workflow is running in directory $GITHUB_WORKSPACE, but that directory actually contains the contents of the repository '%s'.", c.trialLogicalRepoSlug)
		sections = append(sections, PromptSection{
			Content: trialContent,
			IsFile:  false,
		})
	}

	// 5. Cache memory instructions (if enabled)
	if data.CacheMemoryConfig != nil && len(data.CacheMemoryConfig.Caches) > 0 {
		unifiedPromptLog.Printf("Adding cache memory section: caches=%d", len(data.CacheMemoryConfig.Caches))
		section := buildCacheMemoryPromptSection(data.CacheMemoryConfig)
		if section != nil {
			sections = append(sections, *section)
		}
	}

	// 6. Repo memory instructions (if enabled)
	if data.RepoMemoryConfig != nil && len(data.RepoMemoryConfig.Memories) > 0 {
		unifiedPromptLog.Printf("Adding repo memory section: memories=%d", len(data.RepoMemoryConfig.Memories))
		var repoMemContent strings.Builder
		generateRepoMemoryPromptSection(&repoMemContent, data.RepoMemoryConfig)
		sections = append(sections, PromptSection{
			Content: repoMemContent.String(),
			IsFile:  false,
		})
	}

	// 7. Safe outputs instructions (if enabled)
	if HasSafeOutputsEnabled(data.SafeOutputs) {
		unifiedPromptLog.Print("Adding safe outputs section")
		safeOutputsContent := `<safe-outputs>
<description>GitHub API Access Instructions</description>
<important>
The gh CLI is NOT authenticated. Do NOT use gh commands for GitHub operations.
</important>
<instructions>
To create or modify GitHub resources (issues, discussions, pull requests, etc.), you MUST call the appropriate safe output tool. Simply writing content will NOT work - the workflow requires actual tool calls.

Discover available tools from the safeoutputs MCP server.

**Critical**: Tool calls write structured data that downstream jobs process. Without tool calls, follow-up actions will be skipped.

**Note**: If you made no other safe output tool calls during this workflow execution, call the "noop" tool to provide a status message indicating completion or that no actions were needed.
</instructions>
</safe-outputs>`
		sections = append(sections, PromptSection{
			Content: safeOutputsContent,
			IsFile:  false,
		})
	}

	// 8. GitHub context (if GitHub tool is enabled)
	if hasGitHubTool(data.ParsedTools) {
		unifiedPromptLog.Print("Adding GitHub context section")
		// Extract expressions from GitHub context prompt
		extractor := NewExpressionExtractor()
		expressionMappings, err := extractor.ExtractExpressions(githubContextPromptText)
		if err == nil && len(expressionMappings) > 0 {
			// Replace expressions with environment variable references
			modifiedPromptText := extractor.ReplaceExpressionsWithEnvVars(githubContextPromptText)

			// Build environment variables map
			envVars := make(map[string]string)
			for _, mapping := range expressionMappings {
				envVars[mapping.EnvVar] = fmt.Sprintf("${{ %s }}", mapping.Content)
			}

			sections = append(sections, PromptSection{
				Content: modifiedPromptText,
				IsFile:  false,
				EnvVars: envVars,
			})
		}
	}

	// 9. PR context (if comment-related triggers and checkout is needed)
	hasCommentTriggers := c.hasCommentRelatedTriggers(data)
	needsCheckout := c.shouldAddCheckoutStep(data)
	permParser := NewPermissionsParser(data.Permissions)
	hasContentsRead := permParser.HasContentsReadAccess()

	if hasCommentTriggers && needsCheckout && hasContentsRead {
		unifiedPromptLog.Print("Adding PR context section with condition")
		// Use shell condition for PR comment detection
		// This checks for issue_comment, pull_request_review_comment, or pull_request_review events
		// For issue_comment, we also need to check if it's on a PR (github.event.issue.pull_request != null)
		// However, for simplicity in the unified step, we'll add an environment variable to check this
		shellCondition := `[ "$GITHUB_EVENT_NAME" = "issue_comment" ] && [ -n "$GH_AW_IS_PR_COMMENT" ] || [ "$GITHUB_EVENT_NAME" = "pull_request_review_comment" ] || [ "$GITHUB_EVENT_NAME" = "pull_request_review" ]`

		// Add environment variable to check if issue_comment is on a PR
		envVars := map[string]string{
			"GH_AW_IS_PR_COMMENT": "${{ github.event.issue.pull_request && 'true' || '' }}",
		}

		sections = append(sections, PromptSection{
			Content:        prContextPromptFile,
			IsFile:         true,
			ShellCondition: shellCondition,
			EnvVars:        envVars,
		})
	}

	return sections
}

// generateUnifiedPromptCreationStep generates a single workflow step (or multiple if needed) that creates
// the complete prompt file with built-in context instructions prepended to the user prompt content.
//
// This consolidates the prompt creation process:
// 1. Built-in context instructions (temp folder, playwright, safe outputs, etc.) - PREPENDED
// 2. User prompt content from markdown - APPENDED
//
// The function handles chunking for large content and ensures proper environment variable handling.
func (c *Compiler) generateUnifiedPromptCreationStep(yaml *strings.Builder, builtinSections []PromptSection, userPromptChunks []string, expressionMappings []*ExpressionMapping, data *WorkflowData) {
	unifiedPromptLog.Print("Generating unified prompt creation step")
	unifiedPromptLog.Printf("Built-in sections: %d, User prompt chunks: %d", len(builtinSections), len(userPromptChunks))

	// Get the heredoc delimiter for consistent usage
	delimiter := GenerateHeredocDelimiter("PROMPT")

	// Collect all environment variables from built-in sections and user prompt expressions
	allEnvVars := make(map[string]string)

	// Also collect all expression mappings for the substitution step (using a map to avoid duplicates)
	expressionMappingsMap := make(map[string]*ExpressionMapping)

	// Add environment variables and expression mappings from built-in sections
	for _, section := range builtinSections {
		for key, value := range section.EnvVars {
			// Extract the GitHub expression from the value (e.g., "${{ github.repository }}" -> "github.repository")
			// This is needed for the substitution step
			if strings.HasPrefix(value, "${{ ") && strings.HasSuffix(value, " }}") {
				content := strings.TrimSpace(value[4 : len(value)-3])
				// Add to both allEnvVars (for prompt creation step) and expressionMappingsMap (for substitution step)
				allEnvVars[key] = value
				// Only add if not already present (user prompt expressions take precedence)
				if _, exists := expressionMappingsMap[key]; !exists {
					expressionMappingsMap[key] = &ExpressionMapping{
						EnvVar:  key,
						Content: content,
					}
				}
			} else {
				// For static values (not GitHub Actions expressions), only add to expressionMappingsMap
				// This ensures they're only available in the substitution step, not the prompt creation step
				if _, exists := expressionMappingsMap[key]; !exists {
					expressionMappingsMap[key] = &ExpressionMapping{
						EnvVar:  key,
						Content: fmt.Sprintf("'%s'", value), // Wrap in quotes for substitution
					}
				}
			}
		}
	}

	// Add environment variables from user prompt expressions (these override built-in ones)
	for _, mapping := range expressionMappings {
		allEnvVars[mapping.EnvVar] = fmt.Sprintf("${{ %s }}", mapping.Content)
		expressionMappingsMap[mapping.EnvVar] = mapping
	}

	// Convert map back to slice for the substitution step
	allExpressionMappings := make([]*ExpressionMapping, 0, len(expressionMappingsMap))

	// Sort the keys to ensure stable output
	sortedKeys := make([]string, 0, len(expressionMappingsMap))
	for key := range expressionMappingsMap {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	// Add mappings in sorted order
	for _, key := range sortedKeys {
		allExpressionMappings = append(allExpressionMappings, expressionMappingsMap[key])
	}

	// Generate the step with all environment variables
	yaml.WriteString("      - name: Create prompt with built-in context\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt\n")

	if data.SafeOutputs != nil {
		yaml.WriteString("          GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}\n")
	}

	// Add all environment variables in sorted order for consistency
	var envKeys []string
	for key := range allEnvVars {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		fmt.Fprintf(yaml, "          %s: %s\n", key, allEnvVars[key])
	}

	yaml.WriteString("        run: |\n")
	yaml.WriteString("          bash /opt/gh-aw/actions/create_prompt_first.sh\n")

	// Track if we're inside a heredoc and whether we're writing the first content
	inHeredoc := false
	isFirstContent := true

	// 1. Write built-in sections first (prepended), wrapped in <system> tags
	if len(builtinSections) > 0 {
		// Open system tag for built-in prompts
		if isFirstContent {
			yaml.WriteString("          cat << '" + delimiter + "' > \"$GH_AW_PROMPT\"\n")
			isFirstContent = false
		} else {
			yaml.WriteString("          cat << '" + delimiter + "' >> \"$GH_AW_PROMPT\"\n")
		}
		yaml.WriteString("          <system>\n")
		yaml.WriteString("          " + delimiter + "\n")
	}

	for i, section := range builtinSections {
		unifiedPromptLog.Printf("Writing built-in section %d/%d: hasCondition=%v, isFile=%v",
			i+1, len(builtinSections), section.ShellCondition != "", section.IsFile)

		if section.ShellCondition != "" {
			// Close heredoc if open, add conditional
			if inHeredoc {
				yaml.WriteString("          " + delimiter + "\n")
				inHeredoc = false
			}
			fmt.Fprintf(yaml, "          if %s; then\n", section.ShellCondition)

			if section.IsFile {
				// File reference inside conditional
				promptPath := fmt.Sprintf("%s/%s", promptsDir, section.Content)
				if isFirstContent {
					yaml.WriteString("            " + fmt.Sprintf("cat \"%s\" > \"$GH_AW_PROMPT\"\n", promptPath))
					isFirstContent = false
				} else {
					yaml.WriteString("            " + fmt.Sprintf("cat \"%s\" >> \"$GH_AW_PROMPT\"\n", promptPath))
				}
			} else {
				// Inline content inside conditional - open heredoc, write content, close
				if isFirstContent {
					yaml.WriteString("            cat << '" + delimiter + "' > \"$GH_AW_PROMPT\"\n")
					isFirstContent = false
				} else {
					yaml.WriteString("            cat << '" + delimiter + "' >> \"$GH_AW_PROMPT\"\n")
				}
				normalizedContent := normalizeLeadingWhitespace(section.Content)
				cleanedContent := removeConsecutiveEmptyLines(normalizedContent)
				contentLines := strings.Split(cleanedContent, "\n")
				for _, line := range contentLines {
					yaml.WriteString("            " + line + "\n")
				}
				yaml.WriteString("            " + delimiter + "\n")
			}

			yaml.WriteString("          fi\n")
		} else {
			// Unconditional section
			if section.IsFile {
				// Close heredoc if open
				if inHeredoc {
					yaml.WriteString("          " + delimiter + "\n")
					inHeredoc = false
				}
				// Cat the file
				promptPath := fmt.Sprintf("%s/%s", promptsDir, section.Content)
				if isFirstContent {
					yaml.WriteString("          " + fmt.Sprintf("cat \"%s\" > \"$GH_AW_PROMPT\"\n", promptPath))
					isFirstContent = false
				} else {
					yaml.WriteString("          " + fmt.Sprintf("cat \"%s\" >> \"$GH_AW_PROMPT\"\n", promptPath))
				}
			} else {
				// Inline content - open heredoc if not already open
				if !inHeredoc {
					if isFirstContent {
						yaml.WriteString("          cat << '" + delimiter + "' > \"$GH_AW_PROMPT\"\n")
						isFirstContent = false
					} else {
						yaml.WriteString("          cat << '" + delimiter + "' >> \"$GH_AW_PROMPT\"\n")
					}
					inHeredoc = true
				}
				// Write content directly to open heredoc
				normalizedContent := normalizeLeadingWhitespace(section.Content)
				cleanedContent := removeConsecutiveEmptyLines(normalizedContent)
				contentLines := strings.Split(cleanedContent, "\n")
				for _, line := range contentLines {
					yaml.WriteString("          " + line + "\n")
				}
			}
		}
	}

	// Close system tag for built-in prompts
	if len(builtinSections) > 0 {
		// Close heredoc if open
		if inHeredoc {
			yaml.WriteString("          " + delimiter + "\n")
			inHeredoc = false
		}
		yaml.WriteString("          cat << '" + delimiter + "' >> \"$GH_AW_PROMPT\"\n")
		yaml.WriteString("          </system>\n")
		yaml.WriteString("          " + delimiter + "\n")
	}

	// 2. Write user prompt chunks (appended after built-in sections)
	for chunkIdx, chunk := range userPromptChunks {
		unifiedPromptLog.Printf("Writing user prompt chunk %d/%d", chunkIdx+1, len(userPromptChunks))

		// Check if this chunk is a runtime-import macro
		if strings.HasPrefix(chunk, "{{#runtime-import ") && strings.HasSuffix(chunk, "}}") {
			// This is a runtime-import macro - write it using heredoc for safe escaping
			unifiedPromptLog.Print("Detected runtime-import macro, writing directly")

			// Close heredoc if open before writing runtime-import macro
			if inHeredoc {
				yaml.WriteString("          " + delimiter + "\n")
				inHeredoc = false
			}

			// Write the macro directly with proper indentation
			// Write the macro using a heredoc to avoid potential escaping issues
			if isFirstContent {
				yaml.WriteString("          cat << '" + delimiter + "' > \"$GH_AW_PROMPT\"\n")
				isFirstContent = false
			} else {
				yaml.WriteString("          cat << '" + delimiter + "' >> \"$GH_AW_PROMPT\"\n")
			}
			yaml.WriteString("          " + chunk + "\n")
			yaml.WriteString("          " + delimiter + "\n")
			continue
		}

		// Regular chunk - close heredoc if open before starting new chunk
		if inHeredoc {
			yaml.WriteString("          " + delimiter + "\n")
			inHeredoc = false
		}

		// Each user prompt chunk is written as a separate heredoc append
		if isFirstContent {
			yaml.WriteString("          cat << '" + delimiter + "' > \"$GH_AW_PROMPT\"\n")
			isFirstContent = false
		} else {
			yaml.WriteString("          cat << '" + delimiter + "' >> \"$GH_AW_PROMPT\"\n")
		}

		lines := strings.Split(chunk, "\n")
		for _, line := range lines {
			yaml.WriteString("          ")
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
		yaml.WriteString("          " + delimiter + "\n")
	}

	// Close heredoc if still open
	if inHeredoc {
		yaml.WriteString("          " + delimiter + "\n")
	}

	// Generate JavaScript-based placeholder substitution step (replaces multiple sed calls)
	// This handles both built-in section expressions and user prompt expressions
	if len(allExpressionMappings) > 0 {
		generatePlaceholderSubstitutionStep(yaml, allExpressionMappings, "      ")
	}

	unifiedPromptLog.Print("Unified prompt creation step generated successfully")
}
