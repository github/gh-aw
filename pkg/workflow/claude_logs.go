package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var claudeLogsLog = logger.New("workflow:claude_logs")

// ParseLogMetrics implements engine-specific log parsing for Claude
func (e *ClaudeEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	claudeLogsLog.Printf("Parsing Claude log metrics: %d bytes", len(logContent))
	var metrics LogMetrics
	var maxTokenUsage int

	// First try to parse as JSON array (Claude logs are structured as JSON arrays)
	if strings.TrimSpace(logContent) != "" {
		if resultMetrics := e.parseClaudeJSONLog(logContent, verbose); resultMetrics.TokenUsage > 0 || resultMetrics.EstimatedCost > 0 || resultMetrics.Turns > 0 || len(resultMetrics.ToolCalls) > 0 || len(resultMetrics.ToolSequences) > 0 {
			metrics.TokenUsage = resultMetrics.TokenUsage
			metrics.EstimatedCost = resultMetrics.EstimatedCost
			metrics.Turns = resultMetrics.Turns
			metrics.ToolCalls = resultMetrics.ToolCalls         // Copy tool calls
			metrics.ToolSequences = resultMetrics.ToolSequences // Copy tool sequences
		}
	}

	// Process line by line for error counting and fallback parsing
	lines := strings.Split(logContent, "\n")

	for _, line := range lines {
		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// If we haven't found cost data yet from JSON parsing, try streaming JSON
		if metrics.TokenUsage == 0 || metrics.EstimatedCost == 0 || metrics.Turns == 0 {
			jsonMetrics := ExtractJSONMetrics(line, verbose)
			if jsonMetrics.TokenUsage > 0 || jsonMetrics.EstimatedCost > 0 {
				// Check if this is a Claude result payload with aggregated costs
				if e.isClaudeResultPayload(line) {
					// For Claude result payloads, use the aggregated values directly
					if resultMetrics := e.extractClaudeResultMetrics(line); resultMetrics.TokenUsage > 0 || resultMetrics.EstimatedCost > 0 || resultMetrics.Turns > 0 {
						metrics.TokenUsage = resultMetrics.TokenUsage
						metrics.EstimatedCost = resultMetrics.EstimatedCost
						metrics.Turns = resultMetrics.Turns
					}
				} else {
					// For streaming JSON, keep the maximum token usage found
					if jsonMetrics.TokenUsage > maxTokenUsage {
						maxTokenUsage = jsonMetrics.TokenUsage
					}
					if metrics.EstimatedCost == 0 && jsonMetrics.EstimatedCost > 0 {
						metrics.EstimatedCost += jsonMetrics.EstimatedCost
					}
				}
				continue
			}
		}
	}

	// If no result payload was found, use the maximum from streaming JSON
	if metrics.TokenUsage == 0 {
		metrics.TokenUsage = maxTokenUsage
	}

	claudeLogsLog.Printf("Parsed log metrics: tokens=%d, cost=$%.4f, turns=%d", metrics.TokenUsage, metrics.EstimatedCost, metrics.Turns)
	return metrics
}

// isClaudeResultPayload checks if the JSON line is a Claude result payload with type: "result"
func (e *ClaudeEngine) isClaudeResultPayload(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return false
	}

	var jsonData map[string]any
	if err := json.Unmarshal([]byte(trimmed), &jsonData); err != nil {
		return false
	}

	typeField, exists := jsonData["type"]
	if !exists {
		return false
	}

	typeStr, ok := typeField.(string)
	return ok && typeStr == "result"
}

// extractClaudeResultMetrics extracts metrics from Claude result payload
func (e *ClaudeEngine) extractClaudeResultMetrics(line string) LogMetrics {
	var metrics LogMetrics

	trimmed := strings.TrimSpace(line)
	var jsonData map[string]any
	if err := json.Unmarshal([]byte(trimmed), &jsonData); err != nil {
		return metrics
	}

	// Extract total_cost_usd directly
	if totalCost, exists := jsonData["total_cost_usd"]; exists {
		if cost := ConvertToFloat(totalCost); cost > 0 {
			metrics.EstimatedCost = cost
		}
	}

	// Extract usage information with all token types
	if usage, exists := jsonData["usage"]; exists {
		if usageMap, ok := usage.(map[string]any); ok {
			inputTokens := ConvertToInt(usageMap["input_tokens"])
			outputTokens := ConvertToInt(usageMap["output_tokens"])
			cacheCreationTokens := ConvertToInt(usageMap["cache_creation_input_tokens"])
			cacheReadTokens := ConvertToInt(usageMap["cache_read_input_tokens"])

			totalTokens := inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens
			if totalTokens > 0 {
				metrics.TokenUsage = totalTokens
			}
		}
	}

	// Extract number of turns
	if numTurns, exists := jsonData["num_turns"]; exists {
		if turns := ConvertToInt(numTurns); turns > 0 {
			metrics.Turns = turns
		}
	}

	// Note: Duration extraction is handled in the main parsing logic where we have access to tool calls
	// This is because we need to distribute duration among tool calls

	return metrics
}

// parseClaudeJSONLog parses Claude logs as a JSON array or mixed format (debug logs + JSONL)
func (e *ClaudeEngine) parseClaudeJSONLog(logContent string, verbose bool) LogMetrics {
	claudeLogsLog.Print("Attempting to parse Claude JSON log")
	var metrics LogMetrics

	// Try to parse the entire log as a JSON array first (old format)
	var logEntries []map[string]any
	if err := json.Unmarshal([]byte(logContent), &logEntries); err != nil {
		// If that fails, try to parse as mixed format (debug logs + JSONL)
		claudeLogsLog.Print("JSON array parse failed, trying JSONL format")
		if verbose {
			fmt.Fprintf(os.Stderr, "Failed to parse Claude log as JSON array, trying JSONL format: %v\n", err)
		}

		logEntries = []map[string]any{}
		lines := strings.Split(logContent, "\n")

		for i := 0; i < len(lines); i++ {
			line := lines[i]
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine == "" {
				continue // Skip empty lines
			}

			// If a line looks like a JSON array (starts with '['), try to parse it as an array
			if strings.HasPrefix(trimmedLine, "[") {
				buf := trimmedLine
				// If the closing bracket is not on the same line, accumulate subsequent lines
				if !strings.Contains(trimmedLine, "]") {
					j := i + 1
					for j < len(lines) {
						buf += "\n" + lines[j]
						if strings.Contains(lines[j], "]") {
							// Advance outer loop to the line we consumed
							i = j
							break
						}
						j++
					}
				}

				var arr []map[string]any
				if err := json.Unmarshal([]byte(buf), &arr); err == nil {
					logEntries = append(logEntries, arr...)
					continue
				}

				// If parsing as a single-line or multi-line array failed, attempt to extract a JSON array substring
				openIdx := strings.Index(buf, "[")
				closeIdx := strings.LastIndex(buf, "]")
				if openIdx != -1 && closeIdx != -1 && closeIdx > openIdx {
					sub := buf[openIdx : closeIdx+1]
					var arr2 []map[string]any
					if err2 := json.Unmarshal([]byte(sub), &arr2); err2 == nil {
						logEntries = append(logEntries, arr2...)
						continue
					}
				}
			}

			// Skip debug log lines that don't start with '{'
			if !strings.HasPrefix(trimmedLine, "{") {
				continue
			}

			// Try to parse each line as JSON
			var jsonEntry map[string]any
			if err := json.Unmarshal([]byte(trimmedLine), &jsonEntry); err != nil {
				// Skip invalid JSON lines (could be partial debug output)
				if verbose {
					fmt.Fprintf(os.Stderr, "Skipping invalid JSON line: %s\n", trimmedLine)
				}
				continue
			}

			logEntries = append(logEntries, jsonEntry)
		}

		if len(logEntries) == 0 {
			if verbose {
				fmt.Fprintf(os.Stderr, "No valid JSON entries found in Claude log\n")
			}
			return metrics
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "Extracted %d JSON entries from mixed format Claude log\n", len(logEntries))
		}
	}

	// Look for the result entry with type: "result"
	toolCallMap := make(map[string]*ToolCallInfo) // Track tool calls across entries
	var currentSequence []string                  // Track tool sequence within current context

	for _, entry := range logEntries {
		if entryType, exists := entry["type"]; exists {
			if typeStr, ok := entryType.(string); ok && typeStr == "result" {
				// Found the result payload, extract cost and token data
				if totalCost, exists := entry["total_cost_usd"]; exists {
					if cost := ConvertToFloat(totalCost); cost > 0 {
						metrics.EstimatedCost = cost
					}
				}

				// Extract usage information with all token types
				if usage, exists := entry["usage"]; exists {
					if usageMap, ok := usage.(map[string]any); ok {
						inputTokens := ConvertToInt(usageMap["input_tokens"])
						outputTokens := ConvertToInt(usageMap["output_tokens"])
						cacheCreationTokens := ConvertToInt(usageMap["cache_creation_input_tokens"])
						cacheReadTokens := ConvertToInt(usageMap["cache_read_input_tokens"])

						totalTokens := inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens
						if totalTokens > 0 {
							metrics.TokenUsage = totalTokens
						}
					}
				}

				// Extract number of turns
				if numTurns, exists := entry["num_turns"]; exists {
					if turns := ConvertToInt(numTurns); turns > 0 {
						metrics.Turns = turns
					}
				}

				// Extract duration information and distribute to tool calls
				if durationMs, exists := entry["duration_ms"]; exists {
					if duration := ConvertToFloat(durationMs); duration > 0 {
						totalDuration := time.Duration(duration * float64(time.Millisecond))
						// Distribute the total duration among tool calls
						// Since we don't have per-tool timing, we approximate by using the total duration
						// as the maximum duration for all tools that don't have duration set yet
						e.distributeTotalDurationToToolCalls(toolCallMap, totalDuration)
					}
				}

				if verbose {
					fmt.Fprintf(os.Stderr, "Extracted from Claude result payload: tokens=%d, cost=%.4f, turns=%d\n",
						metrics.TokenUsage, metrics.EstimatedCost, metrics.Turns)
				}
				break
			} else if typeStr == "assistant" {
				// Parse tool_use entries for tool call statistics and sequence
				if message, exists := entry["message"]; exists {
					if messageMap, ok := message.(map[string]any); ok {
						if content, exists := messageMap["content"]; exists {
							if contentArray, ok := content.([]any); ok {
								sequenceInMessage := e.parseToolCallsWithSequence(contentArray, toolCallMap)
								if len(sequenceInMessage) > 0 {
									currentSequence = append(currentSequence, sequenceInMessage...)
								}
							}
						}
					}
				}
			}
		}

		// Parse tool results from user entries for output sizes
		if entry["type"] == "user" {
			if message, exists := entry["message"]; exists {
				if messageMap, ok := message.(map[string]any); ok {
					if content, exists := messageMap["content"]; exists {
						if contentArray, ok := content.([]any); ok {
							e.parseToolCalls(contentArray, toolCallMap)
						}
					}
				}
			}
		}
	}

	// Finalize tool calls and sequences using shared helper
	FinalizeToolCallsAndSequence(&metrics, toolCallMap, currentSequence)

	if verbose && len(metrics.ToolSequences) > 0 {
		totalTools := 0
		for _, seq := range metrics.ToolSequences {
			totalTools += len(seq)
		}
		fmt.Fprintf(os.Stderr, "Claude parser extracted %d tool sequences with %d total tool calls\n",
			len(metrics.ToolSequences), totalTools)
	}

	return metrics
}

// parseToolCallsWithSequence extracts tool call information from Claude log content array and returns sequence
func (e *ClaudeEngine) parseToolCallsWithSequence(contentArray []any, toolCallMap map[string]*ToolCallInfo) []string {
	var sequence []string

	for _, contentItem := range contentArray {
		if contentMap, ok := contentItem.(map[string]any); ok {
			if contentType, exists := contentMap["type"]; exists {
				if typeStr, ok := contentType.(string); ok {
					switch typeStr {
					case "tool_use":
						// Extract tool name
						if toolName, exists := contentMap["name"]; exists {
							if nameStr, ok := toolName.(string); ok {
								// Skip internal tools as per existing JavaScript logic (disabled for tool graph visualization)
								// internalTools := []string{
								//	"Read", "Write", "Edit", "MultiEdit", "LS", "Grep", "Glob", "TodoWrite",
								// }
								// if slices.Contains(internalTools, nameStr) {
								//	continue
								// }

								// Prettify tool name
								prettifiedName := PrettifyToolName(nameStr)

								// Special handling for bash - each invocation is unique
								if nameStr == "Bash" {
									if input, exists := contentMap["input"]; exists {
										if inputMap, ok := input.(map[string]any); ok {
											if command, exists := inputMap["command"]; exists {
												if commandStr, ok := command.(string); ok {
													// Create unique bash entry with command info, avoiding colons
													uniqueBashName := fmt.Sprintf("bash_%s", ShortenCommand(commandStr))
													prettifiedName = uniqueBashName
												}
											}
										}
									}
								}

								// Add to sequence
								sequence = append(sequence, prettifiedName)

								// Calculate input size from the input field
								inputSize := 0
								if input, exists := contentMap["input"]; exists {
									inputSize = e.estimateInputSize(input)
								}

								// Initialize or update tool call info
								if toolInfo, exists := toolCallMap[prettifiedName]; exists {
									toolInfo.CallCount++
									if inputSize > toolInfo.MaxInputSize {
										toolInfo.MaxInputSize = inputSize
									}
								} else {
									toolCallMap[prettifiedName] = &ToolCallInfo{
										Name:          prettifiedName,
										CallCount:     1,
										MaxInputSize:  inputSize,
										MaxOutputSize: 0, // Will be updated when we find tool results
										MaxDuration:   0, // Will be updated when we find execution timing
									}
								}
							}
						}
					case "tool_result":
						// Extract output size for tool results
						if content, exists := contentMap["content"]; exists {
							if contentStr, ok := content.(string); ok {
								// Estimate token count (rough approximation: 1 token = ~4 characters)
								outputSize := len(contentStr) / 4

								// Find corresponding tool call to update max output size
								if toolUseID, exists := contentMap["tool_use_id"]; exists {
									if _, ok := toolUseID.(string); ok {
										// This is simplified - in a full implementation we'd track tool_use_id to tool name mapping
										// For now, we'll update the max output size for all tools (conservative estimate)
										for _, toolInfo := range toolCallMap {
											if outputSize > toolInfo.MaxOutputSize {
												toolInfo.MaxOutputSize = outputSize
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return sequence
}

// parseToolCalls extracts tool call information from Claude log content array without sequence tracking
func (e *ClaudeEngine) parseToolCalls(contentArray []any, toolCallMap map[string]*ToolCallInfo) {
	for _, contentItem := range contentArray {
		if contentMap, ok := contentItem.(map[string]any); ok {
			if contentType, exists := contentMap["type"]; exists {
				if typeStr, ok := contentType.(string); ok {
					switch typeStr {
					case "tool_use":
						// Extract tool name
						if toolName, exists := contentMap["name"]; exists {
							if nameStr, ok := toolName.(string); ok {
								// Prettify tool name
								prettifiedName := PrettifyToolName(nameStr)

								// Special handling for bash - each invocation is unique
								if nameStr == "Bash" {
									if input, exists := contentMap["input"]; exists {
										if inputMap, ok := input.(map[string]any); ok {
											if command, exists := inputMap["command"]; exists {
												if commandStr, ok := command.(string); ok {
													// Create unique bash entry with command info, avoiding colons
													uniqueBashName := fmt.Sprintf("bash_%s", ShortenCommand(commandStr))
													prettifiedName = uniqueBashName
												}
											}
										}
									}
								}

								// Calculate input size from the input field
								inputSize := 0
								if input, exists := contentMap["input"]; exists {
									inputSize = e.estimateInputSize(input)
								}

								// Initialize or update tool call info
								if toolInfo, exists := toolCallMap[prettifiedName]; exists {
									toolInfo.CallCount++
									if inputSize > toolInfo.MaxInputSize {
										toolInfo.MaxInputSize = inputSize
									}
								} else {
									toolCallMap[prettifiedName] = &ToolCallInfo{
										Name:          prettifiedName,
										CallCount:     1,
										MaxInputSize:  inputSize,
										MaxOutputSize: 0, // Will be updated when we find tool results
										MaxDuration:   0, // Will be updated when we find execution timing
									}
								}
							}
						}
					case "tool_result":
						// Extract output size for tool results
						if content, exists := contentMap["content"]; exists {
							if contentStr, ok := content.(string); ok {
								// Estimate token count (rough approximation: 1 token = ~4 characters)
								outputSize := len(contentStr) / 4

								// Find corresponding tool call to update max output size
								if toolUseID, exists := contentMap["tool_use_id"]; exists {
									if _, ok := toolUseID.(string); ok {
										// This is simplified - in a full implementation we'd track tool_use_id to tool name mapping
										// For now, we'll update the max output size for all tools (conservative estimate)
										for _, toolInfo := range toolCallMap {
											if outputSize > toolInfo.MaxOutputSize {
												toolInfo.MaxOutputSize = outputSize
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// estimateInputSize estimates the input size in tokens from a tool input object
func (e *ClaudeEngine) estimateInputSize(input any) int {
	// Convert input to JSON string to get approximate size
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return 0
	}
	// Estimate token count (rough approximation: 1 token = ~4 characters)
	return len(inputJSON) / 4
}

// distributeTotalDurationToToolCalls distributes the total workflow duration among tool calls
// Since Claude logs don't provide per-tool timing, we approximate by assigning the total duration
// to all tools that don't have a duration set yet, simulating that they all could have taken this long
func (e *ClaudeEngine) distributeTotalDurationToToolCalls(toolCallMap map[string]*ToolCallInfo, totalDuration time.Duration) {
	// Count tools that don't have duration set yet
	toolsWithoutDuration := 0
	for _, toolInfo := range toolCallMap {
		if toolInfo.MaxDuration == 0 {
			toolsWithoutDuration++
		}
	}

	// If no tools without duration, don't update anything
	if toolsWithoutDuration == 0 {
		return
	}

	// For Claude logs, since we only have total duration, we assign the total duration
	// as the maximum possible duration for each tool. This is conservative but gives
	// users an idea of the overall workflow timing
	for _, toolInfo := range toolCallMap {
		if toolInfo.MaxDuration == 0 {
			toolInfo.MaxDuration = totalDuration
		}
	}
}
