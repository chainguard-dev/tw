package shelldeps

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		files          map[string]string // filename -> content
		packages       string            // comma-separated packages
		checkGNUCompat bool
		strict         bool
		wantError      bool
		wantOutput     []string // strings that should appear in output
		wantNoOutput   []string // strings that should NOT appear in output
	}{
		{
			name: "no issues with proper packages",
			files: map[string]string{
				"script.sh": `#!/bin/sh
grep pattern file
awk '{print $1}' data
`,
			},
			packages:       "busybox,bash",
			checkGNUCompat: true,
			strict:         false,
			wantError:      false,
			wantOutput:     []string{"No issues found"},
		},
		{
			name: "missing curl",
			files: map[string]string{
				"script.sh": `#!/bin/sh
curl https://example.com
grep pattern file
`,
			},
			packages:       "busybox,bash",
			checkGNUCompat: true,
			strict:         false,
			wantError:      false,
			wantOutput:     []string{"missing:", "curl"},
		},
		{
			name: "missing curl with strict mode",
			files: map[string]string{
				"script.sh": `#!/bin/sh
curl https://example.com
`,
			},
			packages:       "busybox",
			checkGNUCompat: true,
			strict:         true,
			wantError:      true,
			wantOutput:     []string{"curl"},
		},
		{
			name: "no missing with curl package",
			files: map[string]string{
				"script.sh": `#!/bin/sh
curl https://example.com
grep pattern file
`,
			},
			packages:       "busybox,curl",
			checkGNUCompat: true,
			strict:         true,
			wantError:      false,
			wantOutput:     []string{"No issues found"},
		},
		{
			name: "gnu compat issue - realpath --no-symlinks",
			files: map[string]string{
				"script.sh": `#!/bin/sh
path=$(realpath --no-symlinks /opt)
echo $path
`,
			},
			packages:       "busybox",
			checkGNUCompat: true,
			strict:         false,
			wantError:      false,
			wantOutput:     []string{"gnu-incompatible", "realpath", "--no-symlinks"},
		},
		{
			name: "gnu compat issue in strict mode",
			files: map[string]string{
				"script.sh": `#!/bin/sh
path=$(realpath --no-symlinks /opt)
`,
			},
			packages:       "busybox",
			checkGNUCompat: true,
			strict:         true,
			wantError:      true,
		},
		{
			name: "no gnu issue when coreutils present",
			files: map[string]string{
				"script.sh": `#!/bin/sh
path=$(realpath --no-symlinks /opt)
`,
			},
			packages:       "busybox,coreutils",
			checkGNUCompat: true,
			strict:         true,
			wantError:      false,
			wantOutput:     []string{"No issues found"},
		},
		{
			name: "skip gnu check when disabled",
			files: map[string]string{
				"script.sh": `#!/bin/sh
path=$(realpath --no-symlinks /opt)
`,
			},
			packages:       "busybox",
			checkGNUCompat: false,
			strict:         true,
			wantError:      false,
			wantOutput:     []string{"No issues found"},
			wantNoOutput:   []string{"gnu-incompatible"},
		},
		{
			name: "multiple scripts with mixed issues",
			files: map[string]string{
				"good.sh": `#!/bin/sh
grep pattern file
`,
				"bad.sh": `#!/bin/bash
curl https://example.com
path=$(realpath --no-symlinks /opt)
`,
			},
			packages:       "busybox",
			checkGNUCompat: true,
			strict:         false,
			wantError:      false,
			wantOutput:     []string{"curl", "realpath", "Issues found in 1 of 2"},
		},
		{
			name: "no packages specified - only gnu check",
			files: map[string]string{
				"script.sh": `#!/bin/sh
path=$(realpath --no-symlinks /opt)
`,
			},
			packages:       "",
			checkGNUCompat: true,
			strict:         false,
			wantError:      false,
			wantOutput:     []string{"gnu-incompatible"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory with test files
			tmpDir := t.TempDir()

			for filename, content := range tt.files {
				path := filepath.Join(tmpDir, filename)
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
			}

			// Create the check config
			parentCfg := &cfg{
				verbose: false,
				jsonOut: false,
			}

			checkCfg := &checkCfg{
				parent:         parentCfg,
				packages:       tt.packages,
				checkGNUCompat: tt.checkGNUCompat,
				strict:         tt.strict,
			}

			// Parse packages
			if checkCfg.packages != "" {
				checkCfg.packageList = strings.Split(checkCfg.packages, ",")
				for i, pkg := range checkCfg.packageList {
					checkCfg.packageList[i] = strings.TrimSpace(pkg)
				}
			}

			// Run the check (simplified version without cobra command)
			var output bytes.Buffer
			results, hasIssues := runCheck(t, tmpDir, checkCfg)

			// Output results
			err := checkCfg.outputResultsForTest(&output, results)
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

// runCheck is a test helper that runs the check logic without cobra
func runCheck(t *testing.T, searchDir string, cfg *checkCfg) ([]checkResult, bool) {
	t.Helper()

	// Find shell scripts
	var shellScripts []string
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}

		isShell, _ := isShellScript(path)
		if isShell {
			shellScripts = append(shellScripts, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk error: %v", err)
	}

	// Determine if we should skip GNU compat check
	hasCoreutils := HasGNUCoreutils(cfg.packageList)
	shouldCheckGNU := cfg.checkGNUCompat && !hasCoreutils

	// Process each script
	var results []checkResult
	hasIssues := false

	for _, file := range shellScripts {
		result := cfg.processScriptForTest(file, shouldCheckGNU)
		results = append(results, result)

		if len(result.Missing) > 0 || len(result.GNUIncompatible) > 0 || result.Error != "" {
			hasIssues = true
		}
	}

	return results, hasIssues
}

// processScriptForTest is a test helper (same as processScript but without context)
func (c *checkCfg) processScriptForTest(file string, checkGNU bool) checkResult {
	result := checkResult{File: file}

	f, err := os.Open(file)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer f.Close()

	// Extract shell
	shell, err := extractShebang(f)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Shell = shell

	// Reset for dep extraction
	if _, err := f.Seek(0, 0); err != nil {
		result.Error = err.Error()
		return result
	}

	// Extract dependencies
	deps, err := extractDeps(nil, f, file)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Deps = deps

	// Find missing commands if packages were specified
	if len(c.packageList) > 0 {
		result.Missing = FindMissingCommands(deps, c.packageList)
	}

	// Check GNU compatibility
	if checkGNU {
		if _, err := f.Seek(0, 0); err != nil {
			result.Error = err.Error()
			return result
		}

		incompatibilities, err := CheckGNUCompatibility(f, file)
		if err == nil {
			for _, inc := range incompatibilities {
				result.GNUIncompatible = append(result.GNUIncompatible, gnuIncompatResult{
					Command:     inc.Command,
					Line:        inc.Line,
					Description: inc.Description,
					Fix:         inc.Fix,
				})
			}
		}
	}

	return result
}

// outputResultsForTest is a test helper for outputting results
func (c *checkCfg) outputResultsForTest(output *bytes.Buffer, results []checkResult) error {
	return c.outputResults(output, results)
}

func TestCheckCommandJSON(t *testing.T) {
	// Create temporary directory with test file
	tmpDir := t.TempDir()

	content := `#!/bin/sh
curl https://example.com
path=$(realpath --no-symlinks /opt)
`
	path := filepath.Join(tmpDir, "script.sh")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create the check config with JSON output
	parentCfg := &cfg{
		verbose: false,
		jsonOut: true,
	}

	checkCfg := &checkCfg{
		parent:         parentCfg,
		packages:       "busybox",
		packageList:    []string{"busybox"},
		checkGNUCompat: true,
		strict:         false,
	}

	// Run check
	results, _ := runCheck(t, tmpDir, checkCfg)

	// Output as JSON
	var output bytes.Buffer
	err := checkCfg.outputResults(&output, results)
	if err != nil {
		t.Fatalf("outputResults error: %v", err)
	}

	outputStr := output.String()

	// Verify it's JSON (starts with [ and contains expected fields)
	if !strings.HasPrefix(strings.TrimSpace(outputStr), "[") {
		t.Errorf("JSON output should start with [, got: %s", outputStr[:50])
	}

	if !strings.Contains(outputStr, `"file"`) {
		t.Errorf("JSON output should contain 'file' field")
	}

	if !strings.Contains(outputStr, `"missing"`) {
		t.Errorf("JSON output should contain 'missing' field")
	}

	if !strings.Contains(outputStr, `"gnu_incompatible"`) {
		t.Errorf("JSON output should contain 'gnu_incompatible' field")
	}
}
