package workflow

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var requestContinuationLog = logger.New("workflow:request_continuation")

// Continuation routes. A route turns an accepted request_continuation into one
// trusted follow-up task; without a route the request is metadata only.
const (
	RequestContinuationRouteWorkqueue = "workqueue"
	RequestContinuationRouteIssue     = "issue"
	RequestContinuationRouteDispatch  = "dispatch"

	// requestContinuationMaxDepthLimit bounds the automatic hop budget of one chain.
	requestContinuationMaxDepthLimit = 16
	// RequestContinuationInputName is the workflow_dispatch input a dispatch receiver declares.
	RequestContinuationInputName = "gh_aw_continuation"
	// requestContinuationAdmitAuthorization is the logical authorization name for
	// queue child admission at the worker finalizer.
	requestContinuationAdmitAuthorization = "request_continuation:admit"
)

var requestContinuationWorkflowIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// RequestContinuationConfig records a follow-up request. With Route set, Process Safe
// Outputs hands an accepted request to a trusted queue, issue-gated queue or
// workflow_dispatch successor; without it the request is metadata only.
type RequestContinuationConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Route                string `yaml:"route,omitempty"`     // workqueue, issue or dispatch
	Workflow             string `yaml:"workflow,omitempty"`  // dispatch target workflow ID
	MaxDepth             int    `yaml:"max-depth,omitempty"` // automatic hop budget (workqueue/dispatch)

	parseError   error  // malformed authoring input, reported during validation
	workflowFile string // resolved dispatch target file (e.g. summarize.lock.yml)
	awContext    bool   // dispatch target accepts the compiler-managed aw_context input
}

func (c *Compiler) parseRequestContinuationConfig(outputMap map[string]any) *RequestContinuationConfig {
	value, exists := outputMap["request-continuation"]
	if !exists || value == false {
		return nil
	}
	config := &RequestContinuationConfig{}
	config.Max = defaultIntStr(1)
	fields, ok := value.(map[string]any)
	if !ok {
		return config
	}
	c.parseBaseSafeOutputConfig(fields, &config.BaseSafeOutputConfig, 1)
	config.parseError = parseRequestContinuationRouteFields(fields, config)
	return config
}

// parseRequestContinuationRouteFields reads the routed form's literal fields. Advisory
// forms (no route, workflow or max-depth key) keep the metadata-only parsing above.
func parseRequestContinuationRouteFields(fields map[string]any, config *RequestContinuationConfig) error {
	_, hasRoute := fields["route"]
	_, hasWorkflow := fields["workflow"]
	_, hasDepth := fields["max-depth"]
	if !hasRoute && !hasWorkflow && !hasDepth {
		return nil
	}
	for _, key := range sortedMapKeys(fields) {
		if !slices.Contains([]string{"route", "workflow", "max-depth", "max", "authorization"}, key) {
			return fmt.Errorf("safe-outputs.request-continuation.%s: unknown field for a continuation route; allowed fields are route, workflow, max-depth and max", key)
		}
	}
	if hasRoute {
		route, ok := fields["route"].(string)
		if !ok || strings.Contains(route, "${{") {
			return errors.New("safe-outputs.request-continuation.route: must be a literal workqueue, issue or dispatch")
		}
		if route != RequestContinuationRouteWorkqueue && route != RequestContinuationRouteIssue && route != RequestContinuationRouteDispatch {
			return fmt.Errorf("safe-outputs.request-continuation.route: unsupported route %q; use workqueue, issue or dispatch", route)
		}
		config.Route = route
	}
	if hasWorkflow {
		workflow, ok := fields["workflow"].(string)
		if !ok || !requestContinuationWorkflowIDPattern.MatchString(workflow) || strings.Contains(workflow, "..") || strings.HasSuffix(workflow, ".md") || strings.HasSuffix(workflow, ".yml") || strings.HasSuffix(workflow, ".yaml") {
			return errors.New("safe-outputs.request-continuation.workflow: must be one literal workflow ID in this repository (a file basename without extension, not a path, pattern or expression)")
		}
		config.Workflow = workflow
	}
	if hasDepth {
		depth, ok := literalInteger(fields["max-depth"])
		if !ok || depth < 1 || depth > requestContinuationMaxDepthLimit {
			return fmt.Errorf("safe-outputs.request-continuation.max-depth: must be a literal integer from 1 to %d", requestContinuationMaxDepthLimit)
		}
		config.MaxDepth = depth
	}
	if config.Route == "" {
		if hasWorkflow {
			return errors.New("safe-outputs.request-continuation.workflow: requires route: dispatch")
		}
		return errors.New("safe-outputs.request-continuation.max-depth: requires route: workqueue or dispatch")
	}
	if raw, present := fields["max"]; present {
		if max, ok := literalInteger(raw); !ok || max != 1 {
			return errors.New("safe-outputs.request-continuation.max: a routed continuation accepts exactly one request; omit max or set max: 1")
		}
	}
	if hasWorkflow && config.Route != RequestContinuationRouteDispatch {
		return fmt.Errorf("safe-outputs.request-continuation.workflow: is only valid with route: dispatch, not route: %s", config.Route)
	}
	if config.Route == RequestContinuationRouteDispatch && !hasWorkflow {
		return errors.New("safe-outputs.request-continuation.workflow: route: dispatch requires the target workflow ID")
	}
	if hasDepth && config.Route == RequestContinuationRouteIssue {
		return errors.New("safe-outputs.request-continuation.max-depth: is not valid with route: issue; every issue-gated successor needs its own approval")
	}
	if config.MaxDepth == 0 && config.Route != RequestContinuationRouteIssue {
		config.MaxDepth = 1
	}
	return nil
}

// literalInteger accepts YAML/JSON integer literals (JSON imports decode as float64).
func literalInteger(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), v >= math.MinInt32 && v <= math.MaxInt32
	case uint64:
		return int(v), v <= math.MaxInt32
	case float64:
		return int(v), v == math.Trunc(v) && v >= math.MinInt32 && v <= math.MaxInt32
	default:
		return 0, false
	}
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// requestContinuationRoute returns the configured route, or "" for the advisory form.
func requestContinuationRoute(config *SafeOutputsConfig) string {
	if config == nil || config.RequestContinuation == nil {
		return ""
	}
	return config.RequestContinuation.Route
}

// isRoutedRequestContinuation reports whether request_continuation hands off work.
func isRoutedRequestContinuation(config *SafeOutputsConfig) bool {
	return requestContinuationRoute(config) != ""
}

// isQueueRoutedRequestContinuation reports whether a worker queues its successor.
func isQueueRoutedRequestContinuation(config *SafeOutputsConfig) bool {
	route := requestContinuationRoute(config)
	return route == RequestContinuationRouteWorkqueue || route == RequestContinuationRouteIssue
}

// requestContinuationMaxDepth is the compiled hop budget. Issue routes report the default
// of 1 for the manager's depth check; their gated successors always carry remaining 0.
func requestContinuationMaxDepth(config *RequestContinuationConfig) int {
	if config == nil || config.Route == "" {
		return 0
	}
	if config.MaxDepth > 0 {
		return config.MaxDepth
	}
	return 1
}

// safeOutputHandlerBuiltin reports the effective builtin classification of a handler.
// A routed request_continuation authorizes future execution, so it never receives
// the advisory builtin exemption from global authorization.
func safeOutputHandlerBuiltin(config *SafeOutputsConfig, handler safeOutputHandlerDescriptor) bool {
	if handler.Key == "request-continuation" && isRoutedRequestContinuation(config) {
		return false
	}
	return handler.Builtin
}

func validateRequestContinuationAuthorization(config *SafeOutputsConfig) error {
	if config == nil || config.RequestContinuation == nil || config.RequestContinuation.Authorization == nil {
		return nil
	}
	if config.RequestContinuation.Route != "" {
		return errors.New("safe-outputs.request-continuation.authorization is not supported; continuation routes are authorized by the global safe-outputs.authorization")
	}
	return errors.New("safe-outputs.request-continuation.authorization is not supported because continuation requests are recorded in the conclusion step")
}

// validateRequestContinuationRoute checks the effective (imported) route against the
// workflow's role and resolves the dispatch target. Advisory forms are unchanged.
func (c *Compiler) validateRequestContinuationRoute(data *WorkflowData, markdownPath string) error {
	if data == nil || data.SafeOutputs == nil || data.SafeOutputs.RequestContinuation == nil {
		return nil
	}
	config := data.SafeOutputs.RequestContinuation
	if config.parseError != nil {
		return config.parseError
	}
	switch config.Route {
	case "":
		return nil
	case RequestContinuationRouteWorkqueue, RequestContinuationRouteIssue:
		if !isWorkQueueEnabled(data) || !isWorkQueueWorker(data) {
			return fmt.Errorf("safe-outputs.request-continuation.route: %s requires an assignment-bound work-queue worker (tools.work-queue.worker: true); ordinary workflows use route: dispatch", config.Route)
		}
	case RequestContinuationRouteDispatch:
		if isWorkQueueEnabled(data) {
			return errors.New("safe-outputs.request-continuation.route: dispatch requires an ordinary workflow; work-queue participants use route: workqueue or issue")
		}
		if hasWorkflowCallTrigger(data.On) {
			return errors.New("safe-outputs.request-continuation.route: dispatch is not supported for workflow_call workflows; the follow-up must be dispatched by the repository that owns the target")
		}
		file, awContext, err := resolveRequestContinuationDispatchTarget(config.Workflow, markdownPath)
		if err != nil {
			return err
		}
		config.workflowFile = file
		config.awContext = awContext
	default:
		return fmt.Errorf("safe-outputs.request-continuation.route: unsupported route %q", config.Route)
	}
	if config.Max != nil && *config.Max != "1" {
		return errors.New("safe-outputs.request-continuation.max: a routed continuation accepts exactly one request; omit max or set max: 1")
	}
	requestContinuationLog.Printf("Validated request-continuation route=%s max_depth=%d", config.Route, requestContinuationMaxDepth(config))
	return nil
}

// resolveRequestContinuationDispatchTarget resolves one local workflow ID with the
// dispatch-workflow resolution order (.lock.yml, plain YAML, same-batch Markdown) and
// checks it can be called with exactly gh_aw_continuation (plus compiler-filled aw_context).
func resolveRequestContinuationDispatchTarget(name, markdownPath string) (string, bool, error) {
	field := "safe-outputs.request-continuation.workflow"
	if name == "" || !requestContinuationWorkflowIDPattern.MatchString(name) || strings.Contains(name, "..") {
		return "", false, fmt.Errorf("%s: must be one literal workflow ID in this repository", field)
	}
	if name == getCurrentWorkflowName(markdownPath) || name == GetWorkflowIDFromPath(markdownPath) {
		return "", false, fmt.Errorf("%s: %q is this workflow; a continuation must dispatch a different workflow", field, name)
	}
	target, err := findWorkflowFile(name, markdownPath)
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", field, err)
	}
	extension, found := resolveWorkflowExtension(target)
	if !found {
		return "", false, fmt.Errorf("%s: workflow %q not found in %s (checked %s.lock.yml, %s.yml, %s.yaml, %s.md)", field, name, constants.GetWorkflowDir(), name, name, name, name)
	}
	if err := rejectWorkQueueDispatchTarget(field, name, target); err != nil {
		return "", false, err
	}
	if err := validateDispatchTargetRepresentations(field, name, target); err != nil {
		return "", false, err
	}
	return name + extension, workflowHasAwContextInput(target, name), nil
}

// rejectWorkQueueDispatchTarget rejects a target whose source or compiled lock is a
// work-queue workflow; queue work continues through the workqueue or issue route.
func rejectWorkQueueDispatchTarget(field, name string, target *findWorkflowFileResult) error {
	if target.mdExists {
		queue, err := mdHasWorkQueueTools(target.mdPath)
		if err != nil {
			return fmt.Errorf("%s: inspect %q: %w", field, name, err)
		}
		if queue {
			return fmt.Errorf("%s: %q is a work-queue workflow; queue work is continued with route: workqueue or issue, not dispatched directly", field, name)
		}
	}
	if target.lockExists {
		protocol, err := lockHasWorkQueueProtocol(target.lockPath)
		if err != nil {
			return fmt.Errorf("%s: inspect compiled %q: %w", field, name, err)
		}
		if protocol {
			return fmt.Errorf("%s: compiled %q is a work-queue worker; recompile it or choose an ordinary workflow", field, name)
		}
	}
	return nil
}

// validateDispatchTargetRepresentations checks every representation that exists so a
// stale compiled target cannot hide a changed source contract (and the source cannot
// hide a stale compiled one).
func validateDispatchTargetRepresentations(field, name string, target *findWorkflowFileResult) error {
	type representation struct {
		label  string
		inputs func() (map[string]any, bool, error)
	}
	var representations []representation
	if target.lockExists {
		representations = append(representations, representation{"compiled " + filepath.Base(target.lockPath), func() (map[string]any, bool, error) {
			return yamlWorkflowDispatchInputs(target.lockPath)
		}})
	} else if target.ymlExists {
		representations = append(representations, representation{filepath.Base(target.ymlPath), func() (map[string]any, bool, error) {
			return yamlWorkflowDispatchInputs(target.ymlPath)
		}})
	}
	if target.mdExists {
		representations = append(representations, representation{"source " + filepath.Base(target.mdPath), func() (map[string]any, bool, error) {
			dispatch, err := mdHasWorkflowDispatch(target.mdPath)
			if err != nil || !dispatch {
				return nil, false, err
			}
			inputs, err := extractMDWorkflowDispatchInputs(target.mdPath)
			return inputs, true, err
		}})
	}
	for _, rep := range representations {
		inputs, dispatch, err := rep.inputs()
		if err != nil {
			return fmt.Errorf("%s: read %s: %w", field, rep.label, err)
		}
		if !dispatch {
			return fmt.Errorf("%s: %s of %q does not support workflow_dispatch", field, rep.label, name)
		}
		if err := validateRequestContinuationReceiverInputs(inputs); err != nil {
			return fmt.Errorf("%s: %s of %q %w", field, rep.label, name, err)
		}
	}
	return nil
}

// yamlWorkflowDispatchInputs returns the workflow_dispatch inputs of a YAML workflow and
// whether it declares workflow_dispatch at all.
func yamlWorkflowDispatchInputs(path string) (map[string]any, bool, error) {
	workflow, err := readWorkflowYAML(path)
	if err != nil {
		return nil, false, err
	}
	onSection, ok := workflow["on"]
	if !ok || !containsWorkflowDispatch(onSection) {
		return nil, false, nil
	}
	return extractInputsFromParsedWorkflow(workflow, "workflow_dispatch"), true, nil
}

func validateRequestContinuationReceiverInputs(inputs map[string]any) error {
	for _, reserved := range []string{WorkQueueAssignmentInputName, "work_queue_claim"} {
		if _, present := inputs[reserved]; present {
			return fmt.Errorf("accepts the work-queue input %q; queue workers cannot be dispatch continuation targets", reserved)
		}
	}
	declared, ok := inputs[RequestContinuationInputName]
	if !ok {
		return fmt.Errorf("must declare a workflow_dispatch string input %q to receive a continuation", RequestContinuationInputName)
	}
	var definition map[string]any
	if fields, isMap := declared.(map[string]any); isMap {
		definition = fields
	}
	if kind, present := definition["type"]; present && kind != "string" {
		return fmt.Errorf("must declare input %q with type: string", RequestContinuationInputName)
	}
	if value, present := definition["default"]; present && value != nil && value != "" {
		return fmt.Errorf("must not give input %q a nonempty default", RequestContinuationInputName)
	}
	for _, name := range sortedMapKeys(inputs) {
		if name == RequestContinuationInputName || name == AwContextInputName {
			continue
		}
		definition, isMap := inputs[name].(map[string]any)
		if !isMap {
			continue
		}
		if required, isBool := definition["required"].(bool); !isBool || !required {
			continue
		}
		if value, present := definition["default"]; present && value != nil && value != "" {
			continue
		}
		return fmt.Errorf("requires input %q without a default; a continuation sends only %q", name, RequestContinuationInputName)
	}
	return nil
}

// workQueueWorkerContinuationRoute returns the effective request-continuation route of
// a worker source, resolving imported and included safe outputs as worker compilation does.
func (c *Compiler) workQueueWorkerContinuationRoute(mdPath string) (string, error) {
	content, err := os.ReadFile(mdPath) // #nosec G304 -- discovered in the compiled workflow's own directory
	if err != nil {
		return "", err
	}
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || result == nil {
		return "", err
	}
	config := c.extractSafeOutputsConfig(result.Frontmatter)
	var imported []string
	if frontmatterDeclaresImports(result.Frontmatter) {
		imports, err := parser.ProcessImportsFromFrontmatterWithSource(result.Frontmatter, filepath.Dir(mdPath), c.getSharedImportCache(), mdPath, string(content))
		if err != nil {
			return "", err
		}
		imported = append(imported, imports.MergedSafeOutputs...)
	}
	included, err := parser.ExpandIncludesForSafeOutputs(result.Markdown, filepath.Dir(mdPath), c.getSharedImportCache())
	if err != nil {
		return "", err
	}
	imported = append(imported, included...)
	if len(imported) > 0 {
		var raw map[string]any
		if section, isMap := result.Frontmatter["safe-outputs"].(map[string]any); isMap {
			raw = section
		}
		if config, err = c.MergeSafeOutputs(config, imported, raw); err != nil {
			return "", err
		}
	}
	if config == nil || config.RequestContinuation == nil || config.RequestContinuation.parseError != nil {
		return "", nil
	}
	return config.RequestContinuation.Route, nil
}

// computeRequestContinuationRoutePermissions returns the safe-output publisher's grant
// for a routed continuation: issues: write to open an approval Issue, actions: write to
// dispatch a follow-up workflow. Queue successors use the ledger's existing grants.
// Staged routes only preview and need no write.
func computeRequestContinuationRoutePermissions(safeOutputs *SafeOutputsConfig, excludePerHandlerApps bool) *Permissions {
	config := safeOutputs.RequestContinuation
	if config == nil || config.Route == "" || isHandlerStaged(templatableBoolIsTrue(safeOutputs.Staged), config.Staged) {
		return nil
	}
	// Broker policy, not GITHUB_TOKEN job permissions, authorizes brokered effects.
	if safeOutputs.TokenBroker && !excludePerHandlerApps {
		return nil
	}
	switch config.Route {
	case RequestContinuationRouteIssue:
		return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite})
	case RequestContinuationRouteDispatch:
		return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionActions: PermissionWrite})
	default:
		return nil
	}
}

// continuationEvidenceStepID binds the producing attempt's agent output for routed
// workflows; its continuation_evidence output becomes an agent job output.
const continuationEvidenceStepID = "bind_continuation_evidence"

// continuationEvidenceExpression is the agent job's bound evidence, read downstream.
func continuationEvidenceExpression() string {
	return fmt.Sprintf("${{ needs.%s.outputs.continuation_evidence }}", constants.AgentJobName)
}

// generateContinuationEvidenceStep hashes the final agent_output.json bytes right before
// the agent output uploads, under the same redaction, envelope and protected-session
// guards, so a later publisher can only route the attempt that produced the request.
func (c *Compiler) generateContinuationEvidenceStep(yaml *strings.Builder, data *WorkflowData, protectedSessionCapture bool) {
	if data.SafeOutputs == nil || !isRoutedRequestContinuation(data.SafeOutputs) {
		return
	}
	outputPath := constants.TmpGhAwDirSlash + constants.AgentOutputFilename.String()
	if isArcDindTopology(data) {
		if rewritten := rewriteTmpGhAwPathsForArcDind([]string{outputPath}); len(rewritten) > 0 {
			outputPath = rewritten[0]
		}
	}
	condition := "always() && " + secretRedactionSuccessCondition(data) + " && steps.validate_final_recovery_envelopes.outcome == 'success'"
	if protectedSessionCapture {
		condition += " && steps.parse_agent_logs.outcome == 'success'"
	}
	yaml.WriteString("      - name: Bind continuation evidence\n")
	yaml.WriteString("        id: " + continuationEvidenceStepID + "\n")
	yaml.WriteString("        if: " + condition + "\n")
	yaml.WriteString("        uses: " + c.getActionPin("actions/github-script") + "\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_CONTINUATION_AGENT_OUTPUT: " + outputPath + "\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/bind_continuation_evidence.cjs');\n")
	yaml.WriteString("            await main({ core });\n")
}

// workQueueContinuationFinishEnv passes the manager's accepted-request receipts, the
// bound evidence and the compiled route policy to the worker finalizer.
func (c *Compiler) workQueueContinuationFinishEnv(data *WorkflowData) []string {
	config := data.SafeOutputs.RequestContinuation
	env := []string{
		"          GH_AW_WORK_QUEUE_CONTINUATIONS: ${{ steps.process_safe_outputs.outputs.work_queue_continuations }}\n",
		"          GH_AW_CONTINUATION_EVIDENCE: " + continuationEvidenceExpression() + "\n",
		"          GH_AW_AGENT_OUTPUT: ${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}\n",
		"          GH_AW_CONTINUATION_ROUTE: " + fmt.Sprintf("%q", config.Route) + "\n",
		"          GH_AW_CONTINUATION_MAX_DEPTH: " + fmt.Sprintf("%q", strconv.Itoa(requestContinuationMaxDepth(config))) + "\n",
	}
	c.addHandlerManagerConfigEnvVar(&env, data)
	return env
}
