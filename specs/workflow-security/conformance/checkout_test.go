package conformance

import (
	"strings"
	"testing"
)

func TestTemporaryCredentialLifecycle(t *testing.T) {
	for _, mutation := range []string{"", "ignored-error", "conditional", "missing-verification", "intervening-script", "sparse", "injected-reset"} {
		t.Run(mutation, func(t *testing.T) {
			doc := fixtureDocument(t)
			agent := doc.Jobs["agent"]
			agent.Steps[0].With["persist-credentials"] = true
			cleanup := step{Run: checkoutCleanup + "\n" + verifyCredentials}
			switch mutation {
			case "ignored-error":
				cleanup.ContinueOnError = true
			case "conditional":
				cleanup.If = "inputs.cleanup"
			case "missing-verification":
				cleanup.Run = checkoutCleanup
			}
			middle := []step{cleanup}
			if mutation == "intervening-script" {
				middle = append([]step{{Run: "echo agent-started"}}, middle...)
			}
			if mutation == "sparse" || mutation == "injected-reset" {
				reset := "git config --local --unset-all remote.origin.promisor || true\n" +
					"git config --local --unset-all remote.origin.partialclonefilter || true"
				if mutation == "injected-reset" {
					reset += "\necho agent-started"
				}
				middle = append([]step{{Run: reset}}, middle...)
			}
			agent.Steps = append(append(agent.Steps[:1:1], middle...), agent.Steps[1:]...)
			doc.Jobs["agent"] = agent
			got := strings.Join(verify(doc, "daily"), "\n")
			valid := mutation == "" || mutation == "sparse"
			if valid && got != "" {
				t.Fatalf("verified cleanup rejected: %s", got)
			}
			if !valid && !strings.Contains(got, "NoCredentialPersistence") {
				t.Fatalf("credential lifecycle mutation %q accepted", mutation)
			}
		})
	}
}
