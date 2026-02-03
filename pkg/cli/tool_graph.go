package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var toolGraphLog = logger.New("cli:tool_graph")

// ToolTransition represents an edge in the tool graph
type ToolTransition struct {
	From  string // Source tool name
	To    string // Target tool name
	Count int    // Number of times this transition occurred
}

// ToolGraph represents a directed graph of tool call sequences
type ToolGraph struct {
	Tools       map[string]bool // Set of all tools
	Transitions map[string]int  // Key: "from->to", Value: count
	sequences   [][]string      // Store sequences for analysis
}

// NewToolGraph creates a new empty tool graph
func NewToolGraph() *ToolGraph {
	return &ToolGraph{
		Tools:       make(map[string]bool),
		Transitions: make(map[string]int),
	}
}

// AddSequence adds a tool call sequence to the graph
func (g *ToolGraph) AddSequence(tools []string) {
	if len(tools) == 0 {
		return
	}

	toolGraphLog.Printf("Adding tool sequence: length=%d, tools=%v", len(tools), tools)

	// Add all tools to the set
	for _, tool := range tools {
		g.Tools[tool] = true
	}

	// Add transitions between consecutive tools
	for i := 0; i < len(tools)-1; i++ {
		from := tools[i]
		to := tools[i+1]
		key := fmt.Sprintf("%s->%s", from, to)
		g.Transitions[key]++
	}

	// Store the sequence for analysis
	g.sequences = append(g.sequences, tools)
}

// GenerateMermaidGraph generates a Mermaid state diagram from the tool graph
func (g *ToolGraph) GenerateMermaidGraph() string {
	if len(g.Tools) == 0 {
		toolGraphLog.Print("No tool calls found for Mermaid graph generation")
		return console.FormatInfoMessage("No tool calls found")
	}

	toolGraphLog.Printf("Generating Mermaid graph: tools=%d, transitions=%d", len(g.Tools), len(g.Transitions))
	var sb strings.Builder
	sb.WriteString("```mermaid\n")
	sb.WriteString("stateDiagram-v2\n")

	// Add tool states with normalized names for Mermaid
	toolToStateMap := make(map[string]string)
	var tools []string
	for tool := range g.Tools {
		tools = append(tools, tool)
	}
	sort.Strings(tools)

	for i, tool := range tools {
		stateId := fmt.Sprintf("tool%d", i)
		toolToStateMap[tool] = stateId
		// Escape special characters in tool names for display
		displayName := strings.ReplaceAll(tool, "\"", "\\\"")
		fmt.Fprintf(&sb, "    %s : %s\n", stateId, displayName)
	}

	// Add start state
	sb.WriteString("    [*] --> start_tool : begin\n")

	// Find the most common starting tool(s)
	startCounts := make(map[string]int)
	for _, sequence := range g.sequences {
		if len(sequence) > 0 {
			startCounts[sequence[0]]++
		}
	}

	// Connect start to the most common starting tools
	if len(startCounts) > 0 {
		var startTools []string
		maxCount := 0
		for tool, count := range startCounts {
			if count > maxCount {
				maxCount = count
				startTools = []string{tool}
			} else if count == maxCount {
				startTools = append(startTools, tool)
			}
		}

		for _, tool := range startTools {
			if stateId, exists := toolToStateMap[tool]; exists {
				fmt.Fprintf(&sb, "    start_tool --> %s\n", stateId)
			}
		}
	}

	// Add transitions with counts as labels
	var transitions []ToolTransition
	for key, count := range g.Transitions {
		parts := strings.Split(key, "->")
		if len(parts) == 2 {
			transitions = append(transitions, ToolTransition{
				From:  parts[0],
				To:    parts[1],
				Count: count,
			})
		}
	}

	// Sort transitions by count (descending) for better visualization
	sort.Slice(transitions, func(i, j int) bool {
		if transitions[i].Count != transitions[j].Count {
			return transitions[i].Count > transitions[j].Count
		}
		if transitions[i].From != transitions[j].From {
			return transitions[i].From < transitions[j].From
		}
		return transitions[i].To < transitions[j].To
	})

	for _, transition := range transitions {
		fromState, fromExists := toolToStateMap[transition.From]
		toState, toExists := toolToStateMap[transition.To]

		if fromExists && toExists {
			label := ""
			if transition.Count > 1 {
				label = fmt.Sprintf(" : %dx", transition.Count)
			}
			fmt.Fprintf(&sb, "    %s --> %s%s\n", fromState, toState, label)
		}
	}

	sb.WriteString("```\n")
	return sb.String()
}

// GetSummary returns a summary of the tool graph
func (g *ToolGraph) GetSummary() string {
	if len(g.Tools) == 0 {
		return "No tool sequences found in the logs."
	}

	var sb strings.Builder
	sb.WriteString("🔄 Tool Sequence Graph Summary\n")
	fmt.Fprintf(&sb, "   • %d unique tools\n", len(g.Tools))
	fmt.Fprintf(&sb, "   • %d tool transitions\n", len(g.Transitions))
	fmt.Fprintf(&sb, "   • %d sequences analyzed\n", len(g.sequences))

	// Find most common transitions
	if len(g.Transitions) > 0 {
		var topTransitions []ToolTransition
		for key, count := range g.Transitions {
			parts := strings.Split(key, "->")
			if len(parts) == 2 {
				topTransitions = append(topTransitions, ToolTransition{
					From:  parts[0],
					To:    parts[1],
					Count: count,
				})
			}
		}

		sort.Slice(topTransitions, func(i, j int) bool {
			return topTransitions[i].Count > topTransitions[j].Count
		})

		sb.WriteString("\nMost common tool transitions:\n")
		for i, transition := range topTransitions {
			if i >= 5 { // Show top 5
				break
			}
			fmt.Fprintf(&sb, "   %d. %s → %s (%dx)\n",
				i+1, transition.From, transition.To, transition.Count)
		}
	}

	return sb.String()
}

// generateToolGraph analyzes processed runs and generates a tool sequence graph
func generateToolGraph(processedRuns []ProcessedRun, verbose bool) {
	if len(processedRuns) == 0 {
		toolGraphLog.Print("No processed runs to generate tool graph")
		return
	}

	toolGraphLog.Printf("Generating tool graph from %d processed runs", len(processedRuns))
	graph := NewToolGraph()
	for _, run := range processedRuns {
		sequences := extractToolSequencesFromRun(run, verbose)
		if verbose && len(sequences) > 0 {
			totalTools := 0
			for _, seq := range sequences {
				totalTools += len(seq)
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run %d contributed %d tool sequences with %d total tools",
				run.Run.DatabaseID, len(sequences), totalTools)))
		}
		for _, sequence := range sequences {
			graph.AddSequence(sequence)
		}
	}

	// Generate and display Mermaid graph only
	mermaidGraph := graph.GenerateMermaidGraph()
	fmt.Println(mermaidGraph)
}

// extractToolSequencesFromRun extracts tool call sequences from a single run
func extractToolSequencesFromRun(run ProcessedRun, verbose bool) [][]string {
	var sequences [][]string

	if run.Run.LogsPath == "" {
		return sequences
	}

	// Extract metrics from the run (which now includes tool sequences)
	metrics := ExtractLogMetricsFromRun(run)

	// Use the tool sequences from the metrics if available
	if len(metrics.ToolSequences) > 0 {
		sequences = append(sequences, metrics.ToolSequences...)

		if verbose {
			totalTools := 0
			for _, seq := range metrics.ToolSequences {
				totalTools += len(seq)
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Extracted %d tool sequences with %d total tool calls from run %d",
				len(metrics.ToolSequences), totalTools, run.Run.DatabaseID)))
		}
	} else if len(metrics.ToolCalls) > 0 {
		// Fallback: convert tool calls to a simple sequence if no sequences available
		// This provides some graph data even when sequence extraction fails
		var tools []string
		for _, toolCall := range metrics.ToolCalls {
			// Add each tool based on its call count to approximate sequence
			for i := 0; i < toolCall.CallCount; i++ {
				tools = append(tools, toolCall.Name)
			}
		}

		if len(tools) > 0 {
			sequences = append(sequences, tools)
		}

		if verbose && len(tools) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No tool sequences found, using fallback with %d tool calls from run %d",
				len(tools), run.Run.DatabaseID)))
		}
	}

	return sequences
}
