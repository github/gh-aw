package workflow

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
)

const minimumCooldown = 5 * time.Minute

func init() {
	ghAwOnSectionKeys["cooldown"] = true
}

func extractCooldown(frontmatter map[string]any) (time.Duration, error) {
	value, exists := extractOnTriggerValue(frontmatter, "cooldown")
	if !exists {
		return 0, nil
	}

	raw, ok := value.(string)
	if !ok {
		return 0, fmt.Errorf("on.cooldown: expected a duration string, got %T", value)
	}
	if strings.Contains(raw, "${{") {
		return 0, errors.New("on.cooldown: GitHub Actions expressions are not supported")
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("on.cooldown: invalid duration %q; use Go duration syntax such as \"5m\", \"1h\", or \"1h30m\"", raw)
	}
	if duration < minimumCooldown {
		return 0, fmt.Errorf("on.cooldown: duration %q must be at least %s", raw, minimumCooldown)
	}

	return duration, nil
}

func (c *Compiler) generateCooldownCheck(data *WorkflowData, steps []string) []string {
	compilerActivationJobsLog.Printf("Adding cooldown check step: cooldown=%s", data.Cooldown)
	steps = append(steps, "      - name: Check workflow cooldown\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckCooldownStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	steps = append(steps, "        env:\n")
	cooldownSeconds := int64((data.Cooldown + time.Second - time.Nanosecond) / time.Second)
	steps = append(steps, fmt.Sprintf("          GH_AW_COOLDOWN_SECONDS: \"%d\"\n", cooldownSeconds))
	steps = append(steps, "        with:\n")
	steps = append(steps, "          github-token: ${{ secrets.GITHUB_TOKEN }}\n")
	steps = append(steps, "          script: |\n")
	return append(steps, generateGitHubScriptWithRequire("check_cooldown.cjs"))
}
