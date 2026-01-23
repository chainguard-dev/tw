package shelldeps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

type checkPackageCfg struct {
	parent     *cfg
	searchPath string // PATH-like string for looking up commands (defaults to /usr/bin:/bin)
	strict     bool   // Exit non-zero if issues found
	packageDir string // Directory to search for package YAML files
}

// melangeConfig represents the structure of a melange YAML file (partial)
type melangeConfig struct {
	Package struct {
		Name         string `yaml:"name"`
		Dependencies struct {
			Runtime []string `yaml:"runtime"`
		} `yaml:"dependencies"`
	} `yaml:"package"`
	Subpackages []struct {
		Name         string `yaml:"name"`
		Dependencies struct {
			Runtime []string `yaml:"runtime"`
		} `yaml:"dependencies"`
		Pipeline []struct {
			Runs string            `yaml:"runs"`
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
		} `yaml:"pipeline"`
	} `yaml:"subpackages"`
	Pipeline []struct {
		Runs string            `yaml:"runs"`
		Uses string            `yaml:"uses"`
		With map[string]string `yaml:"with"`
	} `yaml:"pipeline"`
	Test struct {
		Pipeline []struct {
			Runs string            `yaml:"runs"`
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
		} `yaml:"pipeline"`
	} `yaml:"test"`
}

// runtimeDepsInfo contains analysis of a package's runtime dependencies
type runtimeDepsInfo struct {
	HasBusybox   bool
	HasCoreutils bool
	AllDeps      []string
}

func (c *cfg) checkPackageCommand() *cobra.Command {
	checkPkgCfg := &checkPackageCfg{
		parent: c,
	}
	cmd := &cobra.Command{
		Use:   "check-package <package-name>",
		Short: "Check an installed package's shell scripts for dependencies and GNU compatibility",
		Long: `Analyze shell scripts installed by a package and check for dependency issues.

This command:
  - Gets the list of files installed by the package (using apk info -L)
  - Identifies shell scripts among the installed files
  - Extracts dependencies from each shell script
  - Checks runtime dependencies to detect GNU/busybox compatibility issues
  - Detects GNU-specific flags that don't work with busybox

The --path flag specifies where to look for binaries (defaults to /usr/bin:/bin).
Use --strict to exit with non-zero status if any issues are found.

Example usage:
  # Check an installed package
  tw shell-deps check-package vim

  # Check with strict mode (exit 1 if issues found)
  tw shell-deps check-package --strict git

  # Check with JSON output
  tw shell-deps check-package --json nginx`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkPkgCfg.Run(cmd.Context(), cmd, args[0])
		},
	}

	cmd.Flags().StringVar(&checkPkgCfg.searchPath, "path", "/usr/bin:/bin",
		"PATH-like colon-separated directories to search for commands")
	cmd.Flags().BoolVar(&checkPkgCfg.strict, "strict", false,
		"exit with non-zero status if any issues are found")
	cmd.Flags().StringVar(&checkPkgCfg.packageDir, "package-dir", ".",
		"directory to search for package YAML files")

	return cmd
}

func (c *checkPackageCfg) Run(ctx context.Context, cmd *cobra.Command, packageName string) error {
	// Get list of installed files from the package
	installedFiles, err := c.getInstalledFiles(packageName)
	if err != nil {
		return fmt.Errorf("failed to get installed files for package %s: %w", packageName, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Package: %s\n", packageName)
	fmt.Fprintf(cmd.OutOrStdout(), "Found %d installed file(s)\n", len(installedFiles))

	// Get runtime dependencies for the package
	runtimeDeps, err := c.getRuntimeDeps(packageName)
	if err != nil {
		// Non-fatal - we can still check scripts without runtime dep info
		fmt.Fprintf(cmd.OutOrStdout(), "Warning: could not determine runtime dependencies: %v\n", err)
		runtimeDeps = runtimeDepsInfo{}
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Runtime dependencies: %v\n", runtimeDeps.AllDeps)
		if runtimeDeps.HasBusybox && !runtimeDeps.HasCoreutils {
			fmt.Fprintf(cmd.OutOrStdout(), "Note: Package has busybox but NOT coreutils - GNU-specific flags will fail\n")
		}
	}

	// Filter for shell scripts
	scripts, err := c.findShellScripts(installedFiles)
	if err != nil {
		return fmt.Errorf("failed to find shell scripts: %w", err)
	}

	if len(scripts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No shell scripts found in installed files.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Found %d shell script(s) to check\n\n", len(scripts))

	// Check each script
	var results []packageCheckResult
	hasIssues := false

	for _, script := range scripts {
		result := c.checkScriptWithDeps(ctx, script, runtimeDeps)
		results = append(results, result)

		if result.MissingCoreutils || len(result.GNUIncompatible) > 0 || result.Error != "" {
			hasIssues = true
		}
	}

	// Output results
	if err := c.outputPackageResults(cmd.OutOrStdout(), results, runtimeDeps); err != nil {
		return err
	}

	// Exit with error if strict mode and issues found
	if c.strict && hasIssues {
		return fmt.Errorf("shell dependency issues found in package %s", packageName)
	}

	return nil
}

// scriptSource represents a shell script extracted from the package
type scriptSource struct {
	Name    string // Descriptive name (e.g., "pipeline[0].runs" or file path)
	Content string // The script content
}

// getInstalledFiles returns the list of files installed by a package
func (c *checkPackageCfg) getInstalledFiles(packageName string) ([]string, error) {
	cmd := exec.Command("apk", "info", "-L", packageName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("apk info -L failed: %w (output: %s)", err, string(output))
	}

	lines := strings.Split(string(output), "\n")
	var files []string

	// Skip the first line which is "package-version contains:"
	for i, line := range lines {
		if i == 0 {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Prepend / if not already absolute path
		if !strings.HasPrefix(line, "/") {
			line = "/" + line
		}
		files = append(files, line)
	}

	return files, nil
}

// getRuntimeDeps returns runtime dependencies for a package
func (c *checkPackageCfg) getRuntimeDeps(packageName string) (runtimeDepsInfo, error) {
	// Try to get dependencies from apk
	cmd := exec.Command("apk", "info", "-R", packageName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fall back to trying to find melange YAML
		yamlPath, yamlErr := c.findPackageYAML(packageName)
		if yamlErr != nil {
			return runtimeDepsInfo{}, fmt.Errorf("could not get deps from apk or yaml: apk error: %w, yaml error: %v", err, yamlErr)
		}

		config, parseErr := c.parsePackageYAML(yamlPath)
		if parseErr != nil {
			return runtimeDepsInfo{}, fmt.Errorf("could not parse yaml: %w", parseErr)
		}

		return c.extractRuntimeDeps(config, packageName), nil
	}

	// Parse apk output - only use the first version's dependencies
	lines := strings.Split(string(output), "\n")
	var deps []string
	info := runtimeDepsInfo{}

	// Skip the first line which is "package-version depends on:"
	// Stop at the next empty line (which separates versions)
	inFirstBlock := false
	for i, line := range lines {
		if i == 0 {
			inFirstBlock = true
			continue
		}

		line = strings.TrimSpace(line)

		// Stop if we hit an empty line (end of first version's deps)
		if line == "" {
			break
		}

		// If we see "depends on:", it means we've hit another version - stop
		if strings.Contains(line, "depends on:") {
			break
		}

		if !inFirstBlock {
			continue
		}

		// Skip .so dependencies and other low-level deps
		if strings.HasPrefix(line, "so:") {
			continue
		}
		deps = append(deps, line)

		// Check for busybox and coreutils
		depLower := strings.ToLower(line)
		if depLower == "busybox" || strings.HasPrefix(depLower, "busybox-") {
			info.HasBusybox = true
		}
		if depLower == "coreutils" || strings.HasPrefix(depLower, "coreutils-") {
			info.HasCoreutils = true
		}
	}

	info.AllDeps = deps
	return info, nil
}

// findShellScripts filters a list of files and returns those that are shell scripts
func (c *checkPackageCfg) findShellScripts(files []string) ([]scriptSource, error) {
	var scripts []scriptSource

	for _, filePath := range files {
		// Check if file exists and is a regular file
		info, err := os.Stat(filePath)
		if err != nil {
			if c.parent.verbose {
				fmt.Fprintf(os.Stderr, "Skipping %s: %v\n", filePath, err)
			}
			continue
		}

		if info.IsDir() {
			continue
		}

		// Check for shell script shebang using existing function
		isShell, err := isShellScript(filePath)
		if err != nil {
			if c.parent.verbose {
				fmt.Fprintf(os.Stderr, "Could not check %s: %v\n", filePath, err)
			}
			continue
		}

		if !isShell {
			continue
		}

		// Read the script content
		content, err := os.ReadFile(filePath)
		if err != nil {
			if c.parent.verbose {
				fmt.Fprintf(os.Stderr, "Could not read %s: %v\n", filePath, err)
			}
			continue
		}

		scripts = append(scripts, scriptSource{
			Name:    filePath,
			Content: string(content),
		})
	}

	return scripts, nil
}

// packageCheckResult contains the results for checking a script against package dependencies
type packageCheckResult struct {
	File             string              `json:"file"`
	Deps             []string            `json:"deps,omitempty"`
	GNUIncompatible  []gnuIncompatResult `json:"gnu_incompatible,omitempty"`
	MissingCoreutils bool                `json:"missing_coreutils,omitempty"`
	Error            string              `json:"error,omitempty"`
}

// extractRuntimeDeps extracts runtime dependencies for the target package
func (c *checkPackageCfg) extractRuntimeDeps(config *melangeConfig, targetPackage string) runtimeDepsInfo {
	var deps []string

	// Check if we're looking for a subpackage
	for _, subpkg := range config.Subpackages {
		subName := expandPackageVars(subpkg.Name, config.Package.Name)
		if subName == targetPackage {
			deps = subpkg.Dependencies.Runtime
			break
		}
	}

	// If not a subpackage, use main package deps
	if len(deps) == 0 && config.Package.Name == targetPackage {
		deps = config.Package.Dependencies.Runtime
	}

	info := runtimeDepsInfo{
		AllDeps: deps,
	}

	// Check for busybox and coreutils
	for _, dep := range deps {
		depLower := strings.ToLower(dep)
		if depLower == "busybox" || strings.HasPrefix(depLower, "busybox-") {
			info.HasBusybox = true
		}
		if depLower == "coreutils" || strings.HasPrefix(depLower, "coreutils-") {
			info.HasCoreutils = true
		}
	}

	return info
}

// checkScriptWithDeps checks a script against the package's declared runtime dependencies
func (c *checkPackageCfg) checkScriptWithDeps(ctx context.Context, script scriptSource, runtimeDeps runtimeDepsInfo) packageCheckResult {
	result := packageCheckResult{File: script.Name}

	// Wrap script content in a shebang if needed for parsing
	content := script.Content
	if !strings.HasPrefix(strings.TrimSpace(content), "#!") {
		content = "#!/bin/sh\n" + content
	}

	// Parse the script
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	parsedFile, err := parser.Parse(strings.NewReader(content), script.Name)
	if err != nil {
		result.Error = fmt.Sprintf("parse error: %v", err)
		return result
	}

	// Extract dependencies
	deps, err := extractDeps(ctx, strings.NewReader(content), script.Name)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Deps = deps

	// Check GNU compatibility - only if busybox is declared without coreutils
	if runtimeDeps.HasBusybox && !runtimeDeps.HasCoreutils {
		// Check for GNU-specific flags (these won't work with busybox)
		incompatibilities := CheckGNUCompatibilityAST(parsedFile, script.Name)
		for _, inc := range incompatibilities {
			result.GNUIncompatible = append(result.GNUIncompatible, gnuIncompatResult{
				Command:     inc.Command,
				Flag:        inc.Flag,
				Line:        inc.Line,
				Description: inc.Description,
				Fix:         "Add 'coreutils' to runtime dependencies",
			})
		}
		if len(incompatibilities) > 0 {
			result.MissingCoreutils = true
		}
	}

	return result
}

// outputPackageResults outputs the package check results
func (c *checkPackageCfg) outputPackageResults(w io.Writer, results []packageCheckResult, runtimeDeps runtimeDepsInfo) error {
	if c.parent.jsonOut {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	// Text output
	var scriptsWithIssues []packageCheckResult

	for _, result := range results {
		if result.MissingCoreutils || len(result.GNUIncompatible) > 0 || result.Error != "" {
			scriptsWithIssues = append(scriptsWithIssues, result)
		}
	}

	// Summary header
	fmt.Fprintf(w, "Checked %d script(s)\n", len(results))

	if len(scriptsWithIssues) == 0 {
		fmt.Fprintln(w, "✓ No issues found")
		return nil
	}

	fmt.Fprintf(w, "\n")

	// Report issues
	for _, result := range scriptsWithIssues {
		fmt.Fprintf(w, "%s:\n", result.File)

		if result.Error != "" {
			fmt.Fprintf(w, "  error: %s\n", result.Error)
			continue
		}

		if len(result.GNUIncompatible) > 0 {
			fmt.Fprintf(w, "  gnu-incompatible (busybox cannot handle these):\n")
			for _, inc := range result.GNUIncompatible {
				fmt.Fprintf(w, "    - line %d: %s %s\n", inc.Line, inc.Command, inc.Flag)
				fmt.Fprintf(w, "      %s\n", inc.Description)
			}
		}

		if result.MissingCoreutils {
			fmt.Fprintf(w, "  ⚠ MISSING RUNTIME DEPENDENCY: coreutils\n")
			fmt.Fprintf(w, "    Package declares 'busybox' but scripts use GNU-specific flags.\n")
			fmt.Fprintf(w, "    Add 'coreutils' to dependencies.runtime in the package YAML.\n")
		}

		fmt.Fprintln(w)
	}

	// Summary footer
	fmt.Fprintf(w, "---\n")
	fmt.Fprintf(w, "Issues found in %d of %d script(s)\n", len(scriptsWithIssues), len(results))

	return nil
}

func (c *checkPackageCfg) findPackageYAML(packageName string) (string, error) {
	// Try different locations and naming patterns
	searchDirs := []string{
		c.packageDir,
		filepath.Join(c.packageDir, "enterprise-packages"),
		filepath.Join(c.packageDir, "os"),
	}

	patterns := []string{
		packageName + ".yaml",
		packageName + ".yml",
	}

	for _, dir := range searchDirs {
		for _, pattern := range patterns {
			path := filepath.Join(dir, pattern)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// Try to find by walking the directory (for subpackages)
	// For subpackages like "valkey-8.1-iamguarded-compat", we need to check
	// if the package name minus a prefix matches a subpackage pattern
	var found string
	err := filepath.Walk(c.packageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		// Check if this YAML file contains the package or subpackage
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Parse the YAML
		var config melangeConfig
		if err := yaml.Unmarshal(content, &config); err != nil {
			return nil
		}

		// Check main package name
		if config.Package.Name == packageName {
			found = path
			return filepath.SkipAll
		}

		// Check subpackage names - handle variable substitution
		for _, subpkg := range config.Subpackages {
			subName := expandPackageVars(subpkg.Name, config.Package.Name)
			if subName == packageName {
				found = path
				return filepath.SkipAll
			}
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return "", fmt.Errorf("error searching for package: %w", err)
	}

	if found != "" {
		return found, nil
	}

	return "", fmt.Errorf("package %s not found in %s", packageName, c.packageDir)
}

func (c *checkPackageCfg) parsePackageYAML(path string) (*melangeConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config melangeConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// expandPackageVars expands common melange variable substitutions in a string
func expandPackageVars(s string, packageName string) string {
	result := s
	result = strings.ReplaceAll(result, "${{package.name}}", packageName)
	return result
}

func (c *checkPackageCfg) extractScriptsFromConfig(config *melangeConfig, targetPackage string) []scriptSource {
	var scripts []scriptSource

	// Check if we're looking for a subpackage
	isSubpackage := false
	var targetSubpkg *struct {
		Name         string `yaml:"name"`
		Dependencies struct {
			Runtime []string `yaml:"runtime"`
		} `yaml:"dependencies"`
		Pipeline []struct {
			Runs string            `yaml:"runs"`
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
		} `yaml:"pipeline"`
	}

	for i := range config.Subpackages {
		subpkg := &config.Subpackages[i]
		subName := expandPackageVars(subpkg.Name, config.Package.Name)
		if subName == targetPackage {
			isSubpackage = true
			targetSubpkg = subpkg
			break
		}
	}

	if isSubpackage && targetSubpkg != nil {
		// Extract scripts from subpackage pipeline
		for i, step := range targetSubpkg.Pipeline {
			if step.Runs != "" {
				scripts = append(scripts, scriptSource{
					Name:    fmt.Sprintf("subpackage:%s/pipeline[%d].runs", targetPackage, i),
					Content: step.Runs,
				})
			}
			// Also check 'with' for script content (common in iamguarded pipelines)
			for key, val := range step.With {
				if looksLikeScript(val) {
					scripts = append(scripts, scriptSource{
						Name:    fmt.Sprintf("subpackage:%s/pipeline[%d].with.%s", targetPackage, i, key),
						Content: val,
					})
				}
			}
		}
	} else {
		// Extract scripts from main package pipeline
		for i, step := range config.Pipeline {
			if step.Runs != "" {
				scripts = append(scripts, scriptSource{
					Name:    fmt.Sprintf("pipeline[%d].runs", i),
					Content: step.Runs,
				})
			}
		}

		// Extract scripts from test pipeline
		for i, step := range config.Test.Pipeline {
			if step.Runs != "" {
				scripts = append(scripts, scriptSource{
					Name:    fmt.Sprintf("test/pipeline[%d].runs", i),
					Content: step.Runs,
				})
			}
		}
	}

	return scripts
}

// looksLikeScript checks if a string looks like shell script content
func looksLikeScript(s string) bool {
	// Check for common shell indicators
	indicators := []string{
		"#!/",
		"set -",
		"if [",
		"for ",
		"while ",
		"echo ",
		"mkdir ",
		"cp ",
		"mv ",
		"rm ",
		"chmod ",
		"chown ",
	}

	for _, indicator := range indicators {
		if strings.Contains(s, indicator) {
			return true
		}
	}

	// Check if it has multiple lines with shell-like commands
	lines := strings.Split(s, "\n")
	shellLineCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Check for common command patterns
		if strings.Contains(line, "=") || strings.Contains(line, "|") ||
			strings.Contains(line, "&&") || strings.Contains(line, "||") ||
			strings.HasPrefix(line, "export ") {
			shellLineCount++
		}
	}

	return shellLineCount >= 2
}

func outputCheckResultsJSON(w io.Writer, results []checkResult) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}
