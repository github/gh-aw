package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var jobLog = logger.New("workflow:jobs")

// Job represents a GitHub Actions job with all its properties
type Job struct {
	Name                       string
	DisplayName                string // Optional display name for the job (name property in YAML)
	RunsOn                     string
	If                         string
	HasWorkflowRunSafetyChecks bool // If true, the job's if condition includes workflow_run safety checks
	Permissions                string
	TimeoutMinutes             int
	Concurrency                string            // Job-level concurrency configuration
	Environment                string            // Job environment configuration
	Container                  string            // Job container configuration
	Services                   string            // Job services configuration
	Env                        map[string]string // Job-level environment variables
	Steps                      []string
	Needs                      []string // Job dependencies (needs clause)
	Outputs                    map[string]string

	// Reusable workflow call properties
	Uses    string            // Path to reusable workflow (e.g., ./.github/workflows/reusable.yml)
	With    map[string]any    // Input parameters for reusable workflow
	Secrets map[string]string // Secrets for reusable workflow
}

// JobManager manages a collection of jobs and handles dependency validation
type JobManager struct {
	jobs     map[string]*Job
	jobOrder []string // Job names in sorted alphabetical order
}

// NewJobManager creates a new JobManager instance
func NewJobManager() *JobManager {
	return &JobManager{
		jobs: make(map[string]*Job),
	}
}

// AddJob adds a job to the manager
func (jm *JobManager) AddJob(job *Job) error {
	if job.Name == "" {
		return fmt.Errorf("job name cannot be empty")
	}

	if _, exists := jm.jobs[job.Name]; exists {
		return fmt.Errorf("job '%s' already exists", job.Name)
	}

	jobLog.Printf("Adding job: %s", job.Name)
	jm.jobs[job.Name] = job
	jm.jobOrder = append(jm.jobOrder, job.Name)
	// Keep jobOrder sorted alphabetically after each addition
	sort.Strings(jm.jobOrder)
	return nil
}

// GetJob retrieves a job by name
func (jm *JobManager) GetJob(name string) (*Job, bool) {
	job, exists := jm.jobs[name]
	return job, exists
}

// GetAllJobs returns all jobs in the manager
func (jm *JobManager) GetAllJobs() map[string]*Job {
	// Return a copy to prevent external modification
	result := make(map[string]*Job)
	for name, job := range jm.jobs {
		result[name] = job
	}
	return result
}

// ValidateDependencies checks that all job dependencies exist and there are no cycles
func (jm *JobManager) ValidateDependencies() error {
	jobLog.Printf("Validating dependencies for %d jobs", len(jm.jobs))
	// First check that all dependencies reference existing jobs
	for jobName, job := range jm.jobs {
		for _, dep := range job.Needs {
			if _, exists := jm.jobs[dep]; !exists {
				jobLog.Printf("Validation failed: job %s depends on non-existent job %s", jobName, dep)
				return fmt.Errorf("job '%s' depends on non-existent job '%s'", jobName, dep)
			}
		}
	}

	// Check for cycles using DFS
	return jm.detectCycles()
}

// ValidateDuplicateSteps checks that no job has duplicate steps
// This detects compiler bugs where the same step is added multiple times
func (jm *JobManager) ValidateDuplicateSteps() error {
	jobLog.Printf("Validating for duplicate steps in %d jobs", len(jm.jobs))

	for jobName, job := range jm.jobs {
		if len(job.Steps) == 0 {
			continue
		}

		// Track seen steps to detect duplicates
		seen := make(map[string]int)

		for i, step := range job.Steps {
			// Extract step name from YAML for comparison
			stepName := extractStepName(step)
			if stepName == "" {
				// Steps without names can't be checked for duplicates
				continue
			}

			if firstIndex, exists := seen[stepName]; exists {
				jobLog.Printf("Duplicate step detected in job '%s': step '%s' at positions %d and %d", jobName, stepName, firstIndex, i)
				return fmt.Errorf("compiler bug: duplicate step '%s' found in job '%s' (positions %d and %d)", stepName, jobName, firstIndex, i)
			}

			seen[stepName] = i
		}
	}

	jobLog.Print("No duplicate steps detected in any job")
	return nil
}

// extractStepName extracts the step name from a YAML step string
// Returns empty string if no name is found
func extractStepName(stepYAML string) string {
	// Look for "name: " in the step YAML
	// Format is typically "      - name: Step Name" with various indentation
	lines := strings.Split(stepYAML, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Remove leading dash if present
		trimmed = strings.TrimPrefix(trimmed, "-")
		trimmed = strings.TrimSpace(trimmed)

		if strings.HasPrefix(trimmed, "name:") {
			// Extract the name value after "name:"
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
			// Remove quotes if present
			name = strings.Trim(name, "\"'")
			return name
		}
	}
	return ""
}

// detectCycles uses DFS to detect cycles in the job dependency graph
func (jm *JobManager) detectCycles() error {
	jobLog.Print("Detecting cycles in job dependency graph")
	// Track visit states: 0=unvisited, 1=visiting, 2=visited
	visitState := make(map[string]int)

	// Initialize all jobs as unvisited
	for jobName := range jm.jobs {
		visitState[jobName] = 0
	}

	// Run DFS from each unvisited job
	for jobName := range jm.jobs {
		if visitState[jobName] == 0 {
			if err := jm.dfsVisit(jobName, visitState); err != nil {
				return err
			}
		}
	}

	jobLog.Print("No cycles detected in job dependencies")
	return nil
}

// dfsVisit performs DFS visit for cycle detection
func (jm *JobManager) dfsVisit(jobName string, visitState map[string]int) error {
	visitState[jobName] = 1 // Mark as visiting

	job := jm.jobs[jobName]
	for _, dep := range job.Needs {
		if visitState[dep] == 1 {
			// Found a back edge - cycle detected
			jobLog.Printf("Cycle detected: job %s has circular dependency through %s", jobName, dep)
			return fmt.Errorf("cycle detected in job dependencies: job '%s' has circular dependency through '%s'", jobName, dep)
		}
		if visitState[dep] == 0 {
			if err := jm.dfsVisit(dep, visitState); err != nil {
				return err
			}
		}
	}

	visitState[jobName] = 2 // Mark as visited
	return nil
}

// RenderToYAML generates the jobs section of a GitHub Actions workflow
func (jm *JobManager) RenderToYAML() string {
	jobLog.Printf("Rendering %d jobs to YAML", len(jm.jobs))
	if len(jm.jobs) == 0 {
		return "jobs:\n"
	}

	var yaml strings.Builder
	yaml.WriteString("jobs:\n")

	// jobOrder is kept sorted alphabetically by AddJob
	for _, jobName := range jm.jobOrder {
		job := jm.jobs[jobName]
		yaml.WriteString(jm.renderJob(job))
	}

	return yaml.String()
}

// renderJob renders a single job to YAML
func (jm *JobManager) renderJob(job *Job) string {
	var yaml strings.Builder

	fmt.Fprintf(&yaml, "  %s:\n", job.Name)

	// Add display name if present
	if job.DisplayName != "" {
		fmt.Fprintf(&yaml, "    name: %s\n", job.DisplayName)
	}

	// Add needs clause if there are dependencies
	if len(job.Needs) > 0 {
		if len(job.Needs) == 1 {
			fmt.Fprintf(&yaml, "    needs: %s\n", job.Needs[0])
		} else {
			yaml.WriteString("    needs:\n")
			// Sort needs for consistent output
			sortedNeeds := make([]string, len(job.Needs))
			copy(sortedNeeds, job.Needs)
			sort.Strings(sortedNeeds)
			for _, dep := range sortedNeeds {
				fmt.Fprintf(&yaml, "      - %s\n", dep)
			}
		}
	}

	// Add if condition if present
	if job.If != "" {
		// Add zizmor ignore comment if this job has workflow_run safety checks
		if job.HasWorkflowRunSafetyChecks {
			yaml.WriteString("    # zizmor: ignore[dangerous-triggers] - workflow_run trigger is secured with role and fork validation\n")
		}

		// Check if expression is multiline or longer than MaxExpressionLineLength characters
		if strings.Contains(job.If, "\n") || len(job.If) > int(constants.MaxExpressionLineLength) {
			// Use YAML folded style for multiline expressions or long expressions
			yaml.WriteString("    if: >\n")

			if strings.Contains(job.If, "\n") {
				// Already has newlines, use existing logic
				lines := strings.Split(job.If, "\n")
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						fmt.Fprintf(&yaml, "      %s\n", strings.TrimSpace(line))
					}
				}
			} else {
				// Long single-line expression, break it into logical lines
				lines := BreakLongExpression(job.If)
				for _, line := range lines {
					fmt.Fprintf(&yaml, "      %s\n", strings.TrimSpace(line))
				}
			}
		} else {
			// Single line expression that's not too long
			fmt.Fprintf(&yaml, "    if: %s\n", job.If)
		}
	}

	// Add runs-on
	if job.RunsOn != "" {
		fmt.Fprintf(&yaml, "    %s\n", job.RunsOn)
	}

	// Add environment section
	if job.Environment != "" {
		fmt.Fprintf(&yaml, "    %s\n", job.Environment)
	}

	// Add container section
	if job.Container != "" {
		fmt.Fprintf(&yaml, "    %s\n", job.Container)
	}

	// Add services section
	if job.Services != "" {
		fmt.Fprintf(&yaml, "    %s\n", job.Services)
	}

	// Add permissions section
	if job.Permissions != "" {
		fmt.Fprintf(&yaml, "    %s\n", job.Permissions)
	}

	// Add concurrency section
	if job.Concurrency != "" {
		fmt.Fprintf(&yaml, "    %s\n", job.Concurrency)
	}

	// Add timeout-minutes if specified
	if job.TimeoutMinutes > 0 {
		fmt.Fprintf(&yaml, "    timeout-minutes: %d\n", job.TimeoutMinutes)
	}

	// Add environment variables section
	if len(job.Env) > 0 {
		yaml.WriteString("    env:\n")
		// Sort environment variable keys for consistent output
		envKeys := make([]string, 0, len(job.Env))
		for key := range job.Env {
			envKeys = append(envKeys, key)
		}
		sort.Strings(envKeys)

		for _, key := range envKeys {
			fmt.Fprintf(&yaml, "      %s: %s\n", key, job.Env[key])
		}
	}

	// Add outputs section
	if len(job.Outputs) > 0 {
		yaml.WriteString("    outputs:\n")
		// Sort output keys for consistent output
		outputKeys := make([]string, 0, len(job.Outputs))
		for key := range job.Outputs {
			outputKeys = append(outputKeys, key)
		}
		sort.Strings(outputKeys)

		for _, key := range outputKeys {
			fmt.Fprintf(&yaml, "      %s: %s\n", key, job.Outputs[key])
		}
	}

	// Check if this is a reusable workflow call
	if job.Uses != "" {
		// Add uses directive for reusable workflow
		fmt.Fprintf(&yaml, "    uses: %s\n", job.Uses)

		// Add with parameters if present
		if len(job.With) > 0 {
			yaml.WriteString("    with:\n")
			// Sort keys for consistent output
			withKeys := make([]string, 0, len(job.With))
			for key := range job.With {
				withKeys = append(withKeys, key)
			}
			sort.Strings(withKeys)

			for _, key := range withKeys {
				value := job.With[key]
				// Format the value based on its type
				switch v := value.(type) {
				case string:
					fmt.Fprintf(&yaml, "      %s: %s\n", key, v)
				case int, int64, float64:
					fmt.Fprintf(&yaml, "      %s: %v\n", key, v)
				case bool:
					fmt.Fprintf(&yaml, "      %s: %t\n", key, v)
				default:
					fmt.Fprintf(&yaml, "      %s: %v\n", key, v)
				}
			}
		}

		// Add secrets if present
		if len(job.Secrets) > 0 {
			yaml.WriteString("    secrets:\n")
			// Sort secret keys for consistent output
			secretKeys := make([]string, 0, len(job.Secrets))
			for key := range job.Secrets {
				secretKeys = append(secretKeys, key)
			}
			sort.Strings(secretKeys)

			for _, key := range secretKeys {
				fmt.Fprintf(&yaml, "      %s: %s\n", key, job.Secrets[key])
			}
		}
	} else {
		// Add steps section (only for non-reusable workflow jobs)
		if len(job.Steps) > 0 {
			yaml.WriteString("    steps:\n")
			for _, step := range job.Steps {
				// Each step is already formatted with proper indentation
				yaml.WriteString(step)
			}
		}
	}

	// Add newline after each job for proper formatting
	yaml.WriteString("\n")

	return yaml.String()
}

// GetTopologicalOrder returns jobs in topological order (dependencies before dependents)
func (jm *JobManager) GetTopologicalOrder() ([]string, error) {
	jobLog.Printf("Computing topological order for %d jobs", len(jm.jobs))
	// First validate dependencies to ensure no cycles
	if err := jm.ValidateDependencies(); err != nil {
		return nil, err
	}

	// Track in-degree (number of incoming dependencies) for each job
	inDegree := make(map[string]int)
	for jobName := range jm.jobs {
		inDegree[jobName] = 0
	}

	// Calculate in-degrees: count how many dependencies each job has
	for _, job := range jm.jobs {
		inDegree[job.Name] = len(job.Needs)
	}

	// Start with jobs that have no dependencies (in-degree = 0)
	var queue []string
	for jobName, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, jobName)
		}
	}

	result := make([]string, 0, len(jm.jobs))

	// Process jobs in topological order
	for len(queue) > 0 {
		// Sort queue for consistent output
		sort.Strings(queue)

		// Take the first job from queue
		currentJob := queue[0]
		queue = queue[1:]
		result = append(result, currentJob)

		// For each job that depends on the current job, reduce its in-degree
		for jobName, job := range jm.jobs {
			for _, dep := range job.Needs {
				if dep == currentJob {
					inDegree[jobName]--
					if inDegree[jobName] == 0 {
						queue = append(queue, jobName)
					}
				}
			}
		}
	}

	return result, nil
}
