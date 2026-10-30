package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var ghAwSetupLog = logger.New("workflow:gh_aw_setup_steps")

type ghAwSetupStepConfig struct {
	actionMode           ActionMode
	ifCondition          string
	cliVersion           string
	actionRepo           string
	fallbackActionRefTag string
	workflowData         *WorkflowData
	withFields           map[string]string
}

func generateGhAwSetupStep(config ghAwSetupStepConfig) (GitHubActionStep, error) {
	ghAwSetupLog.Printf("Generating gh-aw setup step: action_mode=%s, action_repo=%s, cli_version=%s, with_field_count=%d",
		config.actionMode, config.actionRepo, config.cliVersion, len(config.withFields))
	if config.actionMode == ActionModeDev {
		ghAwSetupLog.Print("Using dev mode: build and install gh-aw CLI from source")
		step := GitHubActionStep{"      - name: Build and install gh-aw CLI from source"}
		if config.ifCondition != "" {
			step = append(step, "        if: "+config.ifCondition)
		}
		step = append(step,
			"        run: |",
			"          gh extension remove aw || true",
			"          make build",
			"          gh extension install .",
			"          gh aw version",
			"        env:",
			"          GH_TOKEN: ${{ github.token }}",
		)
		return step, nil
	}

	// Pinning errors are non-fatal: we still emit a valid step with the fallback
	// action reference so compilation and workflow execution can continue.
	actionRef, pinErr := resolveGhAwSetupActionRef(config)
	if pinErr != nil {
		ghAwSetupLog.Printf("Action ref resolution returned non-fatal error: %v (using fallback ref %s)", pinErr, actionRef)
	} else {
		ghAwSetupLog.Printf("Resolved action ref: %s", actionRef)
	}
	step := GitHubActionStep{
		"      - name: Install gh-aw extension",
	}
	if config.ifCondition != "" {
		step = append(step, "        if: "+config.ifCondition)
	}
	step = append(step, "        uses: "+actionRef)
	step = append(step, "        with:")
	step = append(step, fmt.Sprintf("          version: '%s'", config.cliVersion))

	return appendSortedWithFieldEntries(step, config.withFields), pinErr
}

// resolveGhAwSetupActionRef resolves the setup-cli action reference in priority order:
//  1. Use workflow-aware pin resolution when WorkflowData exists.
//  2. Otherwise use the static pin table (getActionPin) when available.
//  3. Otherwise fall back to repo@tag, then repo with no ref as a final fallback.
func resolveGhAwSetupActionRef(config ghAwSetupStepConfig) (reference string, err error) {
	defer func() { config.workflowData.recordGeneratedActionRef(reference) }()
	if config.workflowData != nil {
		actionRef := fmt.Sprintf("%s@%s", config.actionRepo, config.cliVersion)
		pinnedRef, err := getGeneratedActionPinWithData(config.actionRepo, config.cliVersion, config.workflowData)
		if err != nil {
			return actionRef, err
		}
		if pinnedRef != "" {
			ghAwSetupLog.Printf("Using workflow-aware action pin: %s", pinnedRef)
			return pinnedRef, nil
		}
		ghAwSetupLog.Printf("No workflow-aware pin available, falling back to repo@tag: %s", actionRef)
		return actionRef, nil
	}

	actionRef := getActionPin(config.actionRepo)
	if actionRef != "" {
		ghAwSetupLog.Printf("Using static action pin: %s", actionRef)
		return actionRef, nil
	}

	if config.fallbackActionRefTag != "" {
		fallback := fmt.Sprintf("%s@%s", config.actionRepo, config.fallbackActionRefTag)
		ghAwSetupLog.Printf("Using fallback action ref tag: %s", fallback)
		return fallback, nil
	}
	ghAwSetupLog.Printf("Using bare action repo (no ref): %s", config.actionRepo)
	return config.actionRepo, nil
}
