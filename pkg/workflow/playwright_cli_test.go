//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePlaywrightCLIInstallSteps_DefaultVersionUsesCooldown(t *testing.T) {
	steps := generatePlaywrightCLIInstallSteps(&WorkflowData{
		Tools: map[string]any{
			"playwright": map[string]any{
				"mode": "cli",
			},
		},
	})

	require.Len(t, steps, 3, "expected npm, browser, and skills install steps")

	installStep := strings.Join(steps[0], "\n")
	assert.Contains(t, installStep, "npm install -g @playwright/cli@"+string(constants.DefaultPlaywrightCLIVersion))
	assert.Contains(t, installStep, "NPM_CONFIG_MIN_RELEASE_AGE: '3'")
	assert.Contains(t, installStep, "timeout-minutes: 10")

	browserStep := strings.Join(steps[1], "\n")
	assert.Contains(t, browserStep, `bash "${RUNNER_TEMP}/gh-aw/actions/install_playwright_browsers.sh" chromium`)
	assert.Contains(t, browserStep, "PLAYWRIGHT_BROWSERS_PATH: ${RUNNER_TEMP}/gh-aw/playwright-browsers")
	assert.Contains(t, browserStep, "timeout-minutes: 10")

	skillsStep := strings.Join(steps[2], "\n")
	assert.Contains(t, skillsStep, "playwright-cli install --skills")
	assert.Contains(t, skillsStep, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD: '1'")
	assert.Contains(t, skillsStep, "PLAYWRIGHT_BROWSERS_PATH: ${RUNNER_TEMP}/gh-aw/playwright-browsers")
}

func TestGeneratePlaywrightCLIInstallSteps_ModeOmitted(t *testing.T) {
	steps := generatePlaywrightCLIInstallSteps(&WorkflowData{
		Tools: map[string]any{"playwright": nil},
	})

	require.Len(t, steps, 3)
	assert.Contains(t, strings.Join(steps[0], "\n"), "@playwright/cli@"+string(constants.DefaultPlaywrightCLIVersion))
}

func TestGeneratePlaywrightCLIInstallSteps_SelectedBrowsers(t *testing.T) {
	steps := generatePlaywrightCLIInstallSteps(&WorkflowData{
		Tools: map[string]any{"playwright": map[string]any{
			"browsers": []any{"chrome", "Firefox", "webkit", "chrome-for-testing", "chrome"},
		}},
	})

	assert.Contains(t, strings.Join(steps[1], "\n"), `install_playwright_browsers.sh" chromium firefox webkit`)
}
