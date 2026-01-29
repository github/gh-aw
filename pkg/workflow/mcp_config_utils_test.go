//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestMapToolConfig_GetAny(t *testing.T) {
	tests := []struct {
		name      string
		config    MapToolConfig
		key       string
		wantValue any
		wantOk    bool
	}{
		{
			name: "existing string key",
			config: MapToolConfig{
				"name": "test-server",
			},
			key:       "name",
			wantValue: "test-server",
			wantOk:    true,
		},
		{
			name: "existing number key",
			config: MapToolConfig{
				"port": 8080,
			},
			key:       "port",
			wantValue: 8080,
			wantOk:    true,
		},
		{
			name: "existing boolean key",
			config: MapToolConfig{
				"enabled": true,
			},
			key:       "enabled",
			wantValue: true,
			wantOk:    true,
		},
		{
			name: "existing array key",
			config: MapToolConfig{
				"items": []string{"a", "b", "c"},
			},
			key:       "items",
			wantValue: []string{"a", "b", "c"},
			wantOk:    true,
		},
		{
			name: "existing object key",
			config: MapToolConfig{
				"nested": map[string]any{"key": "value"},
			},
			key:       "nested",
			wantValue: map[string]any{"key": "value"},
			wantOk:    true,
		},
		{
			name: "non-existent key",
			config: MapToolConfig{
				"foo": "bar",
			},
			key:       "missing",
			wantValue: nil,
			wantOk:    false,
		},
		{
			name:      "empty config",
			config:    MapToolConfig{},
			key:       "anything",
			wantValue: nil,
			wantOk:    false,
		},
		{
			name: "nil value",
			config: MapToolConfig{
				"nullable": nil,
			},
			key:       "nullable",
			wantValue: nil,
			wantOk:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOk := tt.config.GetAny(tt.key)

			if gotOk != tt.wantOk {
				t.Errorf("GetAny() gotOk = %v, want %v", gotOk, tt.wantOk)
			}

			// For non-existent keys, value should be nil
			if !tt.wantOk {
				if gotValue != nil {
					t.Errorf("GetAny() gotValue = %v, want nil for non-existent key", gotValue)
				}
				return
			}

			// For existing keys, compare values based on type
			switch wantVal := tt.wantValue.(type) {
			case string:
				if gotVal, ok := gotValue.(string); !ok || gotVal != wantVal {
					t.Errorf("GetAny() gotValue = %v, want %v", gotValue, tt.wantValue)
				}
			case int:
				if gotVal, ok := gotValue.(int); !ok || gotVal != wantVal {
					t.Errorf("GetAny() gotValue = %v, want %v", gotValue, tt.wantValue)
				}
			case bool:
				if gotVal, ok := gotValue.(bool); !ok || gotVal != wantVal {
					t.Errorf("GetAny() gotValue = %v, want %v", gotValue, tt.wantValue)
				}
			case nil:
				if gotValue != nil {
					t.Errorf("GetAny() gotValue = %v, want nil", gotValue)
				}
			default:
				// For complex types like arrays and objects, just verify they exist
				if gotValue == nil {
					t.Errorf("GetAny() gotValue = nil, want non-nil value")
				}
			}
		})
	}
}

func TestGetTypeString(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "nil value",
			value: nil,
			want:  "null",
		},
		{
			name:  "int value",
			value: 42,
			want:  "number",
		},
		{
			name:  "int64 value",
			value: int64(100),
			want:  "number",
		},
		{
			name:  "float64 value",
			value: 3.14,
			want:  "number",
		},
		{
			name:  "float32 value",
			value: float32(2.71),
			want:  "number",
		},
		{
			name:  "boolean true",
			value: true,
			want:  "boolean",
		},
		{
			name:  "boolean false",
			value: false,
			want:  "boolean",
		},
		{
			name:  "string value",
			value: "hello world",
			want:  "string",
		},
		{
			name:  "empty string",
			value: "",
			want:  "string",
		},
		{
			name: "object (map[string]any)",
			value: map[string]any{
				"key": "value",
			},
			want: "object",
		},
		{
			name:  "empty object",
			value: map[string]any{},
			want:  "object",
		},
		{
			name:  "array of strings",
			value: []string{"a", "b", "c"},
			want:  "array",
		},
		{
			name:  "array of ints",
			value: []int{1, 2, 3},
			want:  "array",
		},
		{
			name:  "array of any",
			value: []any{"mixed", 123, true},
			want:  "array",
		},
		{
			name:  "empty array",
			value: []string{},
			want:  "array",
		},
		{
			name:  "array of objects",
			value: []map[string]any{{"key": "value"}},
			want:  "array",
		},
		{
			name: "unknown type (struct)",
			value: struct {
				Name string
			}{Name: "test"},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTypeString(tt.value)
			if got != tt.want {
				t.Errorf("getTypeString(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestWriteArgsToYAMLInline(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no args",
			args: []string{},
			want: "",
		},
		{
			name: "single simple arg",
			args: []string{"--verbose"},
			want: `, "--verbose"`,
		},
		{
			name: "multiple simple args",
			args: []string{"--verbose", "--debug"},
			want: `, "--verbose", "--debug"`,
		},
		{
			name: "args with spaces",
			args: []string{"--message", "hello world"},
			want: `, "--message", "hello world"`,
		},
		{
			name: "args with quotes",
			args: []string{"--text", `say "hello"`},
			want: `, "--text", "say \"hello\""`,
		},
		{
			name: "args with special characters",
			args: []string{"--path", "/tmp/test\n\t"},
			want: `, "--path", "/tmp/test\n\t"`,
		},
		{
			name: "args with backslashes",
			args: []string{"--path", `C:\Windows\System32`},
			want: `, "--path", "C:\\Windows\\System32"`,
		},
		{
			name: "empty string arg",
			args: []string{""},
			want: `, ""`,
		},
		{
			name: "unicode args",
			args: []string{"--text", "Hello 世界 🌍"},
			want: `, "--text", "Hello 世界 🌍"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var builder strings.Builder
			writeArgsToYAMLInline(&builder, tt.args)
			got := builder.String()
			if got != tt.want {
				t.Errorf("writeArgsToYAMLInline() = %q, want %q", got, tt.want)
			}
		})
	}
}
