package shelldeps

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestCheckCommand(t *testing.T) {
	tests := []struct {
		name            string
		files           map[string]string // filename -> content
		binaries        []string          // binaries to create in fake PATH (as real binaries = coreutils)
		busyboxBinaries []string          // binaries to create as busybox symlinks
		strict          bool
		wantError       bool
		wantOutput      []string // strings that should appear in output
		wantNoOutput    []string // strings that should NOT appear in output
	}{
		{
			name: "no issues with available commands",
			files: map[string]string{
				"script.sh": `#!/bin/sh
grep pattern file
awk '{print $1}' data
`,
			},
			binaries:   []string{"grep", "awk"},
			strict:     false,
			wantError:  false,
			wantOutput: []string{"No issues found"},
		},
		{
			name: "missing curl",
			files: map[string]string{
				"script.sh": `#!/bin/sh
curl https://example.com
grep pattern file
`,
			},
			binaries:   []string{"grep"},
			strict:     false,
			wantError:  false,
			wantOutput: []string{"missing:", "curl"},
		},
		{
			name: "missing curl with strict mode",
			files: map[string]string{
				"script.sh": `#!/bin/sh
curl https://example.com
`,
			},
			binaries:  []string{},
			strict:    true,
			wantError: true,
			wantOutput: []string{"curl"},
		},
		{
			name: "gnu compat issue - realpath --no-symlinks",
			files: map[string]string{
				"script.sh": `#!/bin/sh
path=$(realpath --no-symlinks /opt)
echo $path
`,
			},
			binaries:        []string{"echo"},
			busyboxBinaries: []string{"realpath"},
			strict:          false,
			wantError:       false,
			wantOutput:      []string{"gnu-incompatible", "realpath", "--no-symlinks"},
		},
		{
			name: "gnu compat issue in strict mode",
			files: map[string]string{
				"script.sh": `#!/bin/sh
path=$(realpath --no-symlinks /opt)
`,
			},
			busyboxBinaries: []string{"realpath"},
			strict:          true,
			wantError:       true,
		},
		{
			name: "multiple files with mixed issues",
			files: map[string]string{
				"good.sh": `#!/bin/sh
grep pattern file
`,
				"bad.sh": `#!/bin/bash
curl https://example.com
path=$(realpath --no-symlinks /opt)
`,
			},
			binaries:        []string{"grep"},
			busyboxBinaries: []string{"realpath"},
			strict:          false,
			wantError:       false,
			wantOutput:      []string{"curl", "realpath", "Issues found in 1 of 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory with test files
			tmpDir := t.TempDir()
			scriptsDir := filepath.Join(tmpDir, "scripts")
			binDir := filepath.Join(tmpDir, "bin")

			if err := os.MkdirAll(scriptsDir, 0755); err != nil {
				t.Fatalf("failed to create scripts dir: %v", err)
			}
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create bin dir: %v", err)
			}

			// Create script files
			var scriptPaths []string
			for filename, content := range tt.files {
				path := filepath.Join(scriptsDir, filename)
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
				scriptPaths = append(scriptPaths, path)
			}

			// Create fake binaries in bin dir (these simulate coreutils)
			for _, bin := range tt.binaries {
				binPath := filepath.Join(binDir, bin)
				if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
					t.Fatalf("failed to create binary: %v", err)
				}
			}

			// Create busybox symlinks (these simulate busybox-provided commands)
			if len(tt.busyboxBinaries) > 0 {
				busyboxPath := filepath.Join(binDir, "busybox")
				if err := os.WriteFile(busyboxPath, []byte("#!/bin/sh\n"), 0755); err != nil {
					t.Fatalf("failed to create busybox: %v", err)
				}
				for _, bin := range tt.busyboxBinaries {
					binPath := filepath.Join(binDir, bin)
					if err := os.Symlink("busybox", binPath); err != nil {
						t.Fatalf("failed to create busybox symlink: %v", err)
					}
				}
			}

			// Run the check
			parentCfg := &cfg{
				verbose: false,
				jsonOut: false,
			}

			checkCfg := &checkCfg{
				parent:     parentCfg,
				searchPath: binDir,
				strict:     tt.strict,
			}

			// Process scripts
			var results []checkResult
			hasIssues := false
			ctx := context.Background()

			for _, file := range scriptPaths {
				result := checkCfg.processScript(ctx, file)
				results = append(results, result)

				if len(result.Missing) > 0 || len(result.GNUIncompatible) > 0 || result.Error != "" {
					hasIssues = true
				}
			}

			// Output results
			var output bytes.Buffer
			err := checkCfg.outputResults(&output, results)
			if err != nil {
				t.Fatalf("outputResults error: %v", err)
			}

			// Check for expected error
			gotError := tt.strict && hasIssues
			if gotError != tt.wantError {
				t.Errorf("wantError = %v, gotError = %v", tt.wantError, gotError)
			}

			// Check output contains expected strings
			outputStr := output.String()
			for _, want := range tt.wantOutput {
				if !strings.Contains(outputStr, want) {
					t.Errorf("output should contain %q, got:\n%s", want, outputStr)
				}
			}

			// Check output does not contain unwanted strings
			for _, notWant := range tt.wantNoOutput {
				if strings.Contains(outputStr, notWant) {
					t.Errorf("output should NOT contain %q, got:\n%s", notWant, outputStr)
				}
			}
		})
	}
}

func TestCheckCommandJSON(t *testing.T) {
	// Create temporary directory with test file
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	binDir := filepath.Join(tmpDir, "bin")

	os.MkdirAll(scriptsDir, 0755)
	os.MkdirAll(binDir, 0755)

	content := `#!/bin/sh
curl https://example.com
path=$(realpath --no-symlinks /opt)
`
	scriptPath := filepath.Join(scriptsDir, "script.sh")
	if err := os.WriteFile(scriptPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create realpath binary (to trigger GNU check)
	os.WriteFile(filepath.Join(binDir, "realpath"), []byte("#!/bin/sh\n"), 0755)

	// Run check with JSON output
	parentCfg := &cfg{
		verbose: false,
		jsonOut: true,
	}

	checkCfg := &checkCfg{
		parent:     parentCfg,
		searchPath: binDir,
		strict:     false,
	}

	ctx := context.Background()
	result := checkCfg.processScript(ctx, scriptPath)

	// Output as JSON
	var output bytes.Buffer
	err := checkCfg.outputResults(&output, []checkResult{result})
	if err != nil {
		t.Fatalf("outputResults error: %v", err)
	}

	outputStr := output.String()

	// Verify it's JSON (starts with [)
	if !strings.HasPrefix(strings.TrimSpace(outputStr), "[") {
		t.Errorf("JSON output should start with [, got: %s", outputStr[:50])
	}

	if !strings.Contains(outputStr, `"file"`) {
		t.Errorf("JSON output should contain 'file' field")
	}

	if !strings.Contains(outputStr, `"missing"`) {
		t.Errorf("JSON output should contain 'missing' field")
	}
}

func TestFindMissingInPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some binaries
	for _, bin := range []string{"grep", "awk", "sed"} {
		binPath := filepath.Join(tmpDir, bin)
		if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatalf("failed to create binary: %v", err)
		}
	}

	tests := []struct {
		name        string
		deps        []string
		wantMissing []string
	}{
		{
			name:        "no missing",
			deps:        []string{"grep", "awk"},
			wantMissing: nil,
		},
		{
			name:        "some missing",
			deps:        []string{"grep", "curl", "jq"},
			wantMissing: []string{"curl", "jq"},
		},
		{
			name:        "all missing",
			deps:        []string{"curl", "jq", "wget"},
			wantMissing: []string{"curl", "jq", "wget"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findMissingInPath(tt.deps, tmpDir)

			if len(got) != len(tt.wantMissing) {
				t.Errorf("findMissingInPath() returned %d items, want %d", len(got), len(tt.wantMissing))
				t.Logf("got: %v, want: %v", got, tt.wantMissing)
				return
			}

			gotMap := make(map[string]bool)
			for _, m := range got {
				gotMap[m] = true
			}

			for _, want := range tt.wantMissing {
				if !gotMap[want] {
					t.Errorf("expected %q to be missing", want)
				}
			}
		})
	}
}

func TestCheckGNUCompatWithPathAutoDetect(t *testing.T) {
	// This tests that when a command is provided by coreutils (real binary),
	// GNU-specific flags are NOT reported as issues.
	// When a command is provided by busybox (symlink), they ARE reported.

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	os.MkdirAll(binDir, 0755)

	// Create a "realpath" as a real binary (simulates coreutils)
	realpathPath := filepath.Join(binDir, "realpath")
	if err := os.WriteFile(realpathPath, []byte("#!/bin/sh\necho real"), 0755); err != nil {
		t.Fatalf("failed to create realpath: %v", err)
	}

	script := `#!/bin/sh
path=$(realpath --no-symlinks /opt)
`
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(script), "test.sh")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// With path pointing to coreutils-like binary, should NOT report issues
	issues := CheckGNUCompatWithPath(file, "test.sh", binDir)

	// Since it's a real binary (not busybox symlink), provider is "coreutils"
	// so issues should be filtered out
	if len(issues) != 0 {
		t.Errorf("expected 0 issues when coreutils provides the command, got %d", len(issues))
		for _, issue := range issues {
			t.Logf("  Issue: %s %s", issue.Command, issue.Flag)
		}
	}
}
