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
		{
			name:     "custom prefix for pyca cryptography",
			path:     "/opt/pyca/cryptography/openssl",
			expected: "/opt/pyca/cryptography/openssl",
		},
		{
			name:     "prefix without leading slash",
			path:     "opt/custom/path",
			expected: "/opt/custom/path",
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

func TestIsDevPackage(t *testing.T) {
	tests := []struct {
		name     string
		pkg      string
		expected bool
	}{
		{"package ending with -dev", "libssl-dev", true},
		{"package ending with -devel", "kernel-devel", true},
		{"regular package", "bash", false},
		{"package with dev in middle", "devtools", false},
		{"empty string", "", false},
		{"package with -development", "python-development", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDevPackage(tt.pkg)
			if result != tt.expected {
				t.Errorf("IsDevPackage(%q) = %v, want %v",
					tt.pkg, result, tt.expected)
			}
		})
	}
}

// TestHeaderFileWithPrefixMatching tests the logic for matching header files under different prefixes
func TestHeaderFileWithPrefixMatching(t *testing.T) {
	tests := []struct {
		name           string
		file           string
		prefix         string
		shouldMatch    bool
		description    string
	}{
		{
			name:        "standard header under /usr with /usr prefix",
			file:        "/usr/include/openssl/ssl.h",
			prefix:      "/usr",
			shouldMatch: true,
			description: "Standard case: header in /usr/include",
		},
		{
			name:        "header under custom prefix matches",
			file:        "/opt/pyca/cryptography/openssl/include/openssl/ssl.h",
			prefix:      "/opt/pyca/cryptography/openssl",
			shouldMatch: true,
			description: "Custom prefix case: pyca cryptography OpenSSL headers",
		},
		{
			name:        "header under /usr does not match /opt prefix",
			file:        "/usr/include/openssl/ssl.h",
			prefix:      "/opt/custom",
			shouldMatch: false,
			description: "Prefix mismatch: file not under specified prefix",
		},
		{
			name:        "non-header file under prefix does not match",
			file:        "/usr/lib/libssl.so",
			prefix:      "/usr",
			shouldMatch: false,
			description: "Not a header file, should not match",
		},
		{
			name:        "header under /opt with /usr prefix does not match",
			file:        "/opt/custom/include/foo.h",
			prefix:      "/usr",
			shouldMatch: false,
			description: "Header exists but under wrong prefix",
		},
		{
			name:        "C++ header under custom prefix",
			file:        "/opt/local/include/boost/algorithm.hpp",
			prefix:      "/opt/local",
			shouldMatch: true,
			description: "C++ header under custom /opt/local prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizedPrefix := NormalizePath(tt.prefix)
			hasPrefix := false
			if len(tt.file) >= len(normalizedPrefix) {
				hasPrefix = tt.file[:len(normalizedPrefix)] == normalizedPrefix
			}
			isHeader := isHeaderFile(tt.file)
			matches := hasPrefix && isHeader

			if matches != tt.shouldMatch {
				t.Errorf("File %q with prefix %q: got match=%v, want match=%v\n  %s",
					tt.file, tt.prefix, matches, tt.shouldMatch, tt.description)
			}
		})
	}
}
