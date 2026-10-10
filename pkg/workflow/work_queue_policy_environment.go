package workflow

import (
	"fmt"
	"strings"
)

const workQueuePolicyChunkBytes = 12 * 1024

func workQueuePolicyJSONEnvironment(encoded string) []string {
	if len(encoded) <= workQueuePolicyChunkBytes {
		return []string{"          GH_AW_WORK_QUEUE_POLICY: " + fmt.Sprintf("%q", encoded) + "\n"}
	}
	chunks := splitWorkQueuePolicyJSON(encoded)
	lines := []string{fmt.Sprintf("          GH_AW_WORK_QUEUE_POLICY_PARTS: \"%d\"\n", len(chunks))}
	for index, chunk := range chunks {
		lines = append(lines, fmt.Sprintf("          GH_AW_WORK_QUEUE_POLICY_%d: %q\n", index, chunk))
	}
	return lines
}

// Split only at JSON delimiters so Actions expressions and UTF-8 stay intact.
func splitWorkQueuePolicyJSON(encoded string) []string {
	var chunks []string
	var chunk strings.Builder
	quoted, escaped := false, false
	for _, character := range encoded {
		chunk.WriteRune(character)
		if escaped {
			escaped = false
		} else if quoted && character == '\\' {
			escaped = true
		} else if character == '"' {
			quoted = !quoted
		}
		if chunk.Len() >= workQueuePolicyChunkBytes && !quoted && character == ',' {
			chunks = append(chunks, chunk.String())
			chunk.Reset()
		}
	}
	if chunk.Len() > 0 {
		chunks = append(chunks, chunk.String())
	}
	return chunks
}
