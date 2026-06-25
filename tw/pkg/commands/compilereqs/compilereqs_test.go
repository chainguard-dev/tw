package compilereqs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommand(t *testing.T) {
	cmd := Command()

	assert.NotNil(t, cmd)
	assert.Equal(t, "compilereqs", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Check that required flags are defined
	packageFlag := cmd.Flags().Lookup("package")
	assert.NotNil(t, packageFlag)
	assert.Equal(t, "Main package to generate requirements around (required)", packageFlag.Usage)
	assert.Equal(t, "p", packageFlag.Shorthand)

	versionFlag := cmd.Flags().Lookup("version")
	assert.NotNil(t, versionFlag)
	assert.Equal(t, "Version of the main package (required)", versionFlag.Usage)
	assert.Equal(t, "v", versionFlag.Shorthand)

	dependenciesFlag := cmd.Flags().Lookup("dependencies")
	assert.NotNil(t, dependenciesFlag)
	assert.Equal(t, "Additional dependencies to add (space-separated list)", dependenciesFlag.Usage)
	assert.Equal(t, "d", dependenciesFlag.Shorthand)

	pythonFlag := cmd.Flags().Lookup("python")
	assert.NotNil(t, pythonFlag)
	assert.Equal(t, "Python version or path (overrides UV_PYTHON)", pythonFlag.Usage)
	assert.Equal(t, "P", pythonFlag.Shorthand)

	outputFlag := cmd.Flags().Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "Output file path or directory for the locked requirements", outputFlag.Usage)
	assert.Equal(t, "o", outputFlag.Shorthand)
	assert.Equal(t, "requirements.locked", outputFlag.DefValue)

	indexFlag := cmd.Flags().Lookup("index")
	assert.NotNil(t, indexFlag)
	assert.Equal(t, "Python package index URL (overrides UV_DEFAULT_INDEX)", indexFlag.Usage)
	assert.Equal(t, "i", indexFlag.Shorthand)
	assert.Equal(t, "https://libraries.cgr.dev/python/simple", indexFlag.DefValue)
}

func TestCommandMetadata(t *testing.T) {
	cmd := Command()

	// Verify the command has proper metadata
	assert.Equal(t, "compilereqs", cmd.Use)
	assert.Contains(t, cmd.Short, "locked requirements file")
	assert.Contains(t, cmd.Long, "uv")
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE)

	// Verify examples are provided
	assert.Contains(t, cmd.Long, "Examples:")
	assert.Contains(t, cmd.Long, "tw compilereqs")
}

func TestValidateFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "missing package flag",
			args:        []string{"--version", "1.0.0"},
			expectError: true,
			errorMsg:    "required flag(s) \"package\" not set",
		},
		{
			name:        "missing version flag",
			args:        []string{"--package", "requests"},
			expectError: true,
			errorMsg:    "required flag(s) \"version\" not set",
		},
		{
			name:        "missing both required flags",
			args:        []string{},
			expectError: true,
			errorMsg:    "required flag(s) \"package\", \"version\" not set",
		},
		{
			name:        "both required flags present",
			args:        []string{"--package", "requests", "--version", "2.31.0"},
			expectError: false,
		},
		{
			name:        "short flags work",
			args:        []string{"-p", "django", "-v", "4.2.0"},
			expectError: false,
		},
		{
			name:        "with dependencies flag",
			args:        []string{"-p", "flask", "-v", "2.3.0", "-d", "celery redis"},
			expectError: false,
		},
		{
			name:        "with python flag",
			args:        []string{"-p", "numpy", "-v", "1.24.0", "-P", "3.13"},
			expectError: false,
		},
		{
			name:        "with output flag",
			args:        []string{"-p", "pandas", "-v", "2.0.0", "-o", "requirements.txt"},
			expectError: false,
		},
		{
			name:        "with index flag",
			args:        []string{"-p", "scipy", "-v", "1.11.0", "-i", "https://pypi.org/simple"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := Command()
			cmd.SetArgs(tt.args)
			err := cmd.ParseFlags(tt.args)
			require.NoError(t, err)

			// Validate required flags
			err = cmd.ValidateRequiredFlags()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		content     string
		expectError bool
	}{
		{
			name:        "copy simple file",
			content:     "hello world\n",
			expectError: false,
		},
		{
			name:        "copy empty file",
			content:     "",
			expectError: false,
		},
		{
			name:        "copy multiline file",
			content:     "line1\nline2\nline3\n",
			expectError: false,
		},
		{
			name:        "copy file with special characters",
			content:     "Special: !@#$%^&*()\nUnicode: 你好世界\n",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcPath := filepath.Join(tmpDir, "source.txt")
			destPath := filepath.Join(tmpDir, "dest.txt")

			// Create source file
			err := os.WriteFile(srcPath, []byte(tt.content), 0o644)
			require.NoError(t, err)

			// Copy file
			err = copyFile(srcPath, destPath)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify content matches
				srcContent, err := os.ReadFile(srcPath)
				require.NoError(t, err)
				destContent, err := os.ReadFile(destPath)
				require.NoError(t, err)
				assert.Equal(t, srcContent, destContent)

				// Verify permissions
				info, err := os.Stat(destPath)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
			}

			// Cleanup
			os.Remove(srcPath)
			os.Remove(destPath)
		})
	}
}

func TestCopyFileErrors(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("source file does not exist", func(t *testing.T) {
		srcPath := filepath.Join(tmpDir, "nonexistent.txt")
		destPath := filepath.Join(tmpDir, "dest.txt")

		err := copyFile(srcPath, destPath)
		assert.Error(t, err)
	})

	t.Run("destination directory does not exist", func(t *testing.T) {
		srcPath := filepath.Join(tmpDir, "source.txt")
		destPath := filepath.Join(tmpDir, "nonexistent", "dest.txt")

		// Create source file
		err := os.WriteFile(srcPath, []byte("test"), 0o644)
		require.NoError(t, err)

		err = copyFile(srcPath, destPath)
		assert.Error(t, err)
	})
}

func TestCfgStructDefaults(t *testing.T) {
	cmd := Command()

	// Parse with just required flags
	cmd.SetArgs([]string{"-p", "test", "-v", "1.0.0"})
	err := cmd.ParseFlags([]string{"-p", "test", "-v", "1.0.0"})
	require.NoError(t, err)

	// Check default values
	outputFlag := cmd.Flags().Lookup("output")
	assert.Equal(t, "requirements.locked", outputFlag.DefValue)

	indexFlag := cmd.Flags().Lookup("index")
	assert.Equal(t, "https://libraries.cgr.dev/python/simple", indexFlag.DefValue)
}

func TestFlagShorthands(t *testing.T) {
	cmd := Command()

	tests := []struct {
		flagName  string
		shorthand string
	}{
		{"package", "p"},
		{"version", "v"},
		{"dependencies", "d"},
		{"python", "P"},
		{"output", "o"},
		{"index", "i"},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, flag)
			assert.Equal(t, tt.shorthand, flag.Shorthand)
		})
	}
}

func TestCommandExamples(t *testing.T) {
	cmd := Command()

	// Verify that the command includes helpful examples
	examples := []string{
		"tw compilereqs -p requests -v 2.31.0",
		"tw compilereqs -p django -v 4.2.0 -d \"celery redis\"",
		"tw compilereqs -p flask -v 2.3.0 --python 3.13",
		"tw compilereqs -p numpy -v 1.24.0 -o requirements.txt",
		"tw compilereqs -p requests -v 2.31.0 -i https://libraries.cgr.dev/python/simple",
	}

	for _, example := range examples {
		assert.Contains(t, cmd.Long, example, "Command should include example: %s", example)
	}
}
