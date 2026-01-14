package checkers

import (
	"testing"
)

// TestPreparePathPrefix tests the preparePathPrefix utility function
func TestPreparePathPrefix(t *testing.T) {
	tests := []struct {
		name       string
		pathPrefix string
		expected   string
	}{
		{
			name:       "missing leading / symbol",
			pathPrefix: "usr/share",
			expected:   "/usr/share",
		},
		{
			name:       "existing leading / symbol",
			pathPrefix: "/opt/custom",
			expected:   "/opt/custom",
		},
		{
			name:       "empty path results in the default",
			pathPrefix: "",
			expected:   "/usr/share",
		},
		{
			name:       "something with trailing slash gets it removed",
			pathPrefix: "/usr/bin/",
			expected:   "/usr/bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preparePathPrefix(tt.pathPrefix)
			if result != tt.expected {
				t.Errorf("preparePathPrefix(%q) = %q, want %q",
					tt.pathPrefix, result, tt.expected)
			}
		})
	}
}
