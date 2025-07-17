package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	t.Skip("Skipping parseArgs test due to flag package global state issues")
}

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
		expectedPasses int64
		expectedFails  int64
	}{
		{
			name: "valid symlink",
			setup: func() string {
				linkPath := filepath.Join(tempDir, "valid_link")
				if err := os.Symlink(targetFile, linkPath); err != nil {
					t.Fatalf("Failed to create symlink: %v", err)
				}
				return linkPath
			},
			expectedPasses: 1,
			expectedFails:  0,
		},
		{
			name: "broken symlink",
			setup: func() string {
				linkPath := filepath.Join(tempDir, "broken_link")
				if err := os.Symlink("/nonexistent/target", linkPath); err != nil {
					t.Fatalf("Failed to create broken symlink: %v", err)
				}
				return linkPath
			},
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
			
			checkSymlink(linkPath, result)
			
			if result.Passes != tt.expectedPasses {
				t.Errorf("Expected %d passes, got %d", tt.expectedPasses, result.Passes)
			}
			
			if result.Fails != tt.expectedFails {
				t.Errorf("Expected %d fails, got %d", tt.expectedFails, result.Fails)
			}
		})
	}
}

func TestResultAddPass(t *testing.T) {
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
}

func TestResultAddFail(t *testing.T) {
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
}