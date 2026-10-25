package taint

import (
	"regexp"
	"strings"
)

type stepOutputTransport struct {
	id       string
	bindings scope
	modeled  bool
}

var outputIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// Only a named transport with a modeled consumer scope is private. Keeping
// this separate from emit preserves Output sinks for job outputs and user rules.
func (a *analyzer) transportOutput(site Site, name string, sources value, transport *stepOutputTransport) {
	named := outputIdentifier.MatchString(name)
	resolved := transport != nil && outputIdentifier.MatchString(transport.id)
	if transport != nil {
		id := strings.ToLower(transport.id)
		if !resolved {
			id = "*"
		}
		key := strings.ToLower(name)
		if !named {
			key = "*"
		}
		ref := "steps." + id + ".outputs." + key
		transport.bindings[ref] = union(transport.bindings[ref], sources)
	}
	if len(sources) > 0 && transport != nil && (!named || !resolved) {
		a.gapAt(site.Path, site.Line, "step output name or step id is unresolved; downstream output tracking is conservative")
	}
	if a.rule.ID != "secret-leak" || !a.workflowContext || !named || !resolved || !transport.modeled {
		a.emit(site, sources)
	}
}
