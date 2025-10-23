package shelldeps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
)

type scanCfg struct {
	parent      *cfg
	missingPath string
	matchRegex  string
	executable  bool
}

func (c *cfg) scanCommand() *cobra.Command {
	scanCfg := &scanCfg{parent: c}
	cmd := &cobra.Command{
		Use:   "scan [flags] search-dir",
		Short: "Scan a directory for shell scripts and show their dependencies",
		Long:  "Recursively scan a directory for shell scripts and analyze their external command dependencies.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return scanCfg.Run(cmd.Context(), cmd, args)
		},
	}

	cmd.Flags().StringVar(&scanCfg.missingPath, "missing", "", "path to directory containing available executables")
	cmd.Flags().StringVar(&scanCfg.matchRegex, "match", "", "regex pattern to match additional files as shell scripts")
	cmd.Flags().BoolVarP(&scanCfg.executable, "executable", "x", false, "only consider executable files as shell scripts")

	return cmd
}

// validShells lists the shell interpreters we recognize as shell scripts
var validShells = map[string]bool{
	"/bin/sh":   true,
	"/bin/dash": true,
	"/bin/bash": true,
	"sh":        true,
	"dash":      true,
	"bash":      true,
}

// isShellScript checks if a file is a shell script based on its shebang
func isShellScript(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	shell, err := extractShebang(f)
	if err != nil {
		return false, err
	}

	return validShells[shell], nil
}

func (s *scanCfg) Run(ctx context.Context, cmd *cobra.Command, args []string) error {
	searchDir := args[0]

	// Validate search directory
	info, err := os.Stat(searchDir)
	if err != nil {
		return fmt.Errorf("search directory error: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("search path %s is not a directory", searchDir)
	}

	// Validate missing path if provided
	if s.missingPath != "" {
		info, err := os.Stat(s.missingPath)
		if err != nil {
			return fmt.Errorf("--missing path error: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("--missing path %s is not a directory", s.missingPath)
		}
	}

	// Compile match regex if provided
	var matchPattern *regexp.Regexp
	if s.matchRegex != "" {
		matchPattern, err = regexp.Compile(s.matchRegex)
		if err != nil {
			return fmt.Errorf("invalid --match regex: %w", err)
		}
	}

	// Find all shell scripts
	var shellScripts []string
	err = filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if s.parent.verbose {
				clog.WarnContext(ctx, "failed to access path", "path", path, "error", err)
			}
			return nil // Continue walking
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip non-regular files and symlinks
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}

		// If --executable is set, check if file is executable
		isExecutable := info.Mode()&0111 != 0
		if s.executable && !isExecutable {
			return nil
		}

		// Check if basename matches the regex pattern
		matchedByRegex := matchPattern != nil && matchPattern.MatchString(filepath.Base(path))
		if matchedByRegex {
			shellScripts = append(shellScripts, path)
			if s.parent.verbose {
				clog.InfoContext(ctx, "matched file by regex", "path", path)
			}
			return nil
		}

		// Check if it's a shell script by shebang
		isShell, err := isShellScript(path)
		if err != nil {
			if s.parent.verbose {
				clog.WarnContext(ctx, "failed to check shebang", "path", path, "error", err)
			}
			return nil
		}

		if isShell {
			shellScripts = append(shellScripts, path)
			if s.parent.verbose {
				clog.InfoContext(ctx, "found shell script", "path", path)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	if len(shellScripts) == 0 {
		if s.parent.verbose {
			clog.WarnContext(ctx, "no shell scripts found in directory", "dir", searchDir)
		}
		return nil
	}

	// Process each shell script
	var results []scriptResult
	hadErrors := false

	for _, file := range shellScripts {
		result := scriptResult{File: file}

		f, err := os.Open(file)
		if err != nil {
			result.Error = err.Error()
			hadErrors = true
			results = append(results, result)
			if s.parent.verbose {
				clog.ErrorContext(ctx, "failed to open file", "file", file, "error", err)
			}
			continue
		}

		// Extract shell from shebang
		shell, err := extractShebang(f)
		if err != nil {
			f.Close()
			result.Error = fmt.Sprintf("failed to extract shebang: %v", err)
			hadErrors = true
			results = append(results, result)
			if s.parent.verbose {
				clog.ErrorContext(ctx, "failed to extract shebang", "file", file, "error", err)
			}
			continue
		}
		result.Shell = shell

		// Reset file pointer to beginning for extractDeps
		if _, err := f.Seek(0, 0); err != nil {
			f.Close()
			result.Error = fmt.Sprintf("failed to seek to beginning: %v", err)
			hadErrors = true
			results = append(results, result)
			if s.parent.verbose {
				clog.ErrorContext(ctx, "failed to seek", "file", file, "error", err)
			}
			continue
		}

		deps, err := extractDeps(ctx, f, file)
		f.Close()

		if err != nil {
			result.Error = err.Error()
			hadErrors = true
			results = append(results, result)
			if s.parent.verbose {
				clog.ErrorContext(ctx, "failed to parse file", "file", file, "error", err)
			}
			continue
		}

		result.Deps = deps

		// Find missing dependencies if requested
		if s.missingPath != "" {
			missing, err := findMissing(deps, s.missingPath)
			if err != nil {
				result.Error = err.Error()
				hadErrors = true
				results = append(results, result)
				if s.parent.verbose {
					clog.ErrorContext(ctx, "failed to find missing deps", "file", file, "error", err)
				}
				continue
			}
			result.Missing = missing
		}

		results = append(results, result)

		if s.parent.verbose {
			clog.InfoContext(ctx, "processed file", "file", file, "deps", len(deps))
		}
	}

	// Output results
	if err := outputResults(cmd.OutOrStdout(), results, s.parent.jsonOut); err != nil {
		return err
	}

	if hadErrors {
		return fmt.Errorf("errors occurred while processing files")
	}

	return nil
}
