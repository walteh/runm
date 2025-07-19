package slogdevterm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTrimComponents(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []string
		totalLen int
		maxLen   int
		expected []string
	}{
		{
			name:     "simple trim",
			inputs:   []string{"longname", "file.go"},
			totalLen: 15,
			maxLen:   10,
			expected: []string{"lon~", "file.go"}, // excess=5, "longname"(8) -> keep 3 chars -> "lon~"
		},
		{
			name:     "no trimming needed",
			inputs:   []string{"pkg", "file.go"},
			totalLen: 10,
			maxLen:   15,
			expected: []string{"pkg", "file.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create copies to avoid modifying test data
			components := make([]*string, len(tt.inputs))
			for i, input := range tt.inputs {
				temp := input
				components[i] = &temp
			}

			trimComponents(components, tt.totalLen, tt.maxLen)

			result := make([]string, len(components))
			for i, comp := range components {
				result[i] = *comp
			}

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTrimMiddle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "no trimming needed",
			input:    "short.go",
			maxLen:   10,
			expected: "short.go",
		},
		{
			name:     "exact length",
			input:    "exact.go",
			maxLen:   8,
			expected: "exact.go",
		},
		{
			name:     "simple trim",
			input:    "verylongfilename.go",
			maxLen:   10,
			expected: "ver...o.go",
		},
		{
			name:     "shim.go example",
			input:    "shim.go",
			maxLen:   5,
			expected: "sh...",
		},
		{
			name:     "containerd example",
			input:    "containerd-shim-runc-v2.go",
			maxLen:   15,
			expected: "conta...c-v2.go",
		},
		{
			name:     "very short maxLen",
			input:    "file.go",
			maxLen:   3,
			expected: "...",
		},
		{
			name:     "maxLen 1",
			input:    "file.go",
			maxLen:   1,
			expected: ".",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   5,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimMiddle(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), tt.maxLen, "result should not exceed maxLen")
		})
	}
}
