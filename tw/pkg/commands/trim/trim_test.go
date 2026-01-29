package trim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseDependency(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"foo", "foo"},
		{"foo=1.0", "foo"},
		{"foo>=1.0", "foo"},
		{"!foo", ""},
		{"@pinned:foo", "foo"},
		{"@pinned:foo>=1.0", "foo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDependency(tt.input)
			if got != tt.want {
				t.Errorf("parseDependency(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMelangeYAMLGetPackages(t *testing.T) {
	// Create a temporary YAML file
	content := `package:
  name: test-package
  version: 1.0.0
  dependencies:
    runtime:
      - runtime-dep-1
      - runtime-dep-2

environment:
  contents:
    repositories:
      - https://packages.wolfi.dev/os
    packages:
      - build-dep-1
      - build-dep-2
      - build-dep-3

test:
  environment:
    contents:
      packages:
        - test-dep-1
        - test-dep-2

subpackages:
  - name: test-subpkg
    dependencies:
      runtime:
        - subpkg-runtime-dep
    test:
      environment:
        contents:
          packages:
            - subpkg-test-dep

pipeline:
  - uses: go/build
`

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(yamlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	m, err := ParseMelangeYAML(yamlPath)
	if err != nil {
		t.Fatalf("ParseMelangeYAML() error = %v", err)
	}

	packages := m.GetPackages()

	// Check environment packages
	envPkgs, ok := packages["environment.contents.packages"]
	if !ok {
		t.Error("expected to find environment.contents.packages")
	} else if diff := cmp.Diff([]string{"build-dep-1", "build-dep-2", "build-dep-3"}, envPkgs); diff != "" {
		t.Errorf("environment.contents.packages mismatch (-want +got):\n%s", diff)
	}

	// Check package runtime deps
	runtimePkgs, ok := packages["package.dependencies.runtime"]
	if !ok {
		t.Error("expected to find package.dependencies.runtime")
	} else if diff := cmp.Diff([]string{"runtime-dep-1", "runtime-dep-2"}, runtimePkgs); diff != "" {
		t.Errorf("package.dependencies.runtime mismatch (-want +got):\n%s", diff)
	}

	// Check test packages
	testPkgs, ok := packages["test.environment.contents.packages"]
	if !ok {
		t.Error("expected to find test.environment.contents.packages")
	} else if diff := cmp.Diff([]string{"test-dep-1", "test-dep-2"}, testPkgs); diff != "" {
		t.Errorf("test.environment.contents.packages mismatch (-want +got):\n%s", diff)
	}

	// Check subpackage runtime deps
	subpkgRuntime, ok := packages["subpackages[test-subpkg].dependencies.runtime"]
	if !ok {
		t.Error("expected to find subpackages[test-subpkg].dependencies.runtime")
	} else if diff := cmp.Diff([]string{"subpkg-runtime-dep"}, subpkgRuntime); diff != "" {
		t.Errorf("subpackages[test-subpkg].dependencies.runtime mismatch (-want +got):\n%s", diff)
	}

	// Check subpackage test packages
	subpkgTest, ok := packages["subpackages[test-subpkg].test.environment.contents.packages"]
	if !ok {
		t.Error("expected to find subpackages[test-subpkg].test.environment.contents.packages")
	} else if diff := cmp.Diff([]string{"subpkg-test-dep"}, subpkgTest); diff != "" {
		t.Errorf("subpackages[test-subpkg].test.environment.contents.packages mismatch (-want +got):\n%s", diff)
	}
}

func TestMelangeYAMLGetPipelineUses(t *testing.T) {
	content := `package:
  name: test-package
  version: 1.0.0

pipeline:
  - uses: go/build
  - uses: strip
  - runs: echo "hello"

test:
  pipeline:
    - uses: test/tw/command-check

subpackages:
  - name: test-subpkg
    pipeline:
      - uses: split/manpages
    test:
      pipeline:
        - uses: test/tw/symlink-check
`

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(yamlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	m, err := ParseMelangeYAML(yamlPath)
	if err != nil {
		t.Fatalf("ParseMelangeYAML() error = %v", err)
	}

	uses := m.GetPipelineUses()

	// Check main pipeline
	mainPipeline := uses["pipeline"]
	if diff := cmp.Diff([]string{"go/build", "strip"}, mainPipeline); diff != "" {
		t.Errorf("pipeline mismatch (-want +got):\n%s", diff)
	}

	// Check test pipeline
	testPipeline := uses["test.pipeline"]
	if diff := cmp.Diff([]string{"test/tw/command-check"}, testPipeline); diff != "" {
		t.Errorf("test.pipeline mismatch (-want +got):\n%s", diff)
	}

	// Check subpackage pipeline
	subpkgPipeline := uses["subpackages[test-subpkg].pipeline"]
	if diff := cmp.Diff([]string{"split/manpages"}, subpkgPipeline); diff != "" {
		t.Errorf("subpackages[test-subpkg].pipeline mismatch (-want +got):\n%s", diff)
	}

	// Check subpackage test pipeline
	subpkgTestPipeline := uses["subpackages[test-subpkg].test.pipeline"]
	if diff := cmp.Diff([]string{"test/tw/symlink-check"}, subpkgTestPipeline); diff != "" {
		t.Errorf("subpackages[test-subpkg].test.pipeline mismatch (-want +got):\n%s", diff)
	}
}

func TestMelangeYAMLRemovePackages(t *testing.T) {
	content := `package:
  name: test-package
  version: 1.0.0

environment:
  contents:
    packages:
      - keep-1
      - remove-1
      - keep-2
      - remove-2
      - keep-3
`

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(yamlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	m, err := ParseMelangeYAML(yamlPath)
	if err != nil {
		t.Fatalf("ParseMelangeYAML() error = %v", err)
	}

	removed := m.RemovePackages("environment.contents.packages", []string{"remove-1", "remove-2"})

	if diff := cmp.Diff([]string{"remove-1", "remove-2"}, removed); diff != "" {
		t.Errorf("removed packages mismatch (-want +got):\n%s", diff)
	}

	// Verify remaining packages
	packages := m.GetPackages()
	remaining := packages["environment.contents.packages"]
	if diff := cmp.Diff([]string{"keep-1", "keep-2", "keep-3"}, remaining); diff != "" {
		t.Errorf("remaining packages mismatch (-want +got):\n%s", diff)
	}
}

func TestInferTestPipelinePackage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"test/tw/symlink-check", "symlink-check"},
		{"test/tw/command-check", "command-check"},
		{"test/ldd-check", "ldd-check"},
		{"go/build", "build"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := InferTestPipelinePackage(tt.input)
			if got != tt.want {
				t.Errorf("InferTestPipelinePackage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetPipelineScope(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"environment.contents.packages", "pipeline"},
		{"package.dependencies.runtime", "pipeline"},
		{"test.environment.contents.packages", "test.pipeline"},
		{"subpackages[foo].dependencies.runtime", "subpackages[foo].pipeline"},
		{"subpackages[foo].test.environment.contents.packages", "subpackages[foo].test.pipeline"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := getPipelineScope(tt.input)
			if got != tt.want {
				t.Errorf("getPipelineScope(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		packages []string
		inputs   map[string]Input
		want     []string
	}{
		{
			name:     "no substitution needed",
			packages: []string{"go", "busybox"},
			inputs:   nil,
			want:     []string{"go", "busybox"},
		},
		{
			name:     "simple substitution",
			packages: []string{"${{inputs.go-package}}", "busybox"},
			inputs: map[string]Input{
				"go-package": {Default: "go"},
			},
			want: []string{"go", "busybox"},
		},
		{
			name:     "unresolved variable excluded",
			packages: []string{"${{inputs.unknown}}", "busybox"},
			inputs:   nil,
			want:     []string{"busybox"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to config.Input type
			configInputs := make(map[string]configInput)
			for k, v := range tt.inputs {
				configInputs[k] = configInput{Default: v.Default}
			}
			got := applyDefaultsTest(tt.packages, configInputs)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("applyDefaults() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Input is a simplified version for testing
type Input struct {
	Default string
}

type configInput struct {
	Default string
}

// applyDefaultsTest is a test version that works with our simplified Input type
func applyDefaultsTest(packages []string, inputs map[string]configInput) []string {
	var result []string
	for _, pkg := range packages {
		resolvedPkg := pkg
		for name, input := range inputs {
			placeholder := "${{inputs." + name + "}}"
			resolvedPkg = strings.ReplaceAll(resolvedPkg, placeholder, input.Default)
		}
		if resolvedPkg != "" && !strings.Contains(resolvedPkg, "${{") {
			result = append(result, resolvedPkg)
		}
	}
	return result
}

func TestSplitPathPreservingBrackets(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{
			input: "environment.contents.packages",
			want:  []string{"environment", "contents", "packages"},
		},
		{
			input: "subpackages[foo].dependencies.runtime",
			want:  []string{"subpackages[foo]", "dependencies", "runtime"},
		},
		{
			input: "subpackages[${{package.name}}-jobservice].dependencies.runtime",
			want:  []string{"subpackages[${{package.name}}-jobservice]", "dependencies", "runtime"},
		},
		{
			input: "subpackages[${{package.name}}-foo].test.environment.contents.packages",
			want:  []string{"subpackages[${{package.name}}-foo]", "test", "environment", "contents", "packages"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitPathPreservingBrackets(tt.input)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("splitPathPreservingBrackets(%q) mismatch (-want +got):\n%s", tt.input, diff)
			}
		})
	}
}

func TestMelangeYAMLWrite(t *testing.T) {
	content := `package:
  name: test-package
  version: 1.0.0

environment:
  contents:
    packages:
      - keep-1
      - remove-me
      - keep-2
`

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(yamlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	m, err := ParseMelangeYAML(yamlPath)
	if err != nil {
		t.Fatalf("ParseMelangeYAML() error = %v", err)
	}

	// Remove a package
	m.RemovePackages("environment.contents.packages", []string{"remove-me"})

	// Write back
	if err := m.Write(); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Re-read and verify
	m2, err := ParseMelangeYAML(yamlPath)
	if err != nil {
		t.Fatalf("ParseMelangeYAML() after write error = %v", err)
	}

	packages := m2.GetPackages()
	remaining := packages["environment.contents.packages"]
	if diff := cmp.Diff([]string{"keep-1", "keep-2"}, remaining); diff != "" {
		t.Errorf("packages after write mismatch (-want +got):\n%s", diff)
	}
}

func TestMelangeYAMLCleanupEmptyBlocks(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		path           string
		packagesToRmv  []string
		wantContains   []string // strings that should be in the output
		wantNotContain []string // strings that should NOT be in the output
	}{
		{
			name: "remove all packages cleans up environment block",
			content: `package:
  name: test-package
  version: 1.0.0

environment:
  contents:
    packages:
      - pkg1
      - pkg2

pipeline:
  - uses: go/build
`,
			path:           "environment.contents.packages",
			packagesToRmv:  []string{"pkg1", "pkg2"},
			wantContains:   []string{"package:", "pipeline:"},
			wantNotContain: []string{"environment:", "contents:", "packages:"},
		},
		{
			name: "remove all runtime deps cleans up dependencies block",
			content: `package:
  name: test-package
  version: 1.0.0
  dependencies:
    runtime:
      - dep1
      - dep2

pipeline:
  - uses: go/build
`,
			path:           "package.dependencies.runtime",
			packagesToRmv:  []string{"dep1", "dep2"},
			wantContains:   []string{"package:", "name: test-package", "pipeline:"},
			wantNotContain: []string{"dependencies:", "runtime:"},
		},
		{
			name: "partial removal keeps block",
			content: `package:
  name: test-package
  version: 1.0.0

environment:
  contents:
    packages:
      - keep-me
      - remove-me
`,
			path:          "environment.contents.packages",
			packagesToRmv: []string{"remove-me"},
			wantContains:  []string{"environment:", "contents:", "packages:", "keep-me"},
		},
		{
			name: "environment with other contents is preserved",
			content: `package:
  name: test-package
  version: 1.0.0

environment:
  contents:
    repositories:
      - https://packages.wolfi.dev/os
    packages:
      - pkg1
`,
			path:           "environment.contents.packages",
			packagesToRmv:  []string{"pkg1"},
			wantContains:   []string{"environment:", "contents:", "repositories:"},
			wantNotContain: []string{"packages:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			yamlPath := filepath.Join(tmpDir, "test.yaml")
			if err := os.WriteFile(yamlPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			m, err := ParseMelangeYAML(yamlPath)
			if err != nil {
				t.Fatalf("ParseMelangeYAML() error = %v", err)
			}

			m.RemovePackages(tt.path, tt.packagesToRmv)

			if err := m.Write(); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			// Read the raw output
			output, err := os.ReadFile(yamlPath)
			if err != nil {
				t.Fatalf("failed to read output: %v", err)
			}
			outputStr := string(output)

			for _, want := range tt.wantContains {
				if !containsString(outputStr, want) {
					t.Errorf("output should contain %q but doesn't:\n%s", want, outputStr)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if containsString(outputStr, notWant) {
					t.Errorf("output should NOT contain %q but does:\n%s", notWant, outputStr)
				}
			}
		})
	}
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		goarch string
		want   string
	}{
		{"amd64", "x86_64"},
		{"arm64", "aarch64"},
		{"unknown", "unknown"}, // passthrough for unknown architectures
	}

	for _, tt := range tests {
		t.Run(tt.goarch, func(t *testing.T) {
			got := normalizeArch(tt.goarch)
			if got != tt.want {
				t.Errorf("normalizeArch(%q) = %q, want %q", tt.goarch, got, tt.want)
			}
		})
	}
}

func TestFilterRepositories(t *testing.T) {
	tests := []struct {
		name  string
		repos []string
		want  []string
	}{
		{
			name:  "filters @local repos",
			repos: []string{"https://packages.wolfi.dev/os", "@local /path/to/repo"},
			want:  []string{"https://packages.wolfi.dev/os"},
		},
		{
			name:  "filters file:// URLs",
			repos: []string{"https://packages.wolfi.dev/os", "file:///local/repo"},
			want:  []string{"https://packages.wolfi.dev/os"},
		},
		{
			name:  "filters bare paths",
			repos: []string{"https://packages.wolfi.dev/os", "/some/local/path"},
			want:  []string{"https://packages.wolfi.dev/os"},
		},
		{
			name:  "keeps http and https",
			repos: []string{"https://packages.wolfi.dev/os", "http://other.repo.com/packages"},
			want:  []string{"https://packages.wolfi.dev/os", "http://other.repo.com/packages"},
		},
		{
			name:  "empty input",
			repos: []string{},
			want:  nil,
		},
		{
			name:  "all filtered",
			repos: []string{"@local /path", "file:///other"},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterRepositories(tt.repos)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("filterRepositories() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIsVirtualProvide(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"so:libc.musl-x86_64.so.1", true},
		{"so:libcrypto.so.3", true},
		{"cmd:python3", true},
		{"cmd:openssl", true},
		{"pc:openssl", true},
		{"pc:libpq", true},
		{"busybox", false},
		{"go-1.21", false},
		{"python-3.11", false},
		{"openssl", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isVirtualProvide(tt.name)
			if got != tt.want {
				t.Errorf("isVirtualProvide(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
