// Package conformance checks a narrow compiled-workflow security profile.
package conformance

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow"
	"gopkg.in/yaml.v3"
)

type document struct {
	Permissions map[string]string                     `yaml:"permissions"`
	Jobs        map[string]job                        `yaml:"jobs"`
	Policy      *workflow.GHAWManifestDetectionPolicy `yaml:"-"`
}

type job struct {
	Needs       yaml.Node         `yaml:"needs"`
	If          string            `yaml:"if"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []step            `yaml:"steps"`
	Outputs     map[string]string `yaml:"outputs"`
}

type step struct {
	ID              string         `yaml:"id"`
	Run             string         `yaml:"run"`
	If              string         `yaml:"if"`
	ContinueOnError any            `yaml:"continue-on-error"`
	Uses            string         `yaml:"uses"`
	With            map[string]any `yaml:"with"`
}

type report struct {
	Profile     string                                `json:"profile"`
	Lock        string                                `json:"lock"`
	Conforms    bool                                  `json:"conforms"`
	Obligations []string                              `json:"obligations"`
	Violations  []string                              `json:"violations"`
	Detection   *workflow.GHAWManifestDetectionPolicy `json:"detection_policy"`
}

var pinnedAction = regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)

func dependencies(node yaml.Node) ([]string, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		return []string{node.Value}, nil
	case yaml.SequenceNode:
		var needs []string
		if err := node.Decode(&needs); err != nil {
			return nil, err
		}
		return needs, nil
	default:
		return nil, errors.New("needs must be a job name or sequence")
	}
}

func checkAgent(doc document) []string {
	var violations []string
	agent := doc.Jobs["agent"]
	if err := requireJobResults(agent.If, "activation", []string{"success"}, true); err != nil {
		violations = append(violations, "JobIsolation: "+err.Error())
	}
	permissions := agent.Permissions
	if permissions == nil {
		permissions = doc.Permissions
	}
	for scope, level := range permissions {
		if level == "write" && scope != "copilot-requests" && scope != "id-token" {
			violations = append(violations, "JobIsolation: agent write scope "+scope)
		}
	}
	violations = append(violations, checkCheckoutCredentials(agent.Steps)...)
	return violations
}

func checkSeed(doc document) []string {
	var violations []string
	found := false
	for _, s := range doc.Jobs["agent"].Steps {
		if s.With["repository"] == "example/private-dependency" {
			found = true
			if s.With["token"] != "${{ secrets.MODEL_DEPENDENCY_READ_TOKEN }}" {
				violations = append(violations, "GitAuthorization: dependency lost per-checkout token")
			}
		}
	}
	if !found {
		violations = append(violations, "GitAuthorization: dependency checkout missing")
	}
	minted := false
	for _, s := range doc.Jobs["safe_outputs"].Steps {
		if !strings.HasPrefix(s.Uses, "actions/create-github-app-token@") {
			continue
		}
		minted = true
		if s.With["repositories"] != "gh-aw" || s.With["permission-issues"] != "write" {
			violations = append(violations, "AppLeastPrivilege: missing repository/issue-write scope")
		}
		for key := range s.With {
			if strings.HasPrefix(key, "permission-") && key != "permission-issues" {
				violations = append(violations, "AppLeastPrivilege: unexpected "+key)
			}
		}
		if fmt.Sprint(s.With["skip-token-revoke"]) == "true" {
			violations = append(violations, "TokenLifetime: app token revocation disabled")
		}
	}
	if !minted {
		violations = append(violations, "AppLeastPrivilege: no app token mint step")
	}
	return violations
}

func verify(doc document, profile string) []string {
	policy, violations := effectivePolicy(doc, profile)
	violations = append(violations, checkGraph(doc, policy)...)
	violations = append(violations, checkAgent(doc)...)
	violations = append(violations, checkArtifacts(doc)...)
	for name, j := range doc.Jobs {
		for _, s := range j.Steps {
			if s.Uses != "" && !strings.HasPrefix(s.Uses, "./") && !pinnedAction.MatchString(s.Uses) {
				violations = append(violations, "TrustedConfiguration: unpinned action in "+name)
			}
		}
	}
	if profile == "seed" {
		violations = append(violations, checkSeed(doc)...)
	}
	slices.Sort(violations)
	return violations
}

// Run verifies the compiled lock selected by the command-line flags.
func Run() error {
	profile := flag.String("profile", "seed", "Supported profile: seed, daily, or compiled")
	flag.Parse()
	if flag.NArg() != 1 || (*profile != "seed" && *profile != "daily" && *profile != "compiled") {
		return errors.New("usage: conformance --profile seed|daily|compiled compiled.lock.yml")
	}
	data, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		return err
	}
	var doc document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse compiled YAML: %w", err)
	}
	manifest, err := workflow.ExtractGHAWManifestFromLockFile(string(data))
	if err != nil {
		return err
	}
	if manifest != nil {
		doc.Policy = manifest.ThreatDetection
	}
	policy, _ := effectivePolicy(doc, *profile)
	violations := verify(doc, *profile)
	result := report{Profile: *profile, Lock: flag.Arg(0), Conforms: len(violations) == 0,
		Obligations: []string{"JobIsolation", "NoCredentialPersistence", "ArtifactProvenance",
			"DetectionGate", "TrustedConfiguration"}, Violations: violations, Detection: policy}
	if *profile == "seed" {
		result.Obligations = append(result.Obligations, "GitAuthorization", "AppLeastPrivilege", "TokenLifetime")
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return err
	}
	if !result.Conforms {
		return fmt.Errorf("compiled security profile failed with %d violation(s)", len(violations))
	}
	return nil
}
