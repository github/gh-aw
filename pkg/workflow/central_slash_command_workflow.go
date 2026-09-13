package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var centralSlashCommandWorkflowLog = logger.New("workflow:central_slash_command_workflow")

const (
	centralSlashCommandWorkflowFilename       = "agentic_commands.yml"
	legacyCentralSlashCommandWorkflowFilename = "agentic_slash_commands.yml"
)

type slashCommandRoute struct {
	Workflow      string   `json:"workflow"`
	Events        []string `json:"events"`
	AIReaction    string   `json:"ai_reaction,omitempty"`
	Emoji         string   `json:"emoji,omitempty"`
	StatusComment bool     `json:"status_comment,omitempty"`
}

type commandsHeaderMetadata struct {
	PayloadVersion string   `json:"payload_version"`
	SchemaVersion  string   `json:"schema_version"`
	Compiler       string   `json:"compiler_version"`
	Commands       []string `json:"commands"`
	Workflows      []string `json:"workflows"`
}

type helpCommandEntry struct {
	Command       string `json:"command"`
	Description   string `json:"description,omitempty"`
	Centralized   bool   `json:"centralized"`
	Decentralized bool   `json:"decentralized"`
	Label         bool   `json:"label,omitempty"`
	SourceFile    string `json:"source_file,omitempty"`
}

// GenerateCentralSlashCommandWorkflow generates a single centralized slash-command trigger
// workflow for workflows that opt into on.slash_command.strategy: centralized.
// When no centralized slash-command workflows are found, any existing generated file is deleted.
func GenerateCentralSlashCommandWorkflow(ctx context.Context, workflowDataList []*WorkflowData, workflowDir string, repoConfig *RepoConfig) error {
	centralSlashCommandWorkflowLog.Printf("Generating centralized slash-command workflow from %d workflow(s)", len(workflowDataList))
	slashRoutesByCommand, labelRoutesByCommand, mergedEvents := collectCentralCommandRoutes(workflowDataList)

	triggerFile := filepath.Join(workflowDir, centralSlashCommandWorkflowFilename)
	legacyTriggerFile := filepath.Join(workflowDir, legacyCentralSlashCommandWorkflowFilename)
	if (len(slashRoutesByCommand) == 0 && len(labelRoutesByCommand) == 0) || len(mergedEvents) == 0 {
		centralSlashCommandWorkflowLog.Print("No centralized slash-command participants found")
		if err := removeIfExists(triggerFile); err != nil {
			return fmt.Errorf("failed to delete centralized slash-command workflow: %w", err)
		}
		if err := cleanupLegacyCentralSlashCommandWorkflow(legacyTriggerFile); err != nil {
			return err
		}
		return nil
	}

	actionMode := DetectActionMode(GetVersion())
	var resolver SHAResolver
	for _, workflowData := range workflowDataList {
		if workflowData != nil && workflowData.ActionResolver != nil {
			resolver = workflowData.ActionResolver
			break
		}
	}
	setupActionRef := ResolveSetupActionReference(ctx, actionMode, GetVersion(), "", resolver)

	helpCommands := buildHelpCommandEntries(workflowDataList)
	helpCommandEnabled := repoConfig.IsHelpCommandEnabled()

	content, err := buildCentralSlashCommandWorkflowYAML(
		slashRoutesByCommand,
		labelRoutesByCommand,
		mergedEvents,
		resolveCentralSlashRunsOn(workflowDataList),
		setupActionRef,
		helpCommands,
		helpCommandEnabled,
	)
	if err != nil {
		return err
	}

	if err := os.WriteFile(triggerFile, []byte(content), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write centralized slash-command workflow: %w", err)
	}
	if err := cleanupLegacyCentralSlashCommandWorkflow(legacyTriggerFile); err != nil {
		return err
	}
	centralSlashCommandWorkflowLog.Printf("Wrote centralized slash-command workflow: %s", triggerFile)
	return nil
}

func centralRoutingCommandNames(wd *WorkflowData) []string {
	if wd == nil {
		return nil
	}
	if len(wd.Command) > 0 {
		return wd.Command
	}
	return nil
}

func collectCentralCommandRoutes(workflowDataList []*WorkflowData) (map[string][]slashCommandRoute, map[string][]slashCommandRoute, map[string]map[string]struct{}) {
	slashRoutesByCommand, mergedEvents := collectCentralSlashCommandRoutes(workflowDataList)
	labelRoutesByCommand := collectCentralLabelCommandRoutes(workflowDataList, mergedEvents)
	return slashRoutesByCommand, labelRoutesByCommand, mergedEvents
}

func cleanupLegacyCentralSlashCommandWorkflow(path string) error {
	if err := removeIfExists(path); err != nil {
		return fmt.Errorf("failed to delete legacy centralized slash-command workflow: %w", err)
	}
	return nil
}

func removeIfExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func collectCentralSlashCommandRoutes(workflowDataList []*WorkflowData) (map[string][]slashCommandRoute, map[string]map[string]struct{}) {
	routesByCommand := make(map[string][]slashCommandRoute)
	mergedEvents := make(map[string]map[string]struct{})

	for _, wd := range workflowDataList {
		commandNames := centralRoutingCommandNames(wd)
		if wd == nil || !wd.CommandCentralized || len(commandNames) == 0 {
			continue
		}

		filteredEvents := FilterCommentEvents(wd.CommandEvents)
		if len(filteredEvents) == 0 {
			continue
		}

		routeEvents := GetCommentEventNames(filteredEvents)
		routeEvents = sliceutil.Deduplicate(routeEvents)
		sort.Strings(routeEvents)
		if len(routeEvents) == 0 {
			continue
		}

		// Merge workflow-level subscriptions using YAML-ready GitHub event names.
		for _, event := range MergeEventsForYAML(filteredEvents) {
			if mergedEvents[event.EventName] == nil {
				mergedEvents[event.EventName] = make(map[string]struct{})
			}
			for _, t := range event.Types {
				mergedEvents[event.EventName][t] = struct{}{}
			}
		}

		for _, commandName := range commandNames {
			routesByCommand[commandName] = append(routesByCommand[commandName], buildCentralizedRoutes(wd, routeEvents, true)...)
		}
	}

	// Stable ordering for deterministic output.
	for commandName := range routesByCommand {
		slices.SortFunc(routesByCommand[commandName], func(left, right slashCommandRoute) int {
			if left.Workflow != right.Workflow {
				if left.Workflow < right.Workflow {
					return -1
				}
				return 1
			}
			leftEvents := strings.Join(left.Events, ",")
			rightEvents := strings.Join(right.Events, ",")
			if leftEvents != rightEvents {
				if leftEvents < rightEvents {
					return -1
				}
				return 1
			}
			switch {
			case left.AIReaction < right.AIReaction:
				return -1
			case left.AIReaction > right.AIReaction:
				return 1
			case left.Emoji < right.Emoji:
				return -1
			case left.Emoji > right.Emoji:
				return 1
			case !left.StatusComment && right.StatusComment:
				return -1
			case left.StatusComment && !right.StatusComment:
				return 1
			default:
				return 0
			}
		})
	}

	return routesByCommand, mergedEvents
}

func collectCentralLabelCommandRoutes(workflowDataList []*WorkflowData, mergedEvents map[string]map[string]struct{}) map[string][]slashCommandRoute {
	routesByLabel := make(map[string][]slashCommandRoute)

	for _, wd := range workflowDataList {
		if wd == nil || len(wd.LabelCommand) == 0 {
			continue
		}
		// Label-command routes participate in centralized dispatch when either:
		//   1) label_command.strategy is decentralized, or
		//   2) slash_command.strategy is centralized (label checks compile against aw_context).
		if !wd.LabelCommandDecentralized && !wd.CommandCentralized {
			continue
		}

		filteredEvents := FilterLabelCommandEvents(wd.LabelCommandEvents)
		routeEvents := sliceutil.Deduplicate(filteredEvents)
		sort.Strings(routeEvents)
		if len(routeEvents) == 0 {
			continue
		}

		for _, eventName := range routeEvents {
			if mergedEvents[eventName] == nil {
				mergedEvents[eventName] = make(map[string]struct{})
			}
			mergedEvents[eventName]["labeled"] = struct{}{}
		}

		for _, labelName := range wd.LabelCommand {
			routesByLabel[labelName] = append(routesByLabel[labelName], buildCentralizedRoutes(wd, routeEvents, true)...)
		}
	}

	for labelName := range routesByLabel {
		slices.SortFunc(routesByLabel[labelName], func(left, right slashCommandRoute) int {
			if left.Workflow != right.Workflow {
				if left.Workflow < right.Workflow {
					return -1
				}
				return 1
			}
			leftEvents := strings.Join(left.Events, ",")
			rightEvents := strings.Join(right.Events, ",")
			if leftEvents != rightEvents {
				if leftEvents < rightEvents {
					return -1
				}
				return 1
			}
			switch {
			case left.AIReaction < right.AIReaction:
				return -1
			case left.AIReaction > right.AIReaction:
				return 1
			case left.Emoji < right.Emoji:
				return -1
			case left.Emoji > right.Emoji:
				return 1
			case !left.StatusComment && right.StatusComment:
				return -1
			case left.StatusComment && !right.StatusComment:
				return 1
			default:
				return 0
			}
		})
	}

	return routesByLabel
}

func buildCentralizedRoutes(wd *WorkflowData, routeEvents []string, includeStatusComment bool) []slashCommandRoute {
	if wd == nil {
		return nil
	}
	type routeGroup struct {
		reaction      string
		emoji         string
		statusComment bool
	}
	eventGroups := map[routeGroup][]string{}
	groupOrder := make([]routeGroup, 0, len(routeEvents))
	for _, eventName := range routeEvents {
		reaction := resolveCentralizedEventReaction(wd, eventName)
		statusComment := includeStatusComment && resolveCentralizedEventStatusComment(wd, eventName)
		groupKey := routeGroup{
			reaction:      reaction,
			emoji:         wd.FrontmatterEmoji,
			statusComment: statusComment,
		}
		if _, exists := eventGroups[groupKey]; !exists {
			groupOrder = append(groupOrder, groupKey)
		}
		eventGroups[groupKey] = append(eventGroups[groupKey], eventName)
	}
	routes := make([]slashCommandRoute, 0, len(groupOrder))
	for _, groupKey := range groupOrder {
		routes = append(routes, slashCommandRoute{
			Workflow:      wd.WorkflowID,
			Events:        slices.Clone(eventGroups[groupKey]),
			AIReaction:    groupKey.reaction,
			Emoji:         groupKey.emoji,
			StatusComment: groupKey.statusComment,
		})
	}
	return routes
}

func resolveCentralizedEventReaction(wd *WorkflowData, eventName string) string {
	if wd == nil || wd.AIReaction == "" || wd.AIReaction == "none" {
		return ""
	}

	switch eventName {
	case "issues", "issue_comment":
		if shouldIncludeIssueReactions(wd) {
			return string(wd.AIReaction)
		}
	case "pull_request", "pull_request_comment", "pull_request_review_comment":
		if shouldIncludePullRequestReactions(wd) {
			return string(wd.AIReaction)
		}
	case "discussion", "discussion_comment":
		if shouldIncludeDiscussionReactions(wd) {
			return string(wd.AIReaction)
		}
	}

	return ""
}

func resolveCentralizedEventStatusComment(wd *WorkflowData, eventName string) bool {
	if wd == nil || wd.StatusComment == nil || !*wd.StatusComment {
		return false
	}

	switch eventName {
	case "issues", "issue_comment":
		return shouldIncludeIssueStatusComments(wd)
	case "pull_request", "pull_request_comment", "pull_request_review_comment":
		return shouldIncludePullRequestStatusComments(wd)
	case "discussion", "discussion_comment":
		return shouldIncludeDiscussionStatusComments(wd)
	default:
		return false
	}
}

func buildCentralSlashCommandWorkflowYAML(
	slashRoutesByCommand map[string][]slashCommandRoute,
	labelRoutesByCommand map[string][]slashCommandRoute,
	mergedEvents map[string]map[string]struct{},
	runsOn string,
	setupActionRef string,
	helpCommands []helpCommandEntry,
	helpCommandEnabled bool,
) (string, error) {
	slashRoutesJSON, err := json.Marshal(slashRoutesByCommand)
	if err != nil {
		return "", fmt.Errorf("failed to marshal centralized slash-command routes: %w", err)
	}
	labelRoutesJSON, err := json.Marshal(labelRoutesByCommand)
	if err != nil {
		return "", fmt.Errorf("failed to marshal decentralized label-command routes: %w", err)
	}
	helpCommandsJSON, err := json.Marshal(helpCommands)
	if err != nil {
		return "", fmt.Errorf("failed to marshal help commands metadata: %w", err)
	}

	commandsMetadata, err := json.Marshal(buildCommandsHeaderMetadata(slashRoutesByCommand, labelRoutesByCommand))
	if err != nil {
		return "", fmt.Errorf("failed to marshal centralized slash-command metadata: %w", err)
	}

	header := GenerateWorkflowHeader("", "gh-aw", "")

	var b strings.Builder
	b.WriteString("# gh-aw-commands: ")
	b.Write(commandsMetadata)
	b.WriteString("\n")
	writeCentralRouteSummaryComments(&b, slashRoutesByCommand, labelRoutesByCommand)
	b.WriteString(header)
	b.WriteString(`name: "Agentic Commands"

on:
`)
	writeCentralSlashEventsYAML(&b, mergedEvents)
	b.WriteString(`
permissions: {}

jobs:
  route:
    runs-on: ` + runsOn + `
    timeout-minutes: 15
`)
	writeCentralSlashRoutePermissions(&b, mergedEvents)
	b.WriteString(`
    steps:
      - name: Checkout repository
        uses: ` + getActionPin("actions/checkout") + `
        with:
          persist-credentials: false

      - name: Setup Scripts
        uses: ` + setupActionRef + `
        with:
          destination: ` + SetupActionDestination + `

      - name: Route slash command
        uses: ` + getActionPin("actions/github-script") + `
        # runner-guard:ignore RGS-016 -- routing tables below contain emoji variation selectors (U+FE0F) and zero-width joiners (U+200D) used to render standard emoji sequences, not steganographic payloads.
        env:
          GH_AW_SLASH_ROUTING: '` + escapeYAMLSingleQuoted(string(slashRoutesJSON)) + `'
          GH_AW_LABEL_ROUTING: '` + escapeYAMLSingleQuoted(string(labelRoutesJSON)) + `'
          GH_AW_HELP_COMMANDS: '` + escapeYAMLSingleQuoted(string(helpCommandsJSON)) + `'
          GH_AW_HELP_COMMAND_ENABLED: '` + strconv.FormatBool(helpCommandEnabled) + `'
          GH_AW_SLASH_COMMAND_DOCS_URL: 'https://github.github.com/gh-aw/reference/command-triggers/'
        with:
          script: |
            const { setupGlobals } = require('` + SetupActionDestination + `/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('` + SetupActionDestination + `/route_slash_command.cjs');
            await main();
`)
	finalYAML, err := finalizeRunnerTempSafety(b.String())
	if err != nil {
		return "", err
	}
	return finalYAML, nil
}

func buildHelpCommandEntries(workflowDataList []*WorkflowData) []helpCommandEntry {
	type aggregate struct {
		Description   string
		DescriptionBy string
		SourceFile    string
		Centralized   bool
		Decentralized bool
		Label         bool
	}
	byCommand := make(map[string]aggregate)
	byLabel := make(map[string]aggregate)

	for _, wd := range workflowDataList {
		if wd == nil || (len(wd.Command) == 0 && len(wd.LabelCommand) == 0) {
			continue
		}
		description := strings.TrimSpace(wd.Description)

		for _, commandName := range wd.Command {
			trimmed := strings.TrimSpace(commandName)
			if trimmed == "" {
				continue
			}
			if trimmed == "help" {
				centralSlashCommandWorkflowLog.Printf(
					"Warning: 'help' is reserved for the builtin /help handler in workflow %s; "+
						"this command will not be dispatched unless help_command is disabled via aw.json",
					wd.WorkflowID,
				)
			}
			existing := byCommand[trimmed]
			if existing.Description != "" && description != "" && existing.Description != description {
				centralSlashCommandWorkflowLog.Printf(
					"Conflicting descriptions for /%s: keeping %q from workflow %s, ignoring %q from workflow %s",
					trimmed,
					existing.Description,
					existing.DescriptionBy,
					description,
					wd.WorkflowID,
				)
			}
			// Conflict resolution keeps the first non-empty description encountered
			// while iterating workflowDataList, which is deterministic for compilation.
			if existing.Description == "" && description != "" {
				existing.Description = description
				existing.DescriptionBy = wd.WorkflowID
			}
			if existing.SourceFile == "" {
				existing.SourceFile = wd.WorkflowID
			}
			if wd.CommandCentralized {
				existing.Centralized = true
			} else {
				// Slash commands are either centralized or decentralized in current workflow metadata:
				// CommandCentralized=false indicates the command is handled in its own workflow.
				existing.Decentralized = true
			}
			byCommand[trimmed] = existing
		}

		for _, labelName := range wd.LabelCommand {
			trimmed := strings.TrimSpace(labelName)
			if trimmed == "" {
				continue
			}
			existing := byLabel[trimmed]
			if existing.Description != "" && description != "" && existing.Description != description {
				centralSlashCommandWorkflowLog.Printf(
					"Conflicting descriptions for label %q: keeping %q from workflow %s, ignoring %q from workflow %s",
					trimmed,
					existing.Description,
					existing.DescriptionBy,
					description,
					wd.WorkflowID,
				)
			}
			if existing.Description == "" && description != "" {
				existing.Description = description
				existing.DescriptionBy = wd.WorkflowID
			}
			if existing.SourceFile == "" {
				existing.SourceFile = wd.WorkflowID
			}
			existing.Label = true
			byLabel[trimmed] = existing
		}
	}

	commands := sliceutil.SortedKeys(byCommand)

	labels := sliceutil.SortedKeys(byLabel)

	entries := make([]helpCommandEntry, 0, len(commands)+len(labels))
	for _, command := range commands {
		item := byCommand[command]
		entries = append(entries, helpCommandEntry{
			Command:       command,
			Description:   item.Description,
			Centralized:   item.Centralized,
			Decentralized: item.Decentralized,
			SourceFile:    item.SourceFile,
		})
	}
	for _, labelName := range labels {
		item := byLabel[labelName]
		entries = append(entries, helpCommandEntry{
			Command:     labelName,
			Description: item.Description,
			Label:       true,
			SourceFile:  item.SourceFile,
		})
	}

	return entries
}

func writeCentralRouteSummaryComments(b *strings.Builder, slashRoutesByCommand map[string][]slashCommandRoute, labelRoutesByCommand map[string][]slashCommandRoute) {
	b.WriteString("# Routing summary (sorted):\n")
	b.WriteString("#   slash commands:\n")
	writeCentralRouteTypeSummary(b, slashRoutesByCommand, "/")
	b.WriteString("#   labels:\n")
	writeCentralRouteTypeSummary(b, labelRoutesByCommand, "")
}

func writeCentralRouteTypeSummary(b *strings.Builder, routesByTrigger map[string][]slashCommandRoute, prefix string) {
	if len(routesByTrigger) == 0 {
		b.WriteString("#     (none)\n")
		return
	}

	triggers := sliceutil.SortedKeys(routesByTrigger)

	for _, trigger := range triggers {
		routes := slices.Clone(routesByTrigger[trigger])
		slices.SortFunc(routes, func(left, right slashCommandRoute) int {
			if left.Workflow != right.Workflow {
				if left.Workflow < right.Workflow {
					return -1
				}
				return 1
			}
			leftEvents := strings.Join(left.Events, ",")
			rightEvents := strings.Join(right.Events, ",")
			if leftEvents != rightEvents {
				if leftEvents < rightEvents {
					return -1
				}
				return 1
			}
			switch {
			case left.AIReaction < right.AIReaction:
				return -1
			case left.AIReaction > right.AIReaction:
				return 1
			default:
				return 0
			}
		})
		for _, route := range routes {
			b.WriteString("#     ")
			b.WriteString(prefix)
			b.WriteString(trigger)
			b.WriteString(" -> ")
			b.WriteString(route.Workflow)
			b.WriteString(" [")
			b.WriteString(strings.Join(route.Events, ","))
			b.WriteString("]")
			if route.AIReaction != "" {
				b.WriteString(" reaction=")
				b.WriteString(route.AIReaction)
			}
			b.WriteString("\n")
		}
	}
}

func writeCentralSlashRoutePermissions(b *strings.Builder, mergedEvents map[string]map[string]struct{}) {
	b.WriteString(`    permissions:
      actions: write
      contents: read
`)
	if mergedEvents["issues"] != nil || mergedEvents["issue_comment"] != nil || mergedEvents["pull_request"] != nil {
		b.WriteString("      issues: write\n")
	}
	if needsPullRequestsPermission(mergedEvents) {
		b.WriteString("      pull-requests: write\n")
	}
	if mergedEvents["discussion"] != nil || mergedEvents["discussion_comment"] != nil {
		b.WriteString("      discussions: write\n")
	}
}

func needsPullRequestsPermission(mergedEvents map[string]map[string]struct{}) bool {
	// issue_comment and issues events can target pull requests (issue-backed PR payloads),
	// and runtime branch resolution uses pulls.get for those cases.
	pullRequestEvents := []string{"issues", "issue_comment", "pull_request", "pull_request_comment", "pull_request_review_comment", "pull_request_review"}
	for _, eventName := range pullRequestEvents {
		if mergedEvents[eventName] != nil {
			return true
		}
	}
	return false
}

func buildCommandsHeaderMetadata(slashRoutesByCommand map[string][]slashCommandRoute, labelRoutesByCommand map[string][]slashCommandRoute) commandsHeaderMetadata {
	commands := make([]string, 0, len(slashRoutesByCommand))
	workflowSet := make(map[string]struct {
	})
	for command, routes := range slashRoutesByCommand {
		commands = append(commands, command)
		for _, route := range routes {
			if route.Workflow != "" {
				workflowSet[route.Workflow] = struct {
				}{}
			}
		}
	}
	for _, routes := range labelRoutesByCommand {
		for _, route := range routes {
			if route.Workflow != "" {
				workflowSet[route.Workflow] = struct {
				}{}
			}
		}
	}
	sort.Strings(commands)
	workflows := sliceutil.SortedKeys(workflowSet)
	metadataCompilerVersion := "dev"
	if IsRelease() && strings.TrimSpace(GetVersion()) != "" {
		metadataCompilerVersion = GetVersion()
	}
	return commandsHeaderMetadata{
		PayloadVersion: "v1",
		SchemaVersion:  "v1",
		Compiler:       metadataCompilerVersion,
		Commands:       commands,
		Workflows:      workflows,
	}
}

func resolveCentralSlashRunsOn(workflowDataList []*WorkflowData) string {
	counts := map[string]int{}
	for _, wd := range workflowDataList {
		if wd == nil {
			continue
		}
		participates := (wd.CommandCentralized && len(wd.Command) > 0) || (wd.LabelCommandDecentralized && len(wd.LabelCommand) > 0)
		if !participates {
			continue
		}

		resolved := constants.DefaultActivationJobRunnerImage
		if wd.SafeOutputs != nil && strings.TrimSpace(wd.SafeOutputs.RunsOn) != "" {
			resolved = formatRunsOnSnippetForInlineValue(wd.SafeOutputs.RunsOn)
		} else if strings.TrimSpace(wd.RunsOnSlim) != "" {
			resolved = formatRunsOnSnippetForInlineValue(wd.RunsOnSlim)
		}
		counts[resolved]++
	}

	best := constants.DefaultActivationJobRunnerImage
	bestCount := counts[best]
	for candidate, count := range counts {
		if count > bestCount || (count == bestCount && candidate < best) {
			best = candidate
			bestCount = count
		}
	}
	return best
}

func formatRunsOnSnippetForInlineValue(runsOn string) string {
	runsOn = strings.TrimSpace(runsOn)
	if !strings.HasPrefix(runsOn, "runs-on:") {
		return runsOn
	}

	value := strings.TrimPrefix(runsOn, "runs-on:")
	if !strings.HasPrefix(value, "\n") {
		return strings.TrimSpace(value)
	}

	value = strings.TrimPrefix(value, "\n")
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		// The 2-space strip matches DefaultMarshalOptions map indentation.
		// The 6-space re-indent aligns with the central slash command template,
		// where runs-on: lives at 4-space job-level indent (4 + 2 = 6).
		line = strings.TrimPrefix(line, "  ")
		lines[i] = "      " + line
	}
	return "\n" + strings.Join(lines, "\n")
}

func writeCentralSlashEventsYAML(b *strings.Builder, mergedEvents map[string]map[string]struct{}) {
	eventOrder := []string{
		"issues",
		"issue_comment",
		"pull_request",
		"pull_request_review",
		"pull_request_review_comment",
		"discussion",
		"discussion_comment",
	}

	for _, eventName := range eventOrder {
		typeSet := mergedEvents[eventName]
		if len(typeSet) == 0 {
			continue
		}
		types := sliceutil.SortedKeys(typeSet)
		b.WriteString("  " + eventName + ":\n")
		b.WriteString("    types: [" + strings.Join(types, ", ") + "]\n")
	}
}
