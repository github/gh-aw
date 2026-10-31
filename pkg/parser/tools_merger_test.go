package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSingleLedgerMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config map[string]any
		want   bool
	}{
		{name: "nil configuration is a single ledger", want: true},
		{name: "empty configuration is a single ledger", config: map[string]any{}, want: true},
		{name: "schema identifies a single ledger regardless of value", config: map[string]any{"schema": nil}, want: true},
		{name: "record size identifies a single ledger", config: map[string]any{"max-record-kb": 0}, want: true},
		{name: "segment size identifies a single ledger", config: map[string]any{"max-segment-kb": 256}, want: true},
		{name: "patch size identifies a single ledger", config: map[string]any{"max-patch-kb": nil}, want: true},
		{name: "string type identifies a single ledger", config: map[string]any{"type": "jsonl"}, want: true},
		{name: "empty string type still identifies a single ledger", config: map[string]any{"type": ""}, want: true},
		{name: "string key identifies a single ledger", config: map[string]any{"key": "識別子"}, want: true},
		{name: "empty string key still identifies a single ledger", config: map[string]any{"key": ""}, want: true},
		{name: "nil type does not identify a single ledger", config: map[string]any{"type": nil}},
		{name: "numeric type does not identify a single ledger", config: map[string]any{"type": 1}},
		{name: "named ledger called type is not a scalar option", config: map[string]any{"type": map[string]any{"schema": "events"}}},
		{name: "boolean key does not identify a single ledger", config: map[string]any{"key": false}},
		{name: "named ledger called key is not a scalar option", config: map[string]any{"key": map[string]any{"schema": "events"}}},
		{name: "enabled compaction identifies a single ledger", config: map[string]any{"compaction": true}, want: true},
		{name: "disabled compaction identifies a single ledger", config: map[string]any{"compaction": false}, want: true},
		{name: "compaction schedule identifies a single ledger", config: map[string]any{"compaction": map[string]any{"schedule": "daily"}}, want: true},
		{name: "minimum compaction segments identifies a single ledger", config: map[string]any{"compaction": map[string]any{"min-segments": 0}}, want: true},
		{name: "maximum compaction segments identifies a single ledger", config: map[string]any{"compaction": map[string]any{"max-segments": 10}}, want: true},
		{name: "compaction script presence identifies a single ledger", config: map[string]any{"compaction": map[string]any{"script": nil}}, want: true},
		{name: "empty compaction map does not identify a single ledger", config: map[string]any{"compaction": map[string]any{}}},
		{name: "nil compaction map does not identify a single ledger", config: map[string]any{"compaction": map[string]any(nil)}},
		{name: "unknown compaction option does not identify a single ledger", config: map[string]any{"compaction": map[string]any{"schema": "events"}}},
		{name: "string compaction does not identify a single ledger", config: map[string]any{"compaction": "daily"}},
		{name: "nil compaction does not identify a single ledger", config: map[string]any{"compaction": nil}},
		{name: "replay script presence identifies a single ledger", config: map[string]any{"replay": map[string]any{"script": nil}}, want: true},
		{name: "replay config presence identifies a single ledger", config: map[string]any{"replay": map[string]any{"config": nil}}, want: true},
		{name: "empty replay map does not identify a single ledger", config: map[string]any{"replay": map[string]any{}}},
		{name: "nil replay map does not identify a single ledger", config: map[string]any{"replay": map[string]any(nil)}},
		{name: "unknown replay option does not identify a single ledger", config: map[string]any{"replay": map[string]any{"schedule": "daily"}}},
		{name: "boolean replay does not identify a single ledger", config: map[string]any{"replay": false}},
		{name: "nil replay does not identify a single ledger", config: map[string]any{"replay": nil}},
		{name: "named ledgers do not identify a single ledger", config: map[string]any{"events": map[string]any{"type": "jsonl"}, "記録": map[string]any{"key": "id"}}},
		{name: "unrecognized scalar does not identify a single ledger", config: map[string]any{"unknown": "value"}},
		{name: "recognized option wins among unrecognized options", config: map[string]any{"unknown": nil, "type": 1, "key": "id"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isSingleLedgerMap(tt.config))
		})
	}
}
