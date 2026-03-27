package dryrun

import (
	"testing"
)

func TestEscapeJSONPointer(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output string
	}{
		{
			name:   "No special characters",
			input:  "test",
			output: "test",
		},
		{
			name:   "Tilde",
			input:  "test~key",
			output: "test~0key",
		},
		{
			name:   "Forward slash",
			input:  "test/key",
			output: "test~1key",
		},
		{
			name:   "Both tilde and slash",
			input:  "test~key/key",
			output: "test~0key~1key",
		},
		{
			name:   "Multiple special chars",
			input:  "a/b/c~d",
			output: "a~1b~1c~0d",
		},
		{
			name:   "Empty string",
			input:  "",
			output: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeJSONPointer(tt.input)
			if result != tt.output {
				t.Errorf("escapeJSONPointer(%q) = %q, want %q", tt.input, result, tt.output)
			}
		})
	}
}
