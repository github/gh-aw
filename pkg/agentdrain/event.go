package agentdrain

import (
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var eventLog = logger.New("agentdrain:event")

// FlattenEvent converts an AgentEvent into a deterministic string suitable for
// template mining. Field keys are sorted alphabetically; fields listed in
// excludeFields are omitted. The result looks like:
//
//	stage=tool_call key1=val1 key2=val2
func FlattenEvent(evt AgentEvent, excludeFields []string) string {
	eventLog.Printf("Flattening event: stage=%s, fields=%d, exclude=%d", evt.Stage, len(evt.Fields), len(excludeFields))
	excluded := make(map[string]struct{}, len(excludeFields))
	for _, f := range excludeFields {
		excluded[f] = struct{}{}
	}

	keys := sliceutil.FilterMapKeys(evt.Fields, func(k string, _ string) bool {
		return !setutil.Contains(excluded, k)
	})
	sort.Strings(keys)

	parts := make([]string, 0, len(keys)+1)
	if evt.Stage != "" {
		parts = append(parts, "stage="+evt.Stage)
	}
	for _, k := range keys {
		parts = append(parts, k+"="+evt.Fields[k])
	}
	return strings.Join(parts, " ")
}

// StageSequence converts a slice of AgentEvents into a space-separated string
// of their stage names, e.g. "plan tool_call tool_result finish".
func StageSequence(events []AgentEvent) string {
	eventLog.Printf("Building stage sequence from %d event(s)", len(events))
	return strings.Join(sliceutil.Map(events, func(e AgentEvent) string { return e.Stage }), " ")
}
