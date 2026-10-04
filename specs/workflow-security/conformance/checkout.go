package conformance

import (
	"fmt"
	"regexp"
	"strings"
)

const verifyCredentials = `bash "${RUNNER_TEMP}/gh-aw/actions/verify_git_credentials.sh"`
const checkoutCleanup = `bash "${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials_checkout.sh"`
const finalCleanup = `bash "${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials.sh"`
const knownCleanup = `bash "${RUNNER_TEMP}/gh-aw/actions/clean_known_action_credentials.sh"`

var sparseReset = regexp.MustCompile(`^git(?: -C "\$\{\{ github\.workspace \}\}/[a-zA-Z0-9_./ -]+")? config --local --unset-all remote\.origin\.promisor \|\| true\n[ \t]*git(?: -C "\$\{\{ github\.workspace \}\}/[a-zA-Z0-9_./ -]+")? config --local --unset-all remote\.origin\.partialclonefilter \|\| true$`)

func isVerifiedCleanup(s step) bool {
	if s.If != "" || (s.ContinueOnError != nil && s.ContinueOnError != false) {
		return false
	}
	run := strings.TrimSpace(s.Run)
	return run == checkoutCleanup+"\n"+verifyCredentials ||
		run == finalCleanup+"\n"+verifyCredentials ||
		run == finalCleanup+"\n"+knownCleanup+"\n"+verifyCredentials
}

func checkCheckoutCredentials(steps []step) []string {
	var violations []string
	for index, s := range steps {
		if !strings.HasPrefix(s.Uses, "actions/checkout@") ||
			fmt.Sprint(s.With["persist-credentials"]) == "false" {
			continue
		}
		next := index + 1
		candidate, found := stepAt(steps, next)
		if found && sparseReset.MatchString(strings.TrimSpace(candidate.Run)) {
			next++
			candidate, found = stepAt(steps, next)
		}
		if !found || !isVerifiedCleanup(candidate) {
			violations = append(violations, "NoCredentialPersistence: retained checkout credentials lack immediate fail-closed cleanup and verification")
		}
	}
	return violations
}

func stepAt(steps []step, position int) (step, bool) {
	for index, candidate := range steps {
		if index == position {
			return candidate, true
		}
	}
	return step{}, false
}
