package workflow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/goccy/go-yaml"
)

const decisionStepID = "decision_gate"
const decisionQuestionPrefix = "GH_AW_DECISION_QUESTION_"

type DecisionConfig struct {
	Model     string
	Questions []string
}

// Check the authored graph before generic validators can quote credential-bearing
// source lines. The generated graph is checked again after import expansion.
func validateAuthoredDecision(frontmatter map[string]any, document *aw.Document, sourcePath string) error {
	config, err := extractDecision(frontmatter)
	if err != nil || config == nil {
		return err
	}
	on, _ := frontmatter["on"].(map[string]any)
	jobs := map[string]any{}
	authoredJobs, _ := frontmatter["jobs"].(map[string]any)
	var steps []any
	for name, value := range authoredJobs {
		job, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if normalizeBuiltinJobAlias(name) == "pre_activation" {
			custom, _ := job["steps"].([]any)
			steps = append(steps, custom...)
			continue
		}
		if isBuiltinJobName(normalizeBuiltinJobAlias(name)) {
			continue
		}
		jobs[name] = job
	}
	onSteps, _ := on["steps"].([]any)
	steps = append(steps, onSteps...)
	env := map[string]any{}
	for i, question := range config.Questions {
		env[decisionQuestionPrefix+strconv.Itoa(i)] = question
	}
	steps = append(steps, map[string]any{"id": decisionStepID, "uses": "actions/github-script@v8", "env": env})
	preActivation := map[string]any{"runs-on": "ubuntu-latest", "steps": steps}
	if on["needs"] != nil {
		preActivation["needs"] = on["needs"]
	}
	jobs["pre_activation"] = preActivation
	encoded, err := yaml.Marshal(map[string]any{"on": "workflow_dispatch", "env": frontmatter["env"], "jobs": jobs})
	if err != nil {
		return errors.New("cannot securely analyze on.decision.questions")
	}
	graph, err := actions.Parse(encoded)
	if err != nil {
		// Incomplete authored jobs are validated after the compiler fills in their
		// defaults. A minimal graph still checks direct references and root env.
		encoded, err = yaml.Marshal(map[string]any{"on": "workflow_dispatch", "env": frontmatter["env"], "jobs": map[string]any{
			"pre_activation": map[string]any{"runs-on": "ubuntu-latest", "steps": steps},
		}})
		if err != nil {
			return errors.New("cannot securely analyze on.decision.questions")
		}
		graph, err = actions.Parse(encoded)
		if err != nil {
			return errors.New("cannot securely analyze on.decision.questions")
		}
	}
	return validateDecisionQuestions(&WorkflowData{Decision: config, SourceDocument: document}, graph, sourcePath)
}

func extractDecision(frontmatter map[string]any) (*DecisionConfig, error) {
	on, _ := frontmatter["on"].(map[string]any)
	value, exists := on["decision"]
	if !exists {
		return nil, nil
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("on.decision must be an object with questions")
	}
	config := &DecisionConfig{Model: "decision"}
	for key := range fields {
		if key != "model" && key != "questions" {
			return nil, errors.New("on.decision only supports model and questions")
		}
	}
	if model, exists := fields["model"]; exists {
		config.Model, ok = model.(string)
		if !ok || strings.TrimSpace(config.Model) == "" || containsExpression(config.Model) {
			return nil, errors.New("on.decision.model must be a nonblank literal model or alias")
		}
		if failures := validateModelIdentifierStrings([]string{config.Model}, "on.decision.model"); len(failures) > 0 {
			return nil, errors.New("on.decision.model must be a valid model or alias")
		}
	}
	questions, ok := fields["questions"].([]any)
	if !ok || len(questions) < 1 || len(questions) > 8 {
		return nil, errors.New("on.decision.questions must contain 1 to 8 inline strings")
	}
	var failures []error
	for i, value := range questions {
		path := fmt.Sprintf("on.decision.questions[%d]", i)
		question, ok := value.(string)
		if !ok || strings.TrimSpace(question) == "" || len(question) > 4096 || strings.HasPrefix(strings.TrimSpace(question), "@") {
			failures = append(failures, fmt.Errorf("%s must be a nonblank inline string of at most 4096 bytes; @filespec is not supported", path))
			continue
		}
		config.Questions = append(config.Questions, question)
	}
	if err := errors.Join(failures...); err != nil {
		return nil, err
	}
	return config, nil
}

func decisionSecretRule() taint.Rule {
	rule := taint.SecretLeakRule()
	rule.ID = "decision-secret"
	// Even presence tests send information about credentials to a model.
	rule.IsFilter = nil
	rule.ConservativeActionOutputs = true
	rule.IsSink = func(site taint.Site) bool {
		return site.Kind == taint.DecisionQuestion
	}
	return rule
}

func validateDecisionQuestions(data *WorkflowData, graph *actions.Workflow, sourcePath string) error {
	if data.Decision == nil {
		return nil
	}
	rule := decisionSecretRule()
	var failures []error
	reported := make(map[int]bool)
	// Check every source expression, including malformed ones, independently so
	// that one question cannot hide diagnostics for subsequent questions.
	for i, question := range data.Decision.Questions {
		expressions, err := taint.AnalyzeText(context.Background(), question, fmt.Sprintf("on.decision.questions[%d]", i), rule, nil)
		if err != nil {
			failures = append(failures, decisionQuestionError(data, sourcePath, i, "invalid Actions expression"))
		}
		for _, expression := range expressions {
			if len(expression.Sources) > 0 && !reported[i] {
				failures = append(failures, decisionQuestionError(data, sourcePath, i, "credential-derived values are not allowed"))
				reported[i] = true
			}
		}
	}
	result, err := taint.Analyze(graph, rule)
	if err != nil {
		failures = append(failures, errors.New("cannot securely analyze on.decision.questions; remove invalid Actions expressions"))
	}
	for _, finding := range result.Findings {
		index, err := strconv.Atoi(strings.TrimPrefix(finding.Site.Input, decisionQuestionPrefix))
		if err != nil || reported[index] {
			continue
		}
		failures = append(failures, decisionQuestionError(data, sourcePath, index, "credential-derived values are not allowed"))
		reported[index] = true
	}
	return errors.Join(failures...)
}

func decisionQuestionError(data *WorkflowData, sourcePath string, index int, message string) error {
	path := fmt.Sprintf("on.decision.questions[%d]", index)
	if data.SourceDocument != nil {
		source := data.SourceDocument.Location
		location := parser.LocateJSONPathInYAML(strings.Join(source.FrontmatterLines, "\n"), fmt.Sprintf("/on/decision/questions/%d", index))
		if location.Found {
			return fmt.Errorf("%s:%d:%d: %s: %s", sourcePath, source.FrontmatterStart+location.Line-1, location.Column, path, message)
		}
	}
	return fmt.Errorf("%s: %s: %s", sourcePath, path, message)
}

func decisionEligibleCondition(data *WorkflowData, original ConditionNode, stepIDs []string, customOutputs map[string]string) (ConditionNode, error) {
	source := stripExpressionWrapper(data.If)
	if source == "" {
		return original, nil
	}
	outputs := maps.Clone(customOutputs)
	if outputs == nil {
		outputs = make(map[string]string)
	}
	for _, id := range stepIDs {
		if _, exists := outputs[id+"_result"]; !exists {
			outputs[id+"_result"] = "steps." + id + ".outcome"
		}
	}
	outputs["activated"] = original.Render()
	for range 8 {
		changed := false
		source = actionslanguageservice.RewriteReferences(source, func(ref []string) (string, bool) {
			if len(ref) != 4 || !strings.EqualFold(ref[0], "needs") || !strings.EqualFold(ref[1], "pre_activation") || !strings.EqualFold(ref[2], "outputs") {
				return "", false
			}
			value, exists := outputs[ref[3]]
			changed = changed || exists
			return "(" + stripExpressionWrapper(value) + ")", exists
		})
		if !changed {
			break
		}
	}
	expression, err := actionslanguageservice.ParseSyntax(source)
	if err != nil {
		return nil, errors.New("on.decision requires a valid if condition that can be evaluated before activation")
	}
	for _, ref := range actionslanguageservice.References(expression) {
		if len(ref) > 0 && strings.EqualFold(ref[0], "needs") {
			if len(ref) < 2 || !slices.Contains(data.OnNeeds, ref[1]) {
				return nil, errors.New("on.decision cannot evaluate the deferred if condition before activation; remove cyclic pre_activation output references and declare upstream dependencies in on.needs")
			}
		}
	}
	condition, err := ParseExpression(source)
	if err != nil {
		return nil, errors.New("on.decision requires a valid if condition that can be evaluated before activation")
	}
	if _, literal := original.(*StringLiteralNode); literal {
		return condition, nil
	}
	return BuildAnd(original, condition), nil
}

func (c *Compiler) appendDecisionStep(data *WorkflowData, steps []string, eligible string) []string {
	steps = append(steps, "      - name: Evaluate activation decision\n", "        id: "+decisionStepID+"\n")
	steps = append(steps, "        continue-on-error: true\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)), "        env:\n")
	steps = append(steps, formatYAMLEnv("          ", "GH_AW_DECISION_MODEL", data.Decision.Model))
	steps = append(steps, fmt.Sprintf("          GH_AW_DECISION_QUESTION_COUNT: \"%d\"\n", len(data.Decision.Questions)))
	for i, question := range data.Decision.Questions {
		steps = append(steps, formatYAMLEnv("          ", decisionQuestionPrefix+strconv.Itoa(i), question))
	}
	env := decisionEnv(data)
	env["GH_AW_DECISION_ELIGIBLE"] = "${{ " + stripExpressionWrapper(eligible) + " }}"
	for _, key := range slices.Sorted(maps.Keys(env)) {
		steps = append(steps, formatYAMLEnv("          ", key, env[key]))
	}
	steps = append(steps, "        with:\n", "          script: |\n", generateGitHubScriptWithRequire("decision_gate.cjs"))
	return c.appendDecisionArtifactStep(data, steps)
}
