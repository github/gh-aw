package parser

import "testing"

func TestUnmarshalJSONFromString(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "valid JSON with trailing whitespace", content: `{"value":true}  `},
		{name: "rejects a second JSON value", content: `{"value":true} {}`, wantErr: true},
		{name: "rejects malformed trailing content", content: `{"value":true} invalid`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value map[string]any
			err := unmarshalJSONFromString(tt.content, &value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unmarshalJSONFromString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
