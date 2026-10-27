package workflow

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/goccy/go-yaml"
)

// Authored host code cannot be made read-only by staging safe outputs.
func (c *Compiler) validateDryRunEffects(data *WorkflowData) error {
	if !c.dryRun {
		return nil
	}
	var unsupported []string
	for _, section := range []struct {
		name, content string
	}{
		{"steps", data.CustomSteps},
		{"pre-steps", data.PreSteps},
		{"pre-agent-steps", data.PreAgentSteps},
		{"post-steps", data.PostSteps},
	} {
		if strings.TrimSpace(section.content) != "" {
			unsupported = append(unsupported, section.name)
		}
	}
	if len(data.OnSteps) > 0 {
		unsupported = append(unsupported, "on.steps")
	}
	if len(data.ResolvedMCPServers) > 0 || (data.ParsedTools != nil && len(data.ParsedTools.Custom) > 0) {
		unsupported = append(unsupported, "custom MCP servers")
	}
	if data.MCPScripts != nil {
		unsupported = append(unsupported, "mcp-scripts")
	}
	unsupported = append(unsupported, dryRunResolvedOutputEffects(data)...)
	if data.Graders != nil {
		for _, name := range slices.Sorted(maps.Keys(data.Graders.Graders)) {
			grader := data.Graders.Graders[name]
			if grader != nil && (grader.Enabled == nil || *grader.Enabled) && grader.Run != "" {
				unsupported = append(unsupported, "graders."+name+".run")
			}
		}
	}
	permissions := NewPermissionsParser(data.Permissions).ToPermissions()
	if level, _ := permissions.Get(PermissionIdToken); level == PermissionWrite {
		unsupported = append(unsupported, "permissions.id-token")
	}
	if data.OnPermissions != nil {
		if level, _ := data.OnPermissions.Get(PermissionIdToken); level == PermissionWrite {
			unsupported = append(unsupported, "on.permissions.id-token")
		}
	}
	unsupported = append(unsupported, dryRunBuiltinJobEffects(data.Jobs)...)
	if len(unsupported) > 0 {
		return fmt.Errorf("--dry-run cannot safely execute %s: remove these declarations and their imports from the diagnostic workflow; staging does not isolate host scripts, external MCP effects, or cloud credentials", strings.Join(unsupported, ", "))
	}
	return nil
}

func dryRunResolvedOutputEffects(data *WorkflowData) []string {
	var unsupported []string
	if data.SecretMasking != nil && len(data.SecretMasking.Steps) > 0 {
		unsupported = append(unsupported, "secret-masking.steps")
	}
	if outputs := data.SafeOutputs; outputs != nil {
		if len(outputs.Scripts) > 0 {
			unsupported = append(unsupported, "safe-outputs.scripts")
		}
		if len(outputs.Steps) > 0 {
			unsupported = append(unsupported, "safe-outputs.steps")
		}
		if len(outputs.Actions) > 0 {
			unsupported = append(unsupported, "safe-outputs.actions")
		}
		if outputs.IDToken != nil && *outputs.IDToken == string(PermissionWrite) {
			unsupported = append(unsupported, "safe-outputs.id-token")
		}
		if detection := outputs.ThreatDetection; detection != nil {
			if len(detection.Steps) > 0 {
				unsupported = append(unsupported, "safe-outputs.threat-detection.steps")
			}
			if len(detection.PostSteps) > 0 {
				unsupported = append(unsupported, "safe-outputs.threat-detection.post-steps")
			}
		}
	}
	return unsupported
}

func dryRunBuiltinJobEffects(jobs map[string]any) []string {
	var unsupported []string
	for _, name := range slices.Sorted(maps.Keys(jobs)) {
		if !isBuiltinJobName(normalizeBuiltinJobAlias(name)) {
			continue
		}
		config, ok := jobs[name].(map[string]any)
		if !ok {
			continue // Job validation reports invalid configurations.
		}
		for _, field := range slices.Sorted(maps.Keys(config)) {
			if config[field] == nil {
				continue
			}
			// Fail closed when augmentation gains fields: only scheduling and
			// failure policy can customize an active built-in job in dry-run.
			switch field {
			case "needs", "if", "timeout-minutes", "continue-on-error":
				continue
			}
			unsupported = append(unsupported, "jobs."+name+"."+field)
		}
	}
	return unsupported
}

func (c *Compiler) disableDryRunCustomJobs(data *WorkflowData) {
	if !c.dryRun {
		return
	}
	// Reusable calls have no caller-side Outputs map. Capture their consumers'
	// contract before removing uses, including remote callees we cannot inspect.
	outputs := dryRunReferencedOutputs(data.On, data.Env, c.jobManager.GetAllJobs())
	for name := range data.Jobs {
		if !isBuiltinJobName(normalizeBuiltinJobAlias(name)) {
			c.disableDryRunCustomJob(name, data, outputs[strings.ToLower(name)])
		}
	}
	if data.SafeOutputs != nil {
		for name := range data.SafeOutputs.Jobs {
			id := stringutil.NormalizeSafeOutputIdentifier(name)
			c.disableDryRunCustomJob(id, data, outputs[strings.ToLower(id)])
		}
		if data.SafeOutputs.CallWorkflow != nil {
			for _, name := range data.SafeOutputs.CallWorkflow.Workflows {
				id := "call-" + sanitizeJobName(name)
				c.disableDryRunCustomJob(id, data, outputs[strings.ToLower(id)])
			}
		}
	}
}

func (c *Compiler) disableDryRunCustomJob(name string, data *WorkflowData, outputs map[string]string) {
	job, exists := c.jobManager.GetJob(name)
	if !exists {
		return
	}
	if job.Outputs == nil {
		job.Outputs = make(map[string]string)
	}
	maps.Copy(job.Outputs, outputs)
	makeDryRunJobInert(job, c.dryRunPlaceholderRunsOn(data, job))
	compilerDevelopmentLog.Printf("Dry-run mutation: disable custom job %q", name)
}

func makeDryRunJobInert(job *Job, runsOn string) {
	// Preserve dependency/output names without retaining executable code or credentials.
	outputs := make(map[string]string, len(job.Outputs))
	needs := slices.Clone(job.Needs)
	for output := range job.Outputs {
		outputs[output] = "${{ '' }}"
	}
	if job.Actions != nil {
		for _, dep := range job.Actions.Needs {
			if !slices.Contains(needs, string(dep)) {
				needs = append(needs, string(dep))
			}
		}
		for output := range job.Actions.Outputs {
			outputs[string(output)] = "${{ '' }}"
		}
		for _, output := range job.Actions.CallOutputs {
			outputs[string(output.Name)] = "${{ '' }}"
		}
	}
	*job = Job{
		Name: job.Name, DisplayName: job.DisplayName, Needs: needs, Outputs: outputs,
		If: "${{ !always() }}", RunsOn: runsOn,
		Permissions: "permissions: {}",
		Steps:       []string{"      - name: Custom job disabled in dry-run\n        run: echo 'Custom job disabled in dry-run'\n"},
	}
}

func (c *Compiler) dryRunPlaceholderRunsOn(data *WorkflowData, job *Job) string {
	requireSelfHosted, _ := compilerenv.ResolveRequireSelfHostedRunners()
	requireSelfHosted = requireSelfHosted || c.requireSelfHosted
	if config, err := c.loadRepoConfig(); err == nil && config != nil {
		requireSelfHosted = requireSelfHosted || config.RequireSelfHostedRunners
	}
	for _, candidate := range []string{job.RunsOn, data.RunsOn, c.formatFrameworkJobRunsOn(data)} {
		// The placeholder has no strategy or credentials, so runner expressions
		// must not retain dependencies on a removed matrix or secret context.
		if strings.Contains(candidate, "${{") {
			continue
		}
		var fields map[string]any
		if err := yaml.Unmarshal([]byte(candidate), &fields); err != nil {
			continue
		}
		runner := fields["runs-on"]
		if isEmptyRunsOnValue(runner) || (requireSelfHosted && !usesSelfHostedRunner(runner)) {
			continue
		}
		return candidate
	}
	if requireSelfHosted {
		return "runs-on: self-hosted"
	}
	return "runs-on: ubuntu-latest"
}

func dryRunReferencedOutputs(values ...any) map[string]map[string]string {
	outputs := make(map[string]map[string]string)
	add := func(job, output string) {
		job = strings.ToLower(job)
		if outputs[job] == nil {
			outputs[job] = make(map[string]string)
		}
		outputs[job][output] = "${{ '' }}"
	}
	var visit func(reflect.Value, bool)
	visit = func(value reflect.Value, implicitCondition bool) {
		if !value.IsValid() {
			return
		}
		if value.CanInterface() {
			if ref, ok := value.Interface().(actions.OutputRef); ok && ref.Step == "" {
				add(string(ref.Job), string(ref.Name))
				return
			}
		}
		switch value.Kind() {
		case reflect.Interface, reflect.Pointer:
			if !value.IsNil() {
				visit(value.Elem(), implicitCondition)
			}
		case reflect.String:
			paths := interpolationReferences(value.String())
			// Conditions allow bare expressions; other strings remain literal
			// unless explicitly wrapped in an Actions interpolation.
			if implicitCondition && !strings.Contains(value.String(), "${{") && value.Type() != reflect.TypeFor[actions.Literal]() {
				if expression, err := actionslanguageservice.ParseSyntax(value.String()); err == nil {
					paths = append(paths, actionslanguageservice.References(expression)...)
				}
			}
			for _, path := range paths {
				if len(path) >= 4 && (strings.EqualFold(path[0], "needs") || strings.EqualFold(path[0], "jobs")) && strings.EqualFold(path[2], "outputs") {
					add(path[1], path[3])
				}
			}
		case reflect.Map:
			iter := value.MapRange()
			for iter.Next() {
				visit(iter.Value(), implicitCondition)
			}
		case reflect.Slice, reflect.Array:
			for i := range value.Len() {
				visit(value.Index(i), implicitCondition)
			}
		case reflect.Struct:
			for definition, field := range value.Fields() {
				if field.CanInterface() {
					visit(field, implicitCondition || definition.Name == "If")
				}
			}
		}
	}
	for _, value := range values {
		visit(reflect.ValueOf(value), false)
	}
	return outputs
}
