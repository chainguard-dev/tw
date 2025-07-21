package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSymlink(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "symlink-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	targetFile := filepath.Join(tempDir, "target.txt")
	if err := ioutil.WriteFile(targetFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	tests := []struct {
		name           string
		setup          func() string
		relativeOnly   bool
		expectedPasses int64
		expectedFails  int64
		shouldSkip     bool // For relative-only mode skipping absolute symlinks
	}{
		{
			name: "valid relative symlink",
			setup: func() string {
				linkPath := filepath.Join(tempDir, "valid_relative")
				if err := os.Symlink("target.txt", linkPath); err != nil {
					t.Fatalf("Failed to create symlink: %v", err)
				}
				return linkPath
			},
			relativeOnly:   false,
			expectedPasses: 1,
			expectedFails:  0,
		},
		{
			name: "valid absolute symlink",
			setup: func() string {
				linkPath := filepath.Join(tempDir, "valid_absolute")
				if err := os.Symlink(targetFile, linkPath); err != nil {
					t.Fatalf("Failed to create symlink: %v", err)
				}
				return linkPath
			},
			relativeOnly:   false,
			expectedPasses: 1,
			expectedFails:  0,
		},
		{
			name: "absolute symlink skipped in relative-only mode",
			setup: func() string {
				linkPath := filepath.Join(tempDir, "absolute_skip")
				if err := os.Symlink(targetFile, linkPath); err != nil {
					t.Fatalf("Failed to create symlink: %v", err)
				}
				return linkPath
			},
			relativeOnly:   true,
			expectedPasses: 0,
			expectedFails:  0,
			shouldSkip:     true,
		},
		{
			name: "broken relative symlink",
			setup: func() string {
				linkPath := filepath.Join(tempDir, "broken_relative")
				if err := os.Symlink("nonexistent.txt", linkPath); err != nil {
					t.Fatalf("Failed to create broken symlink: %v", err)
				}
				return linkPath
			},
			relativeOnly:   false,
			expectedPasses: 0,
			expectedFails:  1,
		},
		{
			name: "broken absolute symlink",
			setup: func() string {
				linkPath := filepath.Join(tempDir, "broken_absolute")
				if err := os.Symlink("/nonexistent/target", linkPath); err != nil {
					t.Fatalf("Failed to create broken symlink: %v", err)
				}
				return linkPath
			},
			relativeOnly:   false,
			expectedPasses: 0,
			expectedFails:  1,
		},
		{
			name: "circular symlink",
			setup: func() string {
				linkPath := filepath.Join(tempDir, "circular")
				if err := os.Symlink("circular", linkPath); err != nil {
					t.Fatalf("Failed to create circular symlink: %v", err)
				}
				return linkPath
			},
			relativeOnly:   false,
			expectedPasses: 0,
			expectedFails:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linkPath := tt.setup()
			result := &Result{
				FailMessages: make([]string, 0),
			}

			checkSymlink(linkPath, result, tt.relativeOnly)

			if result.Passes != tt.expectedPasses {
				t.Errorf("Expected %d passes, got %d", tt.expectedPasses, result.Passes)
			}

			if result.Fails != tt.expectedFails {
				t.Errorf("Expected %d fails, got %d", tt.expectedFails, result.Fails)
			}
		})
	}
}

func TestCheckEscapeRoot(t *testing.T) {
	tests := []struct {
		name        string
		link        string
		target      string
		shouldError bool
	}{
		{
			name:        "normal relative path",
			link:        "/tmp/test/link",
			target:      "../file.txt",
			shouldError: false,
		},
		{
			name:        "path that would escape root (theoretical)",
			link:        "/test/link",
			target:      "../../../../../../../../../../etc/passwd",
			shouldError: false, // This still resolves to /etc/passwd, which is under root
		},
		{
			name:        "deeply nested normal path",
			link:        "/a/b/c/d/link",
			target:      "../../../file.txt",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkEscapeRoot(tt.link, tt.target)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for escaping path, got nil")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
		})
	}
}

func TestResultMethods(t *testing.T) {
	t.Run("AddPass", func(t *testing.T) {
		result := &Result{
			FailMessages: make([]string, 0),
		}

		result.AddPass("test pass message")

		if result.Passes != 1 {
			t.Errorf("Expected 1 pass, got %d", result.Passes)
		}

		if result.Fails != 0 {
			t.Errorf("Expected 0 fails, got %d", result.Fails)
		}
	})

	t.Run("AddFail", func(t *testing.T) {
		result := &Result{
			FailMessages: make([]string, 0),
		}

		result.AddFail("test fail message")

		if result.Passes != 0 {
			t.Errorf("Expected 0 passes, got %d", result.Passes)
		}

		if result.Fails != 1 {
			t.Errorf("Expected 1 fail, got %d", result.Fails)
		}

		if len(result.FailMessages) != 1 {
			t.Errorf("Expected 1 fail message, got %d", len(result.FailMessages))
		}

		if !strings.Contains(result.FailMessages[0], "test fail message") {
			t.Errorf("Expected fail message to contain 'test fail message', got %s", result.FailMessages[0])
		}
	})
}

func TestIsInPaths(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		paths    []string
		expected bool
	}{
		{
			name:     "file in single path",
			filePath: "/usr/bin/test",
			paths:    []string{"/usr/bin"},
			expected: true,
		},
		{
			name:     "file not in paths",
			filePath: "/etc/passwd",
			paths:    []string{"/usr/bin", "/usr/lib"},
			expected: false,
		},
		{
			name:     "exact match",
			filePath: "/usr/bin",
			paths:    []string{"/usr/bin"},
			expected: true,
		},
		{
			name:     "file in multiple paths",
			filePath: "/usr/lib/test.so",
			paths:    []string{"/usr/bin", "/usr/lib", "/usr/share"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInPaths(tt.filePath, tt.paths)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
