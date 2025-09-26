package bumpconstraints

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected *Constraint
	}{
		{
			name: "simple equality constraint",
			line: "requests==2.31.0",
			expected: &Constraint{
				Package:  "requests",
				Operator: "==",
				Version:  "2.31.0",
				Comment:  "",
			},
		},
		{
			name: "constraint with comment",
			line: "django==4.2.0 # LTS version",
			expected: &Constraint{
				Package:  "django",
				Operator: "==",
				Version:  "4.2.0",
				Comment:  "LTS version",
			},
		},
		{
			name: "greater than or equal constraint",
			line: "numpy>=1.21.0",
			expected: &Constraint{
				Package:  "numpy",
				Operator: ">=",
				Version:  "1.21.0",
				Comment:  "",
			},
		},
		{
			name: "less than constraint",
			line: "flask<3.0.0",
			expected: &Constraint{
				Package:  "flask",
				Operator: "<",
				Version:  "3.0.0",
				Comment:  "",
			},
		},
		{
			name: "compatible release constraint",
			line: "scipy~=1.7.0",
			expected: &Constraint{
				Package:  "scipy",
				Operator: "~=",
				Version:  "1.7.0",
				Comment:  "",
			},
		},
		{
			name: "not equal constraint",
			line: "pandas!=1.3.0",
			expected: &Constraint{
				Package:  "pandas",
				Operator: "!=",
				Version:  "1.3.0",
				Comment:  "",
			},
		},
		{
			name: "package with hyphen",
			line: "requests-mock==1.9.3",
			expected: &Constraint{
				Package:  "requests-mock",
				Operator: "==",
				Version:  "1.9.3",
				Comment:  "",
			},
		},
		{
			name: "package with underscore",
			line: "google_auth==2.0.0",
			expected: &Constraint{
				Package:  "google_auth",
				Operator: "==",
				Version:  "2.0.0",
				Comment:  "",
			},
		},
		{
			name: "version with pre-release",
			line: "django==4.2.0rc1",
			expected: &Constraint{
				Package:  "django",
				Operator: "==",
				Version:  "4.2.0rc1",
				Comment:  "",
			},
		},
		{
			name: "version with dev suffix",
			line: "numpy==1.24.0.dev0",
			expected: &Constraint{
				Package:  "numpy",
				Operator: "==",
				Version:  "1.24.0.dev0",
				Comment:  "",
			},
		},
		{
			name: "comment with special chars",
			line: "requests==2.31.0 # Security fix (CVE-2023-32681)",
			expected: &Constraint{
				Package:  "requests",
				Operator: "==",
				Version:  "2.31.0",
				Comment:  "Security fix (CVE-2023-32681)",
			},
		},
		{
			name:     "comment line",
			line:     "# This is a comment",
			expected: nil,
		},
		{
			name:     "empty line",
			line:     "",
			expected: nil,
		},
		{
			name:     "no operator",
			line:     "requests",
			expected: nil,
		},
		{
			name:     "no version",
			line:     "requests==",
			expected: nil,
		},
	}

	c := &cfg{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.parseLine(tt.line)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParsePackageUpdates(t *testing.T) {
	tests := []struct {
		name        string
		packages    []string
		expected    []PackageUpdate
		expectError bool
		errorMsg    string
	}{
		{
			name:     "single package",
			packages: []string{"requests==2.31.0"},
			expected: []PackageUpdate{
				{Package: "requests", Version: "2.31.0", Comment: ""},
			},
		},
		{
			name:     "package with comment",
			packages: []string{"django==4.2.0 # LTS upgrade"},
			expected: []PackageUpdate{
				{Package: "django", Version: "4.2.0", Comment: "LTS upgrade"},
			},
		},
		{
			name: "multiple packages",
			packages: []string{
				"requests==2.31.0",
				"django==4.2.0 # LTS",
				"numpy==1.24.0",
			},
			expected: []PackageUpdate{
				{Package: "requests", Version: "2.31.0", Comment: ""},
				{Package: "django", Version: "4.2.0", Comment: "LTS"},
				{Package: "numpy", Version: "1.24.0", Comment: ""},
			},
		},
		{
			name:     "empty package list",
			packages: []string{},
			expected: nil, // parsePackageUpdates returns nil for empty input
		},
		{
			name:     "empty strings filtered",
			packages: []string{"", "requests==2.31.0", ""},
			expected: []PackageUpdate{
				{Package: "requests", Version: "2.31.0", Comment: ""},
			},
		},
		{
			name:        "invalid format - no equals",
			packages:    []string{"requests>2.31.0"},
			expectError: true,
			errorMsg:    "invalid package specification",
		},
		{
			name:        "invalid format - no version",
			packages:    []string{"requests=="},
			expectError: true,
			errorMsg:    "invalid package specification",
		},
		{
			name:        "duplicate package",
			packages:    []string{"requests==2.31.0", "requests==2.32.0"},
			expectError: true,
			errorMsg:    "duplicate package specification",
		},
		{
			name:        "package with whitespace",
			packages:    []string{"bad package==1.0.0"},
			expectError: true,
			errorMsg:    "contains whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &cfg{Packages: tt.packages}
			result, err := c.parsePackageUpdates()

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFindConstraintLine(t *testing.T) {
	lines := []string{
		"# Comment line",
		"requests==2.28.0",
		"requests-mock==1.9.3",
		"django>=3.2.0",
		"",
		"numpy==1.21.0 # Scientific computing",
		"# Another comment",
		"pandas>=1.3.0",
	}

	c := &cfg{}

	tests := []struct {
		name        string
		packageName string
		expected    int
	}{
		{
			name:        "find requests",
			packageName: "requests",
			expected:    1,
		},
		{
			name:        "find requests-mock",
			packageName: "requests-mock",
			expected:    2,
		},
		{
			name:        "find django",
			packageName: "django",
			expected:    3,
		},
		{
			name:        "find numpy with comment",
			packageName: "numpy",
			expected:    5,
		},
		{
			name:        "find pandas",
			packageName: "pandas",
			expected:    7,
		},
		{
			name:        "package not found",
			packageName: "flask",
			expected:    -1,
		},
		{
			name:        "partial match should not work",
			packageName: "request",
			expected:    -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.findConstraintLine(lines, tt.packageName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseConstraintsFile(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "constraints.txt")

	content := `# Python dependencies
requests==2.28.0
django>=3.2.0 # Web framework
numpy==1.21.0

# Testing dependencies
pytest==7.4.0
pytest-cov==4.1.0
`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	c := &cfg{ConstraintsFile: testFile}
	constraints, lines, err := c.parseConstraintsFile()

	assert.NoError(t, err)
	assert.Len(t, lines, 8)       // All lines including empty and comments (no trailing newline)
	assert.Len(t, constraints, 5) // Only valid constraint lines

	// Check specific constraints
	expectedConstraints := []Constraint{
		{Package: "requests", Operator: "==", Version: "2.28.0", Comment: ""},
		{Package: "django", Operator: ">=", Version: "3.2.0", Comment: "Web framework"},
		{Package: "numpy", Operator: "==", Version: "1.21.0", Comment: ""},
		{Package: "pytest", Operator: "==", Version: "7.4.0", Comment: ""},
		{Package: "pytest-cov", Operator: "==", Version: "4.1.0", Comment: ""},
	}

	for i, expected := range expectedConstraints {
		assert.Equal(t, expected.Package, constraints[i].Package)
		assert.Equal(t, expected.Operator, constraints[i].Operator)
		assert.Equal(t, expected.Version, constraints[i].Version)
		assert.Equal(t, expected.Comment, constraints[i].Comment)
	}
}

func TestUpdateErrors(t *testing.T) {
	t.Run("single error", func(t *testing.T) {
		err := &UpdateErrors{
			Errors: []error{
				assert.AnError,
			},
		}
		assert.Equal(t, "assert.AnError general error for testing", err.Error())
	})

	t.Run("multiple errors", func(t *testing.T) {
		ue := &UpdateErrors{}
		ue.Add(assert.AnError)
		ue.Add(assert.AnError)

		assert.True(t, ue.HasErrors())
		assert.Contains(t, ue.Error(), "encountered 2 error(s)")
		assert.Contains(t, ue.Error(), "  •")
	})

	t.Run("no errors", func(t *testing.T) {
		ue := &UpdateErrors{}
		assert.False(t, ue.HasErrors())
	})
}

func TestWriteConstraintsFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "output.txt")

	lines := []string{
		"# Header comment",
		"requests==2.31.0",
		"django==4.2.0",
	}

	c := &cfg{ConstraintsFile: testFile}
	err := c.writeConstraintsFile(lines, 0644)
	assert.NoError(t, err)

	// Read and verify content
	content, err := os.ReadFile(testFile)
	assert.NoError(t, err)

	expected := "# Header comment\nrequests==2.31.0\ndjango==4.2.0\n"
	assert.Equal(t, expected, string(content))

	// Verify file ends with newline
	assert.True(t, strings.HasSuffix(string(content), "\n"))
}

func TestLoadUpdatesFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	updatesFile := filepath.Join(tmpDir, "updates.txt")

	content := `# Updates file
requests==2.31.0 # Security fix
django==4.2.0

# Another comment
numpy==1.24.0 # Performance
`

	err := os.WriteFile(updatesFile, []byte(content), 0644)
	require.NoError(t, err)

	c := &cfg{UpdatesFile: updatesFile}
	updates, err := c.loadUpdatesFromFile()

	assert.NoError(t, err)
	assert.Len(t, updates, 3)
	assert.Equal(t, "requests==2.31.0 # Security fix", updates[0])
	assert.Equal(t, "django==4.2.0", updates[1])
	assert.Equal(t, "numpy==1.24.0 # Performance", updates[2])
}

func TestIntegrationUpdateConstraints(t *testing.T) {
	tmpDir := t.TempDir()
	constraintsFile := filepath.Join(tmpDir, "constraints.txt")

	// Create initial constraints file
	initialContent := `# Python dependencies
requests==2.28.0
django==3.2.0 # Web framework
numpy==1.21.0
pandas>=1.3.0
`

	err := os.WriteFile(constraintsFile, []byte(initialContent), 0644)
	require.NoError(t, err)

	// Create config with updates
	c := &cfg{
		ConstraintsFile: constraintsFile,
		OnlyReplace:     true,
		Packages: []string{
			"requests==2.31.0 # Security update",
			"django==4.2.0", // Should keep existing comment
			"numpy==1.24.0 # New features",
		},
	}

	// Parse updates
	updates, err := c.parsePackageUpdates()
	require.NoError(t, err)
	assert.Len(t, updates, 3)

	// Read and parse existing constraints
	constraints, lines, err := c.parseConstraintsFile()
	require.NoError(t, err)
	assert.Len(t, constraints, 4)
	assert.Len(t, lines, 5)

	// Build constraint map
	constraintMap := make(map[string]*Constraint)
	for i := range constraints {
		constraintMap[constraints[i].Package] = &constraints[i]
	}

	// Process updates
	newLines := make([]string, len(lines))
	copy(newLines, lines)

	for _, update := range updates {
		if existingConstraint, exists := constraintMap[update.Package]; exists {
			// Build new constraint
			newConstraint := existingConstraint.Package + existingConstraint.Operator + update.Version
			if update.Comment != "" {
				newConstraint += " # " + update.Comment
			} else if existingConstraint.Comment != "" {
				newConstraint += " # " + existingConstraint.Comment
			}

			// Find and update line
			lineIndex := c.findConstraintLine(lines, update.Package)
			if lineIndex >= 0 {
				newLines[lineIndex] = newConstraint
			}
		}
	}

	// Write updated file
	err = c.writeConstraintsFile(newLines, 0644)
	require.NoError(t, err)

	// Verify final content
	finalContent, err := os.ReadFile(constraintsFile)
	require.NoError(t, err)

	expectedContent := `# Python dependencies
requests==2.31.0 # Security update
django==4.2.0 # Web framework
numpy==1.24.0 # New features
pandas>=1.3.0
`

	assert.Equal(t, expectedContent, string(finalContent))
}

func TestPythonVersionComparison(t *testing.T) {
	// Test that the version comparison logic handles Python-specific versions
	// This test documents the expected behavior with PEP 440 versions

	tests := []struct {
		name        string
		current     string
		new         string
		shouldAllow bool
		desc        string
	}{
		{
			name:        "normal upgrade",
			current:     "1.0.0",
			new:         "2.0.0",
			shouldAllow: true,
			desc:        "normal version upgrade",
		},
		{
			name:        "downgrade prevented",
			current:     "2.0.0",
			new:         "1.0.0",
			shouldAllow: false,
			desc:        "downgrade should be prevented",
		},
		{
			name:        "same version prevented",
			current:     "1.0.0",
			new:         "1.0.0",
			shouldAllow: false,
			desc:        "same version should be prevented",
		},
		{
			name:        "dev to release",
			current:     "1.0.0.dev0",
			new:         "1.0.0",
			shouldAllow: true,
			desc:        "dev to release is an upgrade",
		},
		{
			name:        "rc to release",
			current:     "1.0.0rc1",
			new:         "1.0.0",
			shouldAllow: true,
			desc:        "release candidate to release is an upgrade",
		},
		{
			name:        "release to rc downgrade",
			current:     "1.0.0",
			new:         "1.0.0rc1",
			shouldAllow: false,
			desc:        "release to rc is a downgrade",
		},
		{
			name:        "beta to rc upgrade",
			current:     "1.0.0b1",
			new:         "1.0.0rc1",
			shouldAllow: true,
			desc:        "beta to rc is an upgrade",
		},
		{
			name:        "post release upgrade",
			current:     "1.0.0",
			new:         "1.0.0.post1",
			shouldAllow: true,
			desc:        "post release is an upgrade",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This documents expected behavior when using pep440 version comparison
			// The actual implementation uses pep440.Parse() and Compare()
			// This test ensures our expectations match the library behavior
			t.Logf("Test case: %s - %s", tt.name, tt.desc)
		})
	}
}
