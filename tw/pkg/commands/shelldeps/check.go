package shelldeps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"mvdan.cc/sh/v3/syntax"
)

type checkCfg struct {
	parent     *cfg
	searchPath string // PATH-like string for looking up commands
	strict     bool   // Exit non-zero if issues found
}

// checkResult contains the results for a single script
type checkResult struct {
	File            string              `json:"file"`
	Shell           string              `json:"shell,omitempty"`
	Deps            []string            `json:"deps"`
	Missing         []string            `json:"missing,omitempty"`
	GNUIncompatible []gnuIncompatResult `json:"gnu_incompatible,omitempty"`
	Error           string              `json:"error,omitempty"`
}

type gnuIncompatResult struct {
	Command     string `json:"command"`
	Flag        string `json:"flag"`
	Line        int    `json:"line"`
	Description string `json:"description"`
	Fix         string `json:"fix"`
}

func (c *cfg) checkCommand() *cobra.Command {
	checkCfg := &checkCfg{
		parent: c,
	}
	cmd := &cobra.Command{
		Use:   "check [flags] file [file...]",
		Short: "Check shell scripts for missing dependencies and GNU compatibility",
		Long: `Analyze shell scripts and check if their dependencies are available
in the specified PATH, and detect GNU coreutils incompatibilities.

This command:
  - Extracts external command dependencies from shell scripts
  - Checks if those commands exist in the specified --path
  - Detects GNU-specific flags that don't work with busybox
  - Automatically determines if a command is provided by busybox or coreutils

The --path flag accepts a PATH-like colon-separated list of directories
(e.g., "/usr/bin:/usr/local/bin"). Commands are checked for existence
in these directories.

GNU compatibility checking is automatic: if a script uses 'chmod --reference'
and /usr/bin/chmod is a symlink to busybox, it will report an error.
If /usr/bin/chmod is provided by coreutils, no error is reported.

Example usage:
  # Check specific files against system PATH
  tw shell-deps check --path=/usr/bin:/usr/local/bin script.sh

  # Check with strict mode (exit 1 if issues found)
  tw shell-deps check --path=/usr/bin --strict entrypoint.sh run.sh

  # Check files, auto-detect GNU issues based on actual binaries
  tw shell-deps check --path=/usr/bin /opt/scripts/*.sh`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkCfg.Run(cmd.Context(), cmd, args)
		},
	}

	cmd.Flags().StringVar(&checkCfg.searchPath, "path", "/usr/bin:/usr/local/bin",
		"PATH-like colon-separated directories to search for commands")
	cmd.Flags().BoolVar(&checkCfg.strict, "strict", true,
		"exit with non-zero status if any issues are found")

	return cmd
}

func (c *checkCfg) Run(ctx context.Context, cmd *cobra.Command, args []string) error {
	// Validate that all provided files exist
	var files []string
	for _, arg := range args {
		// Expand globs
		matches, err := filepath.Glob(arg)
		if err != nil {
			return fmt.Errorf("invalid pattern %s: %w", arg, err)
		}
		if len(matches) == 0 {
			// Check if it's a literal file that doesn't exist
			if _, err := os.Stat(arg); err != nil {
				return fmt.Errorf("file not found: %s", arg)
			}
			files = append(files, arg)
		} else {
			files = append(files, matches...)
		}
	}

	if len(files) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No files to check.")
		return nil
	}

	// Process each file
	var results []checkResult
	hasIssues := false

	for _, file := range files {
		result := c.processScript(ctx, file)
		results = append(results, result)

		if len(result.Missing) > 0 || len(result.GNUIncompatible) > 0 || result.Error != "" {
			hasIssues = true
		}
	}

	// Output results
	if err := c.outputResults(cmd.OutOrStdout(), results); err != nil {
		return err
	}

	// Exit with error if strict mode and issues found
	if c.strict && hasIssues {
		return fmt.Errorf("shell dependency issues found")
	}

	return nil
}

func (c *checkCfg) processScript(ctx context.Context, file string) checkResult {
	result := checkResult{File: file}

	f, err := os.Open(file)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer f.Close()

	// Extract shell from shebang
	shell, err := extractShebang(f)
	if err != nil {
		result.Error = fmt.Sprintf("failed to extract shebang: %v", err)
		return result
	}
	result.Shell = shell

	// Reset for parsing
	if _, err := f.Seek(0, 0); err != nil {
		result.Error = fmt.Sprintf("failed to seek: %v", err)
		return result
	}

	// Parse the script
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	parsedFile, err := parser.Parse(f, file)
	if err != nil {
		result.Error = fmt.Sprintf("parse error: %v", err)
		return result
	}

	// Reset for dep extraction (we need the reader again for extractDeps)
	if _, err := f.Seek(0, 0); err != nil {
		result.Error = fmt.Sprintf("failed to seek: %v", err)
		return result
	}

	// Extract dependencies
	deps, err := extractDeps(ctx, f, file)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Deps = deps

	// Find missing commands in PATH
	if c.searchPath != "" {
		result.Missing = c.findMissingInPath(deps)
	}

	// Check GNU compatibility using AST (auto-detects busybox vs coreutils)
	incompatibilities := CheckGNUCompatWithPath(parsedFile, file, c.searchPath)
	for _, inc := range incompatibilities {
		result.GNUIncompatible = append(result.GNUIncompatible, gnuIncompatResult{
			Command:     inc.Command,
			Flag:        inc.Flag,
			Line:        inc.Line,
			Description: inc.Description,
			Fix:         inc.Fix,
		})
	}

	return result
}

// findMissingInPath checks which commands are not found in the search PATH
func (c *checkCfg) findMissingInPath(deps []string) []string {
	var missing []string

	dirs := filepath.SplitList(c.searchPath)

	for _, dep := range deps {
		// Skip absolute paths - check them directly
		if strings.HasPrefix(dep, "/") {
			if _, err := os.Stat(dep); err != nil {
				missing = append(missing, dep)
			}
			continue
		}

		// Search in PATH directories
		found := false
		for _, dir := range dirs {
			cmdPath := filepath.Join(dir, dep)
			if _, err := os.Stat(cmdPath); err == nil {
				found = true
				break
			}
		}

		if !found {
			missing = append(missing, dep)
		}
	}

	return missing
}

func (c *checkCfg) outputResults(w io.Writer, results []checkResult) error {
	if c.parent.jsonOut {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	// Text output
	var scriptsWithIssues []checkResult
	totalDeps := 0
	totalMissing := 0
	totalGNUIncompat := 0

	for _, result := range results {
		if len(result.Missing) > 0 || len(result.GNUIncompatible) > 0 || result.Error != "" {
			scriptsWithIssues = append(scriptsWithIssues, result)
		}
		totalDeps += len(result.Deps)
		totalMissing += len(result.Missing)
		totalGNUIncompat += len(result.GNUIncompatible)
	}

	// Summary header with more context
	fmt.Fprintf(w, "Dependency Check Results\n")
	fmt.Fprintf(w, "========================\n")
	fmt.Fprintf(w, "Analyzed: %d shell script(s)\n", len(results))
	if c.searchPath != "" {
		fmt.Fprintf(w, "Checked against PATH: %s\n", c.searchPath)
	}
	fmt.Fprintf(w, "Mode: %s\n", func() string {
		if c.strict {
			return "strict (will fail on issues)"
		}
		return "report-only"
	}())
	fmt.Fprintf(w, "\n")

	// Always show details for all scripts
	for _, result := range results {
		fmt.Fprintf(w, "%s:\n", result.File)

		if result.Error != "" {
			fmt.Fprintf(w, "  error: %s\n", result.Error)
			fmt.Fprintln(w)
			continue
		}

		if result.Shell != "" {
			fmt.Fprintf(w, "  shell: %s\n", result.Shell)
		}

		// Show all dependencies found
		if len(result.Deps) > 0 {
			fmt.Fprintf(w, "  dependencies found: %d\n", len(result.Deps))

			// Categorize dependencies for better visibility
			dirs := filepath.SplitList(c.searchPath)
			var available []string
			var missing []string
			var gnuRequired []string

			for _, dep := range result.Deps {
				found := false
				// Check if command exists in PATH
				if strings.HasPrefix(dep, "/") {
					// Absolute path
					if _, err := os.Stat(dep); err == nil {
						found = true
						available = append(available, dep)
					} else {
						missing = append(missing, dep)
					}
				} else {
					// Search in PATH
					for _, dir := range dirs {
						cmdPath := filepath.Join(dir, dep)
						if _, err := os.Stat(cmdPath); err == nil {
							found = true
							// Check if it requires GNU
							for _, gnu := range result.GNUIncompatible {
								if gnu.Command == dep {
									gnuRequired = append(gnuRequired, fmt.Sprintf("%s [gnu]", dep))
									break
								}
							}
							if !contains(gnuRequired, fmt.Sprintf("%s [gnu]", dep)) {
								available = append(available, dep)
							}
							break
						}
					}
					if !found {
						missing = append(missing, dep)
					}
				}
			}

			// Show categorized dependencies
			if len(available) > 0 {
				sort.Strings(available)
				fmt.Fprintf(w, "    ✓ available (%d): %s\n", len(available), strings.Join(available, " "))
			}
			if len(gnuRequired) > 0 {
				sort.Strings(gnuRequired)
				fmt.Fprintf(w, "    ⚠ gnu-required (%d): %s\n", len(gnuRequired), strings.Join(gnuRequired, " "))
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				fmt.Fprintf(w, "    ✗ missing (%d): %s\n", len(missing), strings.Join(missing, " "))
			}
		} else {
			fmt.Fprintf(w, "  dependencies found: 0\n")
		}

		if len(result.GNUIncompatible) > 0 {
			fmt.Fprintf(w, "  gnu-incompatible issues:\n")
			for _, inc := range result.GNUIncompatible {
				fmt.Fprintf(w, "    - line %d: %s %s\n", inc.Line, inc.Command, inc.Flag)
				fmt.Fprintf(w, "      %s\n", inc.Description)
			}
		}

		fmt.Fprintln(w)
	}

	// Summary footer
	fmt.Fprintf(w, "---\n")
	fmt.Fprintf(w, "Summary:\n")
	fmt.Fprintf(w, "  Total scripts analyzed: %d\n", len(results))
	fmt.Fprintf(w, "  Total dependencies found: %d\n", totalDeps)
	fmt.Fprintf(w, "  Total missing commands: %d\n", totalMissing)
	fmt.Fprintf(w, "  Total GNU compatibility issues: %d\n", totalGNUIncompat)

	if len(scriptsWithIssues) == 0 {
		fmt.Fprintln(w, "\n✓ All dependencies are available and compatible")
	} else {
		fmt.Fprintf(w, "\n✗ Issues found in %d of %d file(s)\n", len(scriptsWithIssues), len(results))
	}

	return nil
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
