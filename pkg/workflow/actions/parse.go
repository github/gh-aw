package actions

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/goccy/go-yaml"
)

type workflowYAML struct {
	Name        string              `yaml:"name"`
	RunName     string              `yaml:"run-name"`
	On          any                 `yaml:"on"`
	Permissions any                 `yaml:"permissions"`
	Env         map[string]any      `yaml:"env"`
	Defaults    map[string]any      `yaml:"defaults"`
	Concurrency any                 `yaml:"concurrency"`
	CacheMode   string              `yaml:"cache-mode"`
	Jobs        map[string]*jobYAML `yaml:"jobs"`
}

type jobYAML struct {
	Name            string           `yaml:"name"`
	RunsOn          any              `yaml:"runs-on"`
	Needs           any              `yaml:"needs"`
	If              any              `yaml:"if"`
	Env             map[string]any   `yaml:"env"`
	Steps           []map[string]any `yaml:"steps"`
	Outputs         map[string]any   `yaml:"outputs"`
	Permissions     any              `yaml:"permissions"`
	Defaults        map[string]any   `yaml:"defaults"`
	Concurrency     any              `yaml:"concurrency"`
	Environment     any              `yaml:"environment"`
	Strategy        map[string]any   `yaml:"strategy"`
	Container       any              `yaml:"container"`
	Services        map[string]any   `yaml:"services"`
	TimeoutMinutes  any              `yaml:"timeout-minutes"`
	ContinueOnError any              `yaml:"continue-on-error"`
	Uses            string           `yaml:"uses"`
	With            map[string]any   `yaml:"with"`
	Secrets         any              `yaml:"secrets"`
}

// Parse builds and validates a native Actions graph from authored or generated
// YAML. Expressions in authored values remain opaque: their declared needs
// edges are authoritative, rather than inferred by scanning expression text.
// Emit retains the original source and its comments, including after AST edits.
func Parse(content []byte) (*Workflow, error) {
	var source workflowYAML
	decoder := yaml.NewDecoder(bytes.NewReader(content), yaml.Strict())
	if err := decoder.Decode(&source); err != nil {
		return nil, fmt.Errorf("parsing GitHub Actions workflow: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("parsing additional YAML document: %w", err)
		}
		return nil, errors.New("workflow must contain exactly one YAML document")
	}
	if len(source.Jobs) == 0 {
		return nil, errors.New("GitHub Actions workflow requires jobs")
	}
	env, err := parseValues(source.Env)
	if err != nil {
		return nil, fmt.Errorf("workflow env: %w", err)
	}
	result := &Workflow{
		Name: source.Name, RunName: source.RunName, On: source.On,
		Permissions: source.Permissions, Env: env,
		Defaults: source.Defaults, Concurrency: source.Concurrency, CacheMode: source.CacheMode,
	}
	for _, id := range sortedKeys(source.Jobs) {
		job, err := parseJob(JobID(id), source.Jobs[id])
		if err != nil {
			return nil, fmt.Errorf("parsing job %q: %w", id, err)
		}
		result.Jobs = append(result.Jobs, job)
	}
	resolved, err := Resolve(result)
	if err != nil {
		return nil, err
	}
	for _, job := range resolved.Jobs {
		if _, err := lowerJob(job, resolved); err != nil {
			return nil, fmt.Errorf("validating job %q: %w", job.ID, err)
		}
	}
	if err := retainWorkflowSource(resolved, content); err != nil {
		return nil, err
	}
	return resolved, nil
}

func parseJob(id JobID, source *jobYAML) (*Job, error) {
	if source == nil {
		return nil, errors.New("job cannot be null")
	}
	job := &Job{
		ID: id, Name: source.Name, RunsOn: source.RunsOn,
		Permissions: source.Permissions, Defaults: source.Defaults,
		Concurrency: source.Concurrency, Environment: source.Environment,
		Strategy: source.Strategy, Container: source.Container,
		Services: source.Services, Uses: source.Uses,
	}
	needs, err := parseNeeds(source.Needs)
	if err != nil {
		return nil, err
	}
	job.Needs = needs
	for _, fields := range source.Steps {
		job.Steps = append(job.Steps, RawStep{Fields: fields})
	}
	if err := parseJobExpressions(job, source); err != nil {
		return nil, err
	}
	if source.TimeoutMinutes != nil {
		if text, ok := source.TimeoutMinutes.(string); ok {
			job.TimeoutMinutesExpression, err = parseCondition(text)
		} else {
			var minutes int64
			minutes, err = strconv.ParseInt(fmt.Sprint(source.TimeoutMinutes), 10, 0)
			job.TimeoutMinutes = int(minutes)
			job.TimeoutMinutesSet = true
		}
		if err != nil {
			return nil, fmt.Errorf("invalid timeout-minutes: %w", err)
		}
	}
	return job, nil
}

func parseJobExpressions(job *Job, source *jobYAML) error {
	var err error
	if job.If, err = parseCondition(source.If); err != nil {
		return err
	}
	if job.ContinueOnError, err = parseCondition(source.ContinueOnError); err != nil {
		return err
	}
	if job.Env, err = parseValues(source.Env); err != nil {
		return fmt.Errorf("env: %w", err)
	}
	if job.With, err = parseValues(source.With); err != nil {
		return fmt.Errorf("with: %w", err)
	}
	outputs, err := parseValues(source.Outputs)
	if err != nil {
		return fmt.Errorf("outputs: %w", err)
	}
	for key, value := range outputs {
		if job.Outputs == nil {
			job.Outputs = make(map[OutputName]Expression)
		}
		job.Outputs[OutputName(key)] = value
	}
	switch secrets := source.Secrets.(type) {
	case nil:
	case string:
		if secrets != "inherit" {
			return errors.New("secrets must be inherit or a mapping")
		}
		job.SecretsInherit = true
	case map[string]any:
		job.Secrets, err = parseValues(secrets)
	default:
		return errors.New("secrets must be inherit or a mapping")
	}
	return err
}

func parseNeeds(value any) ([]JobID, error) {
	switch needs := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []JobID{JobID(needs)}, nil
	case []any:
		result := make([]JobID, 0, len(needs))
		for _, dependency := range needs {
			id, ok := dependency.(string)
			if !ok {
				return nil, errors.New("needs entries must be job IDs")
			}
			result = append(result, JobID(id))
		}
		return result, nil
	default:
		return nil, errors.New("needs must be a job ID or an array of job IDs")
	}
}

func parseCondition(value any) (Expression, error) {
	switch condition := value.(type) {
	case nil:
		return nil, nil
	case bool:
		return Boolean(condition), nil
	case string:
		text := strings.TrimSpace(condition)
		spans, err := actionslanguageservice.ExtractExpressions(text)
		if err == nil && len(spans) == 1 && spans[0].Start == 0 && spans[0].End == len(text) {
			text = strings.TrimSpace(spans[0].Content)
		}
		if text == "true" || text == "false" {
			return Boolean(text == "true"), nil
		}
		if strings.ContainsAny(text, "\r\n") {
			return parsedExpression(text), nil
		}
		return Context(text), nil
	default:
		return nil, errors.New("if, continue-on-error, and expression timeouts must be booleans or expression strings")
	}
}

func parseValues(values map[string]any) (map[string]Expression, error) {
	if values == nil {
		return nil, nil
	}
	result := make(map[string]Expression, len(values))
	for _, key := range sortedKeys(values) {
		var expr Expression
		switch value := values[key].(type) {
		case string:
			expr = Literal(value)
		case bool:
			expr = Boolean(value)
		case int, int64, uint64, float64:
			number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number in %q: %w", key, err)
			}
			expr = Number(number)
		case nil:
			expr = Context("null")
		default:
			return nil, fmt.Errorf("value %q must be a scalar, got %T", key, value)
		}
		result[key] = expr
	}
	return result, nil
}
