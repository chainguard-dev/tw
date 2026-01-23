package shelldeps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"mvdan.cc/sh/v3/syntax"
)

type checkPackageCfg struct {
	parent     *cfg
	searchPath string // PATH-like string for looking up commands (defaults to /usr/bin:/bin)
	strict     bool   // Exit non-zero if issues found
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
  - Checks if dependencies are available in the search path
  - Checks runtime dependencies (using apk info -R) to detect GNU/busybox compatibility issues
  - Detects GNU-specific flags that don't work with busybox
  - Exits with non-zero status if any issues are found

The --path flag specifies where to look for binaries (defaults to /usr/bin:/bin).

Example usage:
  # Check an installed package
  tw shell-deps check-package vim

  # Check with custom search path
  tw shell-deps check-package --path=/usr/bin:/bin:/usr/local/bin git

  # Check with JSON output
  tw shell-deps check-package --json nginx`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkPkgCfg.Run(cmd.Context(), cmd, args[0])
		},
	}

	cmd.Flags().StringVar(&checkPkgCfg.searchPath, "path", "/usr/bin:/bin",
		"PATH-like colon-separated directories to search for commands")
	cmd.Flags().BoolVar(&checkPkgCfg.strict, "strict", true,
		"exit with non-zero status if any issues are found")

	return cmd
}

func (c *checkPackageCfg) Run(ctx context.Context, cmd *cobra.Command, packageName string) error {
	// Get list of installed files from the package
	installedFiles, err := c.getInstalledFiles(packageName)
	if err != nil {
		return fmt.Errorf("failed to get installed files for package %s: %w", packageName, err)
	}

	// Only print package name in text mode, not JSON mode
	if !c.parent.jsonOut {
		fmt.Fprintf(cmd.OutOrStdout(), "Package: %s\n", packageName)
	}

	// Get runtime dependencies for the package
	runtimeDeps, err := c.getRuntimeDeps(packageName)
	if err != nil {
		// Non-fatal - we can still check scripts without runtime dep info
		if c.parent.verbose {
			fmt.Fprintf(cmd.OutOrStdout(), "Warning: could not determine runtime dependencies: %v\n", err)
		}
		runtimeDeps = runtimeDepsInfo{}
	}

	// Filter for shell scripts
	scripts, err := c.findShellScripts(installedFiles)
	if err != nil {
		return fmt.Errorf("failed to find shell scripts: %w", err)
	}

	if len(scripts) == 0 {
		if c.parent.jsonOut {
			// Empty JSON array for no scripts
			fmt.Fprintln(cmd.OutOrStdout(), "[]")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "No shell scripts found in installed files.")
		}
		return nil
	}

	// Check each script
	var results []packageCheckResult
	hasIssues := false

	for _, script := range scripts {
		result := c.checkScriptWithDeps(ctx, script, runtimeDeps)
		results = append(results, result)

		if result.MissingCoreutils || len(result.GNUIncompatible) > 0 || len(result.Missing) > 0 || result.Error != "" {
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
	// Get dependencies from apk
	cmd := exec.Command("apk", "info", "-R", packageName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return runtimeDepsInfo{}, fmt.Errorf("could not get deps from apk: %w (output: %s)", err, string(output))
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
	Missing          []string            `json:"missing,omitempty"`
	GNUIncompatible  []gnuIncompatResult `json:"gnu_incompatible,omitempty"`
	MissingCoreutils bool                `json:"missing_coreutils,omitempty"`
	Error            string              `json:"error,omitempty"`
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

	// Check for missing dependencies in search path
	if c.searchPath != "" {
		result.Missing = findMissingInPath(deps, c.searchPath)
	}

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

	// Text output - show all scripts like 'show' command does
	hasIssues := false
	for _, result := range results {
		fmt.Fprintf(w, "%s:\n", result.File)

		if result.Error != "" {
			fmt.Fprintf(w, "  error: %s\n", result.Error)
			hasIssues = true
			continue
		}

		// Show deps
		if len(result.Deps) > 0 {
			fmt.Fprintf(w, "  deps: %s\n", strings.Join(result.Deps, " "))
		} else {
			fmt.Fprintf(w, "  deps: \n")
		}

		// Show missing dependencies if any
		if len(result.Missing) > 0 {
			fmt.Fprintf(w, "  missing: %s\n", strings.Join(result.Missing, " "))
			hasIssues = true
		}

		// Show GNU incompatibilities if any
		if len(result.GNUIncompatible) > 0 {
			fmt.Fprintf(w, "  gnu-incompatible (busybox cannot handle these):\n")
			for _, inc := range result.GNUIncompatible {
				fmt.Fprintf(w, "    - line %d: %s %s\n", inc.Line, inc.Command, inc.Flag)
				fmt.Fprintf(w, "      %s\n", inc.Description)
			}
			hasIssues = true
		}

		if result.MissingCoreutils {
			fmt.Fprintf(w, "  ⚠ MISSING RUNTIME DEPENDENCY: coreutils\n")
			fmt.Fprintf(w, "    Package declares 'busybox' but scripts use GNU-specific flags.\n")
			fmt.Fprintf(w, "    Add 'coreutils' to dependencies.runtime in the package YAML.\n")
			hasIssues = true
		}
	}

	// Summary footer
	fmt.Fprintf(w, "\n")
	if hasIssues {
		fmt.Fprintf(w, "✗ Issues found in package\n")
	} else {
		fmt.Fprintf(w, "✓ No issues found\n")
	}

	return nil
}

