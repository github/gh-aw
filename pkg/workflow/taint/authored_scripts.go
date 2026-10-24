package taint

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow/aw"
)

type authoredScript struct {
	path    string
	source  string
	profile string
	fields  map[string]any
}

type authoredScriptCollector struct {
	scripts  []authoredScript
	failures []error
}

func (c *authoredScriptCollector) add(fields map[string]any, key, location, profile string) {
	if raw, exists := fields[key]; exists {
		source, ok := raw.(string)
		if !ok {
			c.failures = append(c.failures, fmt.Errorf("AW script must be a string at %s", location))
			return
		}
		c.scripts = append(c.scripts, authoredScript{location, source, profile, fields})
	}
}

func (c *authoredScriptCollector) mapping(fields map[string]any, key, location string) map[string]any {
	if fields[key] == nil {
		return nil
	}
	value, err := optionalField[map[string]any](fields, key, location)
	c.failures = append(c.failures, err)
	return value
}

// Collect only executable schema locations, never arbitrary "script" properties
// in JSON schemas, configuration data, or Markdown examples.
func authoredScripts(document *aw.Document) ([]authoredScript, error) {
	c := &authoredScriptCollector{}
	tools := c.mapping(document.Frontmatter, "mcp-scripts", "workflow")
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		fields, ok := tools[name].(map[string]any)
		if !ok {
			c.failures = append(c.failures, fmt.Errorf("AW script configuration must be a mapping at mcp-scripts.%s", name))
			continue
		}
		for _, language := range []string{"script", "run", "py", "go"} {
			c.add(fields, language, "mcp-scripts."+name+"."+language, "mcp-"+language)
		}
	}
	outputs := c.mapping(document.Frontmatter, "safe-outputs", "workflow")
	handlers := c.mapping(outputs, "scripts", "safe-outputs")
	for _, name := range slices.Sorted(maps.Keys(handlers)) {
		fields, ok := handlers[name].(map[string]any)
		if !ok {
			c.failures = append(c.failures, fmt.Errorf("AW script configuration must be a mapping at safe-outputs.scripts.%s", name))
			continue
		}
		c.add(fields, "script", "safe-outputs.scripts."+name+".script", "safe-output")
	}
	graders := c.mapping(document.Frontmatter, "graders", "workflow")
	for _, name := range slices.Sorted(maps.Keys(graders)) {
		fields, ok := graders[name].(map[string]any)
		if !ok {
			continue // Built-in graders also accept scalar configuration.
		}
		profile := "grader"
		if name == "operational-value" {
			profile = "bash"
		}
		c.add(fields, "script", "graders."+name+".script", profile)
	}
	c.memory(document)
	c.engine(document)
	return c.scripts, errors.Join(c.failures...)
}

func (c *authoredScriptCollector) memory(document *aw.Document) {
	memoryTools := c.mapping(document.Frontmatter, "tools", "workflow")
	for _, name := range []string{"cache-memory", "drive-memory", "repo-memory"} {
		configs := []any{memoryTools[name]}
		if list, ok := memoryTools[name].([]any); ok {
			configs = list
		}
		for i, raw := range configs {
			fields, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			location := "tools." + name
			if _, list := memoryTools[name].([]any); list {
				location += fmt.Sprintf("[%d]", i)
			}
			if validation, exists := fields["validation"]; exists {
				if object, ok := validation.(map[string]any); ok {
					c.add(object, "script", location+".validation.script", "memory")
				} else {
					c.add(fields, "validation", location+".validation", "memory")
				}
			} else {
				for _, key := range []string{"validation-script", "custom-validation"} {
					if _, exists := fields[key]; exists {
						c.add(fields, key, location+"."+key, "memory")
						break
					}
				}
			}
		}
	}
}

func (c *authoredScriptCollector) engine(document *aw.Document) {
	engine, ok := document.Frontmatter["engine"].(map[string]any)
	if !ok {
		return
	}
	if driver, ok := engine["driver"].(map[string]any); ok {
		for _, language := range slices.Sorted(maps.Keys(driver)) {
			profile := "unsupported"
			if language == "node" {
				profile = "node"
			}
			c.add(driver, language, "engine.driver."+language, profile)
		}
	} else if _, exists := engine["driver"]; exists {
		c.add(engine, "driver", "engine.driver", "file")
	}
	behaviors := c.mapping(engine, "behaviors", "engine")
	c.add(behaviors, "harness-script", "engine.behaviors.harness-script", "node")
	c.add(behaviors, "log-parser", "engine.behaviors.log-parser", "log-parser")
	mcp := c.mapping(behaviors, "mcp", "engine.behaviors")
	c.add(mcp, "config-adapter", "engine.behaviors.mcp.config-adapter", "node")
}

// AnalyzeAuthoredScripts checks non-step script entry points. Compiled runtime
// wrappers are deliberately not parsed: each entry uses its actual call contract.
func AnalyzeAuthoredScripts(ctx context.Context, document *aw.Document, rule Rule) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if document == nil || rule.ID == "" || rule.IsSource == nil || rule.IsSink == nil {
		return Result{}, errors.New("authored script analysis requires a document and a valid taint rule")
	}
	a := &analyzer{rule: rule}
	scripts, err := authoredScripts(document)
	failures := []error{err}
	parent, err := a.authoredEnvironment(ctx, document.Frontmatter, "env", nil)
	if err != nil {
		return a.result, err
	}
	mcp, err := a.authoredMCPEnvironment(ctx, document, parent)
	if err != nil {
		return a.result, err
	}
	for _, script := range scripts {
		if err := ctx.Err(); err != nil {
			return a.result, errors.Join(append(failures, err)...)
		}
		bindings, err := a.authoredScriptEnvironment(ctx, document, script, parent, mcp)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		failures = append(failures, a.authoredScript(ctx, script, bindings))
	}
	return a.result, errors.Join(failures...)
}

// Step env expressions and inline script write steps see the workflow context,
// not the execution step's overrides. Only runtime shell bindings are overlaid.
func (a *analyzer) authoredEnvironmentOverlay(ctx context.Context, fields map[string]any, location string, parent, bindings scope) (scope, error) {
	overrides, err := a.authoredEnvironment(ctx, fields, location, parent)
	if err != nil {
		return nil, err
	}
	if fields["env"] == nil {
		return bindings, nil
	}
	env, ok := fields["env"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("AW environment must be a mapping at %s", location)
	}
	for _, name := range slices.Sorted(maps.Keys(env)) {
		bindings["shell."+name] = overrides["shell."+name]
	}
	return bindings, nil
}

func (a *analyzer) authoredMCPEnvironment(ctx context.Context, document *aw.Document, parent scope) (scope, error) {
	bindings := maps.Clone(parent)
	tools, err := optionalField[map[string]any](document.Frontmatter, "mcp-scripts", "workflow")
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		fields, ok := tools[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("AW script configuration must be a mapping at mcp-scripts.%s", name)
		}
		bindings, err = a.authoredEnvironmentOverlay(ctx, fields, "mcp-scripts."+name+".env", parent, bindings)
		if err != nil {
			return nil, err
		}
	}
	return bindings, nil
}

func (a *analyzer) authoredScriptEnvironment(ctx context.Context, document *aw.Document, script authoredScript, parent, mcp scope) (scope, error) {
	if strings.HasPrefix(script.profile, "mcp-") {
		return mcp, nil
	}
	if script.profile == "safe-output" {
		outputs, err := optionalField[map[string]any](document.Frontmatter, "safe-outputs", "workflow")
		if err != nil {
			return nil, err
		}
		return a.authoredEnvironmentOverlay(ctx, outputs, "safe-outputs.env", parent, maps.Clone(parent))
	}
	if script.path != "engine.behaviors.harness-script" && !strings.HasPrefix(script.path, "engine.driver") {
		return parent, nil
	}
	engine, err := optionalField[map[string]any](document.Frontmatter, "engine", "workflow")
	if err != nil {
		return nil, err
	}
	bindings := maps.Clone(parent)
	if script.path == "engine.behaviors.harness-script" {
		behaviors, err := optionalField[map[string]any](engine, "behaviors", "engine")
		if err != nil {
			return nil, err
		}
		execution, err := optionalField[map[string]any](behaviors, "execution", "engine.behaviors")
		if err != nil {
			return nil, err
		}
		bindings, err = a.authoredEnvironmentOverlay(ctx, execution, "engine.behaviors.execution.env", parent, bindings)
		if err != nil {
			return nil, err
		}
	}
	bindings, err = a.authoredEnvironmentOverlay(ctx, engine, "engine.env", parent, bindings)
	if err != nil {
		return nil, err
	}
	if shorthand, ok := document.Frontmatter["sandbox"].(string); ok && (shorthand == "awf" || shorthand == "default") {
		return bindings, nil
	}
	sandbox, err := optionalField[map[string]any](document.Frontmatter, "sandbox", "workflow")
	if err != nil {
		return nil, err
	}
	switch agent := sandbox["agent"].(type) {
	case nil:
		return bindings, nil
	case string, bool:
		if agent != "awf" && agent != false {
			return nil, errors.New("AW sandbox.agent must be awf, false, null, or a mapping")
		}
		return bindings, nil
	}
	agent, err := optionalField[map[string]any](sandbox, "agent", "sandbox")
	if err != nil {
		return nil, err
	}
	return a.authoredEnvironmentOverlay(ctx, agent, "sandbox.agent.env", parent, bindings)
}

func (a *analyzer) authoredScript(ctx context.Context, script authoredScript, bindings scope) error {
	switch script.profile {
	case "mcp-py", "mcp-go", "unsupported":
		a.gap(script.path, "authored script language is not modeled")
		return nil
	case "file":
		sourcePath := path.Clean(script.source)
		if !strings.HasSuffix(sourcePath, ".js") && !strings.HasSuffix(sourcePath, ".cjs") && !strings.HasSuffix(sourcePath, ".mjs") {
			a.gap(script.path, "authored driver language is not modeled")
			return nil
		}
		if a.rule.JavaScript == nil || a.rule.JavaScript.Files == nil {
			a.gap(script.path, "authored JavaScript driver source is unavailable; configure the repository source root")
			return nil
		}
		source, err := javascriptReadSource(a.rule.JavaScript.Files, sourcePath)
		if err != nil {
			a.gap(script.path, "authored JavaScript driver source is unavailable")
			return nil
		}
		script.source = string(source)
		return a.authoredJavaScript(script, bindings, sourcePath)
	}
	base := script.path
	if index := strings.LastIndex(script.path, "."); index >= 0 {
		base = script.path[:index]
	}
	if script.profile == "memory" {
		parent := bindings
		bindings = make(scope)
		for _, name := range []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "RUNNER_TEMP", "GITHUB_WORKSPACE", "CI"} {
			bindings["shell."+name] = parent["shell."+name]
		}
	}
	if script.profile == "grader" {
		bindings = nil // vm.compileFunction has no process, require, or inherited env.
	}
	if script.profile == "mcp-run" || script.profile == "bash" {
		if script.profile == "bash" {
			previous := a.literalScript
			a.literalScript = true // Grader payload encoding prevents Actions interpolation.
			defer func() { a.literalScript = previous }()
			parent := bindings
			bindings = make(scope)
			for _, name := range []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SystemRoot", "ComSpec", "GH_TOKEN", "GH_HOST", "GITHUB_API_URL", "GITHUB_GRAPHQL_URL", "GITHUB_SERVER_URL"} {
				bindings["shell."+name] = parent["shell."+name]
			}
			bindings["shell.GH_TOKEN"] = a.source("github.token", base+".env.GH_TOKEN", nil)
		}
		return a.script(script.source, map[string]any{"shell": "bash"}, script.path, bindings, make(scope), make(scope))
	}
	return a.authoredJavaScript(script, bindings, "")
}

func (a *analyzer) authoredJavaScript(script authoredScript, bindings scope, sourcePath string) error {
	input := JavaScriptInput{Name: script.path, Source: script.source, Env: make(map[string][]Source)}
	for key, sources := range bindings {
		if name, ok := strings.CutPrefix(key, "shell."); ok {
			input.Env[name] = sources
		}
	}
	if sourcePath == "" && script.profile != "grader" && script.profile != "memory" {
		var err error
		input.Source, input.Literals, err = a.javascriptRender(script.source, script.path, bindings)
		if err != nil {
			return err
		}
	}
	j := newJavaScript(a, input, a.rule.JavaScript)
	if sourcePath != "" {
		j.file = sourcePath
	}
	if err := j.authoredGlobals(script); err != nil {
		return err
	}
	if err := j.run(); err != nil {
		return err
	}
	switch script.profile {
	case "mcp-script", "safe-output", "grader":
		j.emit(&JavaScriptNode{Line: 1}, Output, j.returns.taint())
	case "log-parser":
		parseLog := j.env["parseLog"]
		if parseLog.fn == nil {
			j.gap(nil, "authored log parser must define parseLog")
		} else {
			n := parseLog.fn.node
			result := j.invoke(n, parseLog.fn, []jsValue{{kind: jsString}})
			j.emit(n, Output, result.taint())
		}
	}
	return j.failure
}

func (j *javascript) authoredGlobals(script authoredScript) error {
	if script.profile == "grader" {
		for _, name := range []string{"process", "require", "fetch", "core", "github", "context", "exec", "Buffer", "module", "exports"} {
			delete(j.env, name)
		}
		for _, name := range []string{"trace", "run", "workflow", "config", "helpers", "Math"} {
			j.env[name] = jsValue{ref: name}
		}
		j.a.gap(script.path, "grader trace, run, workflow and configuration payload data flow is not modeled")
	}
	if script.profile == "memory" {
		j.env["fs"], j.env["path"] = jsValue{ref: "fs"}, jsValue{ref: "path"}
		for _, name := range []string{"memoryRoot", "memoryDir", "memoryId", "memoryKind", "memoryPhase"} {
			j.env[name] = jsValue{ref: name, kind: jsString}
		}
		j.a.gap(script.path, "memory validation filesystem contents and generated memory metadata are not modeled")
	}
	if script.profile == "mcp-script" || script.profile == "safe-output" {
		for _, name := range []string{"inputs", "config", "item", "resolvedTemporaryIds", "temporaryIdMap"} {
			j.env[name] = jsValue{ref: name, fields: make(map[string]jsValue)}
		}
		inputs, err := optionalField[map[string]any](script.fields, "inputs", script.path)
		if err != nil {
			return err
		}
		for _, name := range slices.Sorted(maps.Keys(inputs)) {
			reference := "inputs." + name
			if script.profile == "safe-output" {
				reference = "config." + name
			}
			j.env[stringutil.SanitizeParameterName(name)] = jsValue{ref: reference, sources: j.a.source(reference, script.path, nil)}
		}
		j.a.gap(script.path, "agent-provided inputs and generated runtime configuration are not modeled")
	}
	return nil
}
