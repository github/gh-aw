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

	"gopkg.in/yaml.v3"
)

type document struct {
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]job    `yaml:"jobs"`
}

type job struct {
	Needs       yaml.Node         `yaml:"needs"`
	If          string            `yaml:"if"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []step            `yaml:"steps"`
}

type step struct {
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
}

type report struct {
	Profile     string   `json:"profile"`
	Lock        string   `json:"lock"`
	Conforms    bool     `json:"conforms"`
	Obligations []string `json:"obligations"`
	Violations  []string `json:"violations"`
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

func checkGraph(doc document) []string {
	var violations []string
	for _, name := range []string{"activation", "agent", "detection", "safe_outputs"} {
		if _, ok := doc.Jobs[name]; !ok {
			violations = append(violations, "JobIsolation: missing "+name+" job")
		}
	}
	for name, prerequisites := range map[string][]string{
		"agent": {"activation"}, "detection": {"agent"}, "safe_outputs": {"agent", "detection"},
	} {
		needs, err := dependencies(doc.Jobs[name].Needs)
		if err != nil {
			violations = append(violations, "JobIsolation: "+name+": "+err.Error())
			continue
		}
		for _, prerequisite := range prerequisites {
			if !slices.Contains(needs, prerequisite) {
				violations = append(violations, "JobIsolation: "+name+" lacks "+prerequisite)
			}
		}
	}
	condition := strings.Join(strings.Fields(doc.Jobs["safe_outputs"].If), " ")
	if condition != "needs.detection.result == 'success'" &&
		condition != "(!cancelled()) && needs.agent.result != 'skipped' && needs.detection.result == 'success'" {
		violations = append(violations, "DetectionGate: missing explicit detection-success condition")
	}
	return violations
}

func checkAgent(doc document) []string {
	var violations []string
	agent := doc.Jobs["agent"]
	for _, statusFunction := range []string{"always(", "failure(", "cancelled("} {
		if strings.Contains(agent.If, statusFunction) {
			violations = append(violations, "JobIsolation: agent condition outside implicit-success profile")
		}
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
	for _, s := range agent.Steps {
		if strings.HasPrefix(s.Uses, "actions/checkout@") &&
			fmt.Sprint(s.With["persist-credentials"]) != "false" {
			violations = append(violations, "NoCredentialPersistence: agent checkout retains credentials")
		}
	}
	return violations
}

func checkArtifacts(doc document) []string {
	uploaded := map[string]struct{}{}
	for _, s := range doc.Jobs["agent"].Steps {
		if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
			uploaded[fmt.Sprint(s.With["name"])] = struct{}{}
		}
	}
	var violations []string
	found := false
	for _, s := range doc.Jobs["safe_outputs"].Steps {
		if !strings.HasPrefix(s.Uses, "actions/download-artifact@") {
			continue
		}
		if _, ok := s.With["run-id"]; ok {
			violations = append(violations, "ArtifactProvenance: cross-run download")
		}
		if _, ok := s.With["repository"]; ok {
			violations = append(violations, "ArtifactProvenance: cross-repository download")
		}
		name := fmt.Sprint(s.With["name"])
		pattern := fmt.Sprint(s.With["pattern"])
		if name == "agent" || pattern == "{agent,agent-output-fallback}" {
			_, present := uploaded["agent"]
			found = found || present
		}
	}
	if !found {
		violations = append(violations, "ArtifactProvenance: no matching agent artifact handoff")
	}
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
	violations := checkGraph(doc)
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
	profile := flag.String("profile", "seed", "Supported profile: seed or daily")
	flag.Parse()
	if flag.NArg() != 1 || (*profile != "seed" && *profile != "daily") {
		return errors.New("usage: conformance --profile seed|daily compiled.lock.yml")
	}
	data, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		return err
	}
	var doc document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse compiled YAML: %w", err)
	}
	violations := verify(doc, *profile)
	result := report{Profile: *profile, Lock: flag.Arg(0), Conforms: len(violations) == 0,
		Obligations: []string{"JobIsolation", "NoCredentialPersistence", "ArtifactProvenance",
			"DetectionGate", "TrustedConfiguration"}, Violations: violations}
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
