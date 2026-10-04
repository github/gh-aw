// This file contains rendering primitives and the top-level render function for the
// unified MCP Gateway + AWF firewall + Agent timeline produced by BuildUnifiedTimeline.
//
// A dedicated rendering primitive exists for every TimelineEventKind so that each event
// type is displayed with appropriate context and formatting:
//
//   TimelineKindToolCall           – renderGatewayToolCallRow
//   TimelineKindDIFCFiltered       – renderGatewayDIFCFilteredRow
//   TimelineKindGuardPolicyBlocked – renderGatewayGuardPolicyBlockedRow
//   TimelineKindNetworkAllowed     – renderFirewallNetworkAllowedRow
//   TimelineKindNetworkBlocked     – renderFirewallNetworkBlockedRow
//   TimelineKindAgentTurn          – renderAgentTurnRow
//   TimelineKindAgentToolStart     – renderAgentToolStartRow
//   TimelineKindAgentToolDone      – renderAgentToolDoneRow
//   TimelineKindAssistantMessage   – renderAgentAssistantMessageRow
//   TimelineKindReasoning          – renderAgentReasoningRow
//   TimelineKindSteering           – renderSteeringRow
//
// renderTimelineEventRow dispatches to the appropriate primitive and returns a
// []string suitable for inclusion in a console.TableConfig.Rows slice.

package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

// timelineEventIcon returns a single Unicode icon for each event kind.
// All icons are cross-compatible Unicode symbols that render correctly in all modern terminals.
// Reasoning uses ◐ (half-filled circle) to match the step summary convention for thinking content.
func timelineEventIcon(kind TimelineEventKind) string {
	switch kind {
	case TimelineKindToolCall:
		return "⚙"
	case TimelineKindDIFCFiltered:
		return "⊖"
	case TimelineKindGuardPolicyBlocked:
		return "⊗"
	case TimelineKindNetworkAllowed:
		return "✓"
	case TimelineKindNetworkBlocked:
		return "✗"
	case TimelineKindAgentTurn:
		return "○"
	case TimelineKindAgentToolStart:
		return "▶"
	case TimelineKindAgentToolDone:
		return "■"
	case TimelineKindAssistantMessage:
		return "●"
	case TimelineKindReasoning:
		return "◐"
	case TimelineKindSteering:
		return "⚠"
	default:
		return "·"
	}
}

// timelineEventKindLabel returns a short human-readable label for each event kind.
func timelineEventKindLabel(kind TimelineEventKind) string {
	switch kind {
	case TimelineKindToolCall:
		return "tool_call"
	case TimelineKindDIFCFiltered:
		return "difc_filtered"
	case TimelineKindGuardPolicyBlocked:
		return "guard_blocked"
	case TimelineKindNetworkAllowed:
		return "net_allowed"
	case TimelineKindNetworkBlocked:
		return "net_blocked"
	case TimelineKindAgentTurn:
		return "agent_turn"
	case TimelineKindAgentToolStart:
		return "tool_start"
	case TimelineKindAgentToolDone:
		return "tool_done"
	case TimelineKindAssistantMessage:
		return "assistant_message"
	case TimelineKindReasoning:
		return "reasoning"
	case TimelineKindSteering:
		return "steering"
	default:
		return string(kind)
	}
}

// timelineSourceLabel returns a short (2-char) label for each event source.
func timelineSourceLabel(source TimelineEventSource) string {
	switch source {
	case TimelineSourceGateway:
		return "GW"
	case TimelineSourceFirewall:
		return "FW"
	case TimelineSourceAgent:
		return "AG"
	default:
		s := strings.ToUpper(string(source))
		if len(s) < 2 {
			return s
		}
		return s[:2]
	}
}

// formatTimelineTime formats a timeline event timestamp as HH:MM:SS.mmm (UTC).
// Returns "-" for the zero value.
func formatTimelineTime(evt UnifiedTimelineEvent) string {
	if evt.Time.IsZero() {
		return "-"
	}
	return evt.Time.UTC().Format("15:04:05.000")
}

// ─── Per-kind rendering primitives ───────────────────────────────────────────

// renderGatewayToolCallRow renders a TimelineKindToolCall event as a table row.
//
// Columns: Time | Src | Kind | Detail | Status
//
// Detail encodes the server and tool name (server/tool). Status shows the
// round-trip duration when available, or the status string (success/error).
// An error suffix is appended to Status when the entry carries an error message.
func renderGatewayToolCallRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindToolCall) + " " + timelineEventKindLabel(TimelineKindToolCall)

	tool := evt.ToolName
	if tool == "" {
		tool = evt.Method
	}
	detail := tool
	if evt.ServerName != "" && tool != "" {
		detail = strings.Join([]string{evt.ServerName, tool}, "/")
	} else if evt.ServerName != "" {
		detail = evt.ServerName
	}
	detail = stringutil.Truncate(detail, 45)

	status := evt.Status
	if evt.Duration > 0 {
		status = fmt.Sprintf("%.0fms", evt.Duration)
	}
	if evt.Error != "" {
		errStr := stringutil.Truncate(evt.Error, 25)
		status = "error: " + errStr
	}
	status = stringutil.Truncate(status, 35)

	return []string{ts, src, kind, detail, status}
}

// renderGatewayDIFCFilteredRow renders a TimelineKindDIFCFiltered event as a table row.
//
// Detail shows the server and tool name. Status shows the author login (prefixed "@")
// when available, falling back to a truncated reason string.
func renderGatewayDIFCFilteredRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindDIFCFiltered) + " " + timelineEventKindLabel(TimelineKindDIFCFiltered)

	detail := evt.ToolName
	if evt.ServerName != "" && evt.ToolName != "" {
		detail = strings.Join([]string{evt.ServerName, evt.ToolName}, "/")
	} else if evt.ServerName != "" {
		detail = evt.ServerName
	}
	detail = stringutil.Truncate(detail, 45)

	status := stringutil.Truncate(evt.Reason, 35)
	if evt.AuthorLogin != "" {
		status = "@" + evt.AuthorLogin
	}

	return []string{ts, src, kind, detail, status}
}

// renderGatewayGuardPolicyBlockedRow renders a TimelineKindGuardPolicyBlocked event as a
// table row.
//
// Detail shows the server and tool name. Status shows the reason or error message.
func renderGatewayGuardPolicyBlockedRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindGuardPolicyBlocked) + " " + timelineEventKindLabel(TimelineKindGuardPolicyBlocked)

	detail := evt.ToolName
	if evt.ServerName != "" && evt.ToolName != "" {
		detail = strings.Join([]string{evt.ServerName, evt.ToolName}, "/")
	} else if evt.ServerName != "" {
		detail = evt.ServerName
	}
	detail = stringutil.Truncate(detail, 45)

	status := evt.Reason
	if status == "" {
		status = evt.Error
	}
	status = stringutil.Truncate(status, 35)

	return []string{ts, src, kind, detail, status}
}

// renderFirewallNetworkAllowedRow renders a TimelineKindNetworkAllowed event as a table row.
//
// Detail shows the target host. Status shows the HTTP status code.
func renderFirewallNetworkAllowedRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindNetworkAllowed) + " " + timelineEventKindLabel(TimelineKindNetworkAllowed)
	detail := stringutil.Truncate(evt.Host, 45)

	status := ""
	if evt.HTTPStatus > 0 {
		status = fmt.Sprintf("HTTP %d", evt.HTTPStatus)
	}
	if evt.HTTPMethod != "" {
		status = evt.HTTPMethod + " " + status
	}
	status = strings.TrimSpace(status)
	status = stringutil.Truncate(status, 35)

	return []string{ts, src, kind, detail, status}
}

// renderFirewallNetworkBlockedRow renders a TimelineKindNetworkBlocked event as a table row.
//
// Detail shows the target host. Status shows the HTTP status code or "blocked" when no
// status is available.
func renderFirewallNetworkBlockedRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindNetworkBlocked) + " " + timelineEventKindLabel(TimelineKindNetworkBlocked)
	detail := stringutil.Truncate(evt.Host, 45)

	status := "blocked"
	if evt.HTTPStatus > 0 {
		status = fmt.Sprintf("HTTP %d", evt.HTTPStatus)
	}
	if evt.HTTPMethod != "" {
		status = evt.HTTPMethod + " " + status
	}
	status = strings.TrimSpace(status)
	status = stringutil.Truncate(status, 35)

	return []string{ts, src, kind, detail, status}
}

// renderAgentTurnRow renders a TimelineKindAgentTurn event as a table row.
//
// Columns: Time | Src | Kind | Detail | Status
//
// Detail encodes the 1-based turn number.  Status is left empty since a turn
// marker does not represent a completed operation.
func renderAgentTurnRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindAgentTurn) + " " + timelineEventKindLabel(TimelineKindAgentTurn)
	detail := fmt.Sprintf("turn %d", evt.TurnIndex)
	return []string{ts, src, kind, detail, ""}
}

// renderAgentToolStartRow renders a TimelineKindAgentToolStart event as a table row.
//
// Columns: Time | Src | Kind | Detail | Status
//
// Detail shows "server/tool" (or just "tool" when the server name is absent).
// Status is left empty because execution has not completed yet.
func renderAgentToolStartRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindAgentToolStart) + " " + timelineEventKindLabel(TimelineKindAgentToolStart)
	var detail string
	if evt.ServerName != "" {
		detail = stringutil.Truncate(strings.Join([]string{evt.ServerName, evt.ToolName}, "/"), 48)
	} else {
		detail = stringutil.Truncate(evt.ToolName, 48)
	}
	return []string{ts, src, kind, detail, ""}
}

// renderAgentToolDoneRow renders a TimelineKindAgentToolDone event as a table row.
//
// Columns: Time | Src | Kind | Detail | Status
//
// Detail shows "server/tool" (or just "tool") matching the start event. Status is
// "success" or "error".
func renderAgentToolDoneRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindAgentToolDone) + " " + timelineEventKindLabel(TimelineKindAgentToolDone)
	var detail string
	if evt.ServerName != "" {
		detail = stringutil.Truncate(strings.Join([]string{evt.ServerName, evt.ToolName}, "/"), 48)
	} else {
		detail = stringutil.Truncate(evt.ToolName, 48)
	}
	status := evt.Status
	if status == "" {
		if evt.Success {
			status = "success"
		} else {
			status = "error"
		}
	}
	return []string{ts, src, kind, detail, status}
}

// renderAgentAssistantMessageRow renders a TimelineKindAssistantMessage event as a table row.
//
// Columns: Time | Src | Kind | Detail | Status
//
// Detail shows a truncated preview of the message content. Status is left empty.
func renderAgentAssistantMessageRow(evt UnifiedTimelineEvent) []string {
	return renderMessageContentTimelineRow(TimelineKindAssistantMessage, evt)
}

func renderMessageContentTimelineRow(kind TimelineEventKind, evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kindLabel := timelineEventIcon(kind) + " " + timelineEventKindLabel(kind)
	detail := stringutil.Truncate(evt.MessageContent, 48)
	return []string{ts, src, kindLabel, detail, ""}
}

// renderAgentReasoningRow renders a TimelineKindReasoning event as a table row.
//
// Columns: Time | Src | Kind | Detail | Status
//
// Detail shows a truncated preview of the reasoning content. Status is left empty.
func renderAgentReasoningRow(evt UnifiedTimelineEvent) []string {
	return renderMessageContentTimelineRow(TimelineKindReasoning, evt)
}

// renderSteeringRow renders a TimelineKindSteering event as a table row.
//
// Columns: Time | Src | Kind | Detail | Status
//
// Detail shows a truncated preview of the steering message. Status shows the
// steering type: "token" for token budget warnings, "time" for timeout warnings.
func renderSteeringRow(evt UnifiedTimelineEvent) []string {
	ts := formatTimelineTime(evt)
	src := timelineSourceLabel(evt.Source)
	kind := timelineEventIcon(TimelineKindSteering) + " " + timelineEventKindLabel(TimelineKindSteering)
	detail := stringutil.Truncate(evt.Reason, 48)
	return []string{ts, src, kind, detail, evt.Status}
}

// renderTimelineEventRow dispatches to the appropriate per-kind rendering primitive and
// returns a []string table row with columns: Time | Src | Kind | Detail | Status.
func renderTimelineEventRow(evt UnifiedTimelineEvent) []string {
	switch evt.Kind {
	case TimelineKindToolCall:
		return renderGatewayToolCallRow(evt)
	case TimelineKindDIFCFiltered:
		return renderGatewayDIFCFilteredRow(evt)
	case TimelineKindGuardPolicyBlocked:
		return renderGatewayGuardPolicyBlockedRow(evt)
	case TimelineKindNetworkAllowed:
		return renderFirewallNetworkAllowedRow(evt)
	case TimelineKindNetworkBlocked:
		return renderFirewallNetworkBlockedRow(evt)
	case TimelineKindAgentTurn:
		return renderAgentTurnRow(evt)
	case TimelineKindAgentToolStart:
		return renderAgentToolStartRow(evt)
	case TimelineKindAgentToolDone:
		return renderAgentToolDoneRow(evt)
	case TimelineKindAssistantMessage:
		return renderAgentAssistantMessageRow(evt)
	case TimelineKindReasoning:
		return renderAgentReasoningRow(evt)
	case TimelineKindSteering:
		return renderSteeringRow(evt)
	default:
		// Fallback for any future event kinds not yet handled.
		ts := formatTimelineTime(evt)
		return []string{ts, timelineSourceLabel(evt.Source), string(evt.Kind), "", ""}
	}
}

// ─── Stream renderer ─────────────────────────────────────────────────────────

// streamStyleRenderer is an interface satisfied by lipgloss style objects (and
// any other type with a Render method).  It is used to pass style objects to
// helper functions without importing lipgloss directly in those helpers.
type streamStyleRenderer interface{ Render(strs ...string) string }

// noopStyleRenderer is a streamStyleRenderer that returns its input unchanged.
// It is used in place of a real lipgloss style when output is not a TTY.
type noopStyleRenderer struct{}

func (noopStyleRenderer) Render(strs ...string) string {
	if len(strs) == 0 {
		return ""
	}
	return strs[0]
}

// streamMaxAnnotationLen is the maximum number of runes shown for inline error
// and reason annotations in the stream renderer.
const streamMaxAnnotationLen = 40

// streamMaxMessageLines is the maximum number of lines of message content shown
// in the stream renderer for user/assistant/reasoning messages.
const streamMaxMessageLines = 3

// streamMaxLineLength is the maximum number of runes per line of message content
// shown in the stream renderer.
const streamMaxLineLength = 80

// formatStreamToolDetail returns "server/tool" when both are non-empty, "tool"
// when only the tool name is set, and "server" as a last resort.
func formatStreamToolDetail(serverName, toolName string) string {
	if serverName != "" && toolName != "" {
		return strings.Join([]string{serverName, toolName}, "/")
	}
	if toolName != "" {
		return toolName
	}
	return serverName
}

// renderMessageSnippet returns up to streamMaxMessageLines non-empty lines of text,
// each indented with prefix and styled using the provided renderer.
// A trailing "…" line (styled muted) is appended when content is truncated.
// Returns an empty string when content is blank.
func renderMessageSnippet(content, indent string, lineStyle, truncStyle streamStyleRenderer) string {
	if content == "" {
		return ""
	}
	var sb strings.Builder
	lines := strings.Split(content, "\n")
	shown := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			continue
		}
		if shown >= streamMaxMessageLines {
			sb.WriteString(indent)
			sb.WriteString(truncStyle.Render("…"))
			sb.WriteString("\n")
			break
		}
		sb.WriteString(indent)
		sb.WriteString(lineStyle.Render(stringutil.Truncate(line, streamMaxLineLength)))
		sb.WriteString("\n")
		shown++
	}
	return sb.String()
}

// renderUnifiedTimelineStream renders a merged slice of UnifiedTimelineEvents as a
// flowing, line-by-line stream that simulates watching a live agentic session.
// Agent turns become section headers; tool and network events are indented beneath
// them. No summary statistics or table are produced.
// Returns an empty string when events is empty.
func renderUnifiedTimelineStream(events []UnifiedTimelineEvent) string {
	if len(events) == 0 {
		return ""
	}

	isTerminal := tty.IsStdoutTerminal()

	// streamColor wraps text with a lipgloss style only when output is a TTY so
	// that piped output stays clean of ANSI escape codes.
	streamColor := func(s streamStyleRenderer, text string) string {
		if isTerminal {
			return s.Render(text)
		}
		return text
	}

	// coloredMessageSnippet renders the first few lines of message content.
	// lineStyle and truncStyle are applied only when output is a TTY.
	coloredMessageSnippet := func(content, indent string, lineStyle, truncStyle streamStyleRenderer) string {
		ls, ts := streamStyleRenderer(noopStyleRenderer{}), streamStyleRenderer(noopStyleRenderer{})
		if isTerminal {
			ls, ts = lineStyle, truncStyle
		}
		return renderMessageSnippet(content, indent, ls, ts)
	}

	var sb strings.Builder
	inTurn := false

	for _, evt := range events {
		ts := formatTimelineTime(evt)

		switch evt.Kind {
		case TimelineKindAgentTurn:
			if inTurn {
				sb.WriteString("\n")
			}
			inTurn = true
			// Turn headers are bold purple (Command), timestamp muted.
			turnLabel := streamColor(styles.Command, fmt.Sprintf("> Turn %d", evt.TurnIndex))
			tsLabel := streamColor(styles.LineNumber, "["+ts+"]")
			fmt.Fprintf(&sb, "%s %s\n", turnLabel, tsLabel)
			// Show the first few lines of the user's message in muted style.
			sb.WriteString(coloredMessageSnippet(evt.MessageContent, "  ", styles.ContextLine, styles.LineNumber))

		case TimelineKindAssistantMessage:
			icon := streamColor(styles.Info, timelineEventIcon(TimelineKindAssistantMessage))
			fmt.Fprintf(&sb, "  %s\n", icon)
			// Show assistant response snippet in standard foreground, muted truncation.
			sb.WriteString(coloredMessageSnippet(evt.MessageContent, "  ", styles.ContextLine, styles.LineNumber))

		case TimelineKindReasoning:
			icon := streamColor(styles.Verbose, timelineEventIcon(TimelineKindReasoning))
			fmt.Fprintf(&sb, "  %s\n", icon)
			// Show reasoning snippet in muted/verbose style.
			sb.WriteString(coloredMessageSnippet(evt.MessageContent, "  ", styles.Verbose, styles.LineNumber))

		case TimelineKindAgentToolStart:
			detail := formatStreamToolDetail(evt.ServerName, evt.ToolName)
			// Tool start is yellow progress indicator.
			icon := streamColor(styles.Progress, timelineEventIcon(TimelineKindAgentToolStart))
			fmt.Fprintf(&sb, "  %s %s\n", icon, detail)

		case TimelineKindAgentToolDone:
			detail := formatStreamToolDetail(evt.ServerName, evt.ToolName)
			status := evt.Status
			if status == "" {
				if evt.Success {
					status = "success"
				} else {
					status = "error"
				}
			}
			// Completion icon and status are green on success, red on error.
			if evt.Success || status == "success" {
				icon := streamColor(styles.Success, timelineEventIcon(TimelineKindAgentToolDone))
				statusColored := streamColor(styles.Success, status)
				fmt.Fprintf(&sb, "  %s %s  %s\n", icon, detail, statusColored)
			} else {
				icon := streamColor(styles.Error, timelineEventIcon(TimelineKindAgentToolDone))
				statusColored := streamColor(styles.Error, status)
				fmt.Fprintf(&sb, "  %s %s  %s\n", icon, detail, statusColored)
			}

		case TimelineKindToolCall:
			detail := formatStreamToolDetail(evt.ServerName, evt.ToolName)
			icon := streamColor(styles.ServerName, timelineEventIcon(TimelineKindToolCall))
			suffix := ""
			if evt.Duration > 0 {
				suffix = "  " + streamColor(styles.LineNumber, fmt.Sprintf("%.0fms", evt.Duration))
			} else if evt.Error != "" {
				suffix = "  " + streamColor(styles.Error, "error: "+stringutil.Truncate(evt.Error, streamMaxAnnotationLen))
			}
			fmt.Fprintf(&sb, "    %s %s%s\n", icon, detail, suffix)

		case TimelineKindNetworkAllowed:
			method := ""
			if evt.HTTPMethod != "" {
				method = "  " + streamColor(styles.LineNumber, evt.HTTPMethod)
			}
			icon := streamColor(styles.Info, timelineEventIcon(TimelineKindNetworkAllowed))
			fmt.Fprintf(&sb, "    %s %s%s\n", icon, evt.Host, method)

		case TimelineKindNetworkBlocked:
			method := ""
			if evt.HTTPMethod != "" {
				method = "  " + streamColor(styles.LineNumber, evt.HTTPMethod)
			}
			icon := streamColor(styles.Error, timelineEventIcon(TimelineKindNetworkBlocked))
			blocked := streamColor(styles.Error, "[blocked]")
			fmt.Fprintf(&sb, "    %s %s%s  %s\n", icon, evt.Host, method, blocked)

		case TimelineKindDIFCFiltered:
			detail := formatStreamToolDetail(evt.ServerName, evt.ToolName)
			icon := streamColor(styles.Warning, timelineEventIcon(TimelineKindDIFCFiltered))
			reason := ""
			if evt.Reason != "" {
				reason = "  " + streamColor(styles.Warning, stringutil.Truncate(evt.Reason, streamMaxAnnotationLen))
			}
			fmt.Fprintf(&sb, "    %s %s%s\n", icon, detail, reason)

		case TimelineKindGuardPolicyBlocked:
			detail := formatStreamToolDetail(evt.ServerName, evt.ToolName)
			icon := streamColor(styles.Error, timelineEventIcon(TimelineKindGuardPolicyBlocked))
			annotation := evt.Reason
			if annotation == "" {
				annotation = evt.Error
			}
			annotationStr := ""
			if annotation != "" {
				annotationStr = "  " + streamColor(styles.Error, stringutil.Truncate(annotation, streamMaxAnnotationLen))
			}
			fmt.Fprintf(&sb, "    %s %s%s\n", icon, detail, annotationStr)

		case TimelineKindSteering:
			icon := streamColor(styles.Warning, timelineEventIcon(TimelineKindSteering))
			msg := stringutil.Truncate(evt.Reason, streamMaxAnnotationLen)
			fmt.Fprintf(&sb, "    %s %s\n", icon, msg)

		default:
			fmt.Fprintf(&sb, "  · [%s] %s  %s\n", ts, string(evt.Kind), timelineSourceLabel(evt.Source))
		}
	}

	if inTurn {
		sb.WriteString("\n")
	}

	return sb.String()
}

// ─── Top-level renderer ───────────────────────────────────────────────────────

// renderUnifiedTimeline renders a merged slice of UnifiedTimelineEvents as a single
// console table preceded by a summary of event counts per source and kind.
// Returns an empty string when events is empty.
func renderUnifiedTimeline(events []UnifiedTimelineEvent) string {
	if len(events) == 0 {
		return ""
	}

	// Tally event counts for the summary header.
	var gwCount, fwCount, agCount int
	var toolCalls, difcFiltered, guardBlocked, netAllowed, netBlocked, steeringCount int
	var agentTurns, agentToolStarts, agentToolDones, assistantMessages, reasoningCount int
	for _, evt := range events {
		switch evt.Source {
		case TimelineSourceGateway:
			gwCount++
			switch evt.Kind {
			case TimelineKindToolCall:
				toolCalls++
			case TimelineKindDIFCFiltered:
				difcFiltered++
			case TimelineKindGuardPolicyBlocked:
				guardBlocked++
			}
		case TimelineSourceFirewall:
			fwCount++
			switch evt.Kind {
			case TimelineKindNetworkAllowed:
				netAllowed++
			case TimelineKindNetworkBlocked:
				netBlocked++
			case TimelineKindSteering:
				steeringCount++
			}
		case TimelineSourceAgent:
			agCount++
			switch evt.Kind {
			case TimelineKindAgentTurn:
				agentTurns++
			case TimelineKindAgentToolStart:
				agentToolStarts++
			case TimelineKindAgentToolDone:
				agentToolDones++
			case TimelineKindAssistantMessage:
				assistantMessages++
			case TimelineKindReasoning:
				reasoningCount++
			}
		}
	}

	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(console.FormatInfoMessage("Unified MCP + Firewall + Agent Event Timeline"))
	sb.WriteString("\n\n")

	fmt.Fprintf(&sb, "Total Events  : %d\n", len(events))
	if gwCount > 0 {
		fmt.Fprintf(&sb, "  Gateway     : %d  (tool_calls=%d, difc_filtered=%d, guard_blocked=%d)\n",
			gwCount, toolCalls, difcFiltered, guardBlocked)
	}
	if fwCount > 0 {
		fwDetail := fmt.Sprintf("allowed=%d, blocked=%d", netAllowed, netBlocked)
		if steeringCount > 0 {
			fwDetail += fmt.Sprintf(", steering=%d", steeringCount)
		}
		fmt.Fprintf(&sb, "  Firewall    : %d  (%s)\n", fwCount, fwDetail)
	}
	if agCount > 0 {
		fmt.Fprintf(&sb, "  Agent       : %d  (turns=%d, tool_start=%d, tool_done=%d, messages=%d, reasoning=%d)\n",
			agCount, agentTurns, agentToolStarts, agentToolDones, assistantMessages, reasoningCount)
	}
	sb.WriteString("\n")

	// Build the table rows using per-kind primitives.
	rows := make([][]string, 0, len(events))
	for _, evt := range events {
		rows = append(rows, renderTimelineEventRow(evt))
	}

	sb.WriteString(console.RenderTable(console.TableConfig{
		Title:   "Event Timeline",
		Headers: []string{"Time", "Src", "Kind", "Detail", "Status"},
		Rows:    rows,
	}))

	return sb.String()
}

// displayUnifiedTimeline collects all JSONL events from every processed run, merges them
// into a single chronologically ordered stream, and writes the rendered timeline to
// stderr. It is a no-op when no events can be collected from any run.
func displayUnifiedTimeline(processedRuns []ProcessedRun, verbose bool) {
	gatewayLogsLog.Printf("Collecting unified timeline events from %d processed runs", len(processedRuns))

	var allEvents []UnifiedTimelineEvent
	for _, pr := range processedRuns {
		logDir := pr.Run.LogsPath
		if logDir == "" {
			continue
		}
		events, err := BuildUnifiedTimeline(logDir, verbose)
		if err != nil {
			gatewayLogsLog.Printf("BuildUnifiedTimeline error for run %d: %v", pr.Run.DatabaseID, err)
			continue
		}
		allEvents = append(allEvents, events...)
	}

	if len(allEvents) == 0 {
		gatewayLogsLog.Print("No unified timeline events found across all runs")
		return
	}

	// Re-sort after merging events from multiple runs.
	sortUnifiedTimelineEvents(allEvents)

	gatewayLogsLog.Printf("Rendering unified timeline: %d total events across %d runs", len(allEvents), len(processedRuns))
	if output := renderUnifiedTimeline(allEvents); output != "" {
		fmt.Fprint(os.Stderr, output)
	}
}

// sortUnifiedTimelineEvents sorts events in-place by ascending wall-clock time.
// It is a no-op when the slice is already sorted; otherwise it delegates to
// slices.SortStableFunc, which preserves insertion order for equal timestamps.
func sortUnifiedTimelineEvents(events []UnifiedTimelineEvent) {
	for i := 1; i < len(events); i++ {
		if events[i].Time.Before(events[i-1].Time) {
			// Only sort when the slice is not already in order.
			slices.SortStableFunc(events, func(a, b UnifiedTimelineEvent) int {
				switch {
				case a.Time.Before(b.Time):
					return -1
				case b.Time.Before(a.Time):
					return 1
				default:
					return 0
				}
			})
			return
		}
	}
}
