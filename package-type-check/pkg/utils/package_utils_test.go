package utils

import (
	"testing"
)

// TestNormalizePath tests the NormalizePath utility function
func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "missing leading / symbol",
			path:     "usr/share",
			expected: "/usr/share",
		},
		{
			name:     "existing leading / symbol",
			path:     "/opt/custom",
			expected: "/opt/custom",
		},
		{
			name:     "empty path is just root",
			path:     "",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePath(tt.path)
			if result != tt.expected {
				t.Errorf("NormalizePath(%q) = %q, want %q",
					tt.path, result, tt.expected)
			}
		})
	}
}
