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

func TestIsHeaderFile(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		expected bool
	}{
		{"C header", "foo.h", true},
		{"C++ .hpp header", "foo.hpp", true},
		{"C++ .hxx header", "foo.hxx", true},
		{"C++ .hh header", "foo.hh", true},
		{"C++ .h++ header", "foo.h++", true},
		{"source file", "foo.c", false},
		{"C++ source", "foo.cpp", false},
		{"object file", "foo.o", false},
		{"no extension", "foo", false},
		{"empty string", "", false},
		{"header in path", "/usr/include/boost/config.hpp", true},
		{"h in middle of name", "fooher.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHeaderFile(tt.file)
			if result != tt.expected {
				t.Errorf("isHeaderFile(%q) = %v, want %v",
					tt.file, result, tt.expected)
			}
		})
	}
}
