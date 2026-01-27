package shelldeps

import (
	"bufio"
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

type cfg struct {
	verbose bool
	jsonOut bool
}

// Command returns the cobra command for shell-deps
func Command() *cobra.Command {
	cfg := &cfg{}
	cmd := &cobra.Command{
		Use:          "shell-deps",
		Short:        "Analyze shell script dependencies",
		Long:         "Process shell scripts (bash, dash, or sh) and list external programs (deps) that the shell script may invoke.",
		SilenceUsage: true,
	}

	cmd.PersistentFlags().BoolVarP(&cfg.verbose, "verbose", "v", false, "increase verbosity")
	cmd.PersistentFlags().BoolVar(&cfg.jsonOut, "json", false, "output in JSON format")

	cmd.AddCommand(
		cfg.showCommand(),
		cfg.scanCommand(),
		cfg.checkCommand(),
		cfg.checkPackageCommand(),
	)

	return cmd
}

// shellBuiltins contains all POSIX and common bash/dash built-in commands
var shellBuiltins = map[string]bool{
	// POSIX special builtins
	"break": true, ":": true, "continue": true, ".": true, "eval": true,
	"exec": true, "exit": true, "export": true, "readonly": true, "return": true,
	"set": true, "shift": true, "times": true, "trap": true, "unset": true,

	// POSIX regular builtins
	"alias": true, "bg": true, "cd": true, "command": true, "false": true,
	"fc": true, "fg": true, "getopts": true, "jobs": true, "kill": true,
	"newgrp": true, "pwd": true, "read": true, "true": true, "umask": true,
	"unalias": true, "wait": true, "hash": true, "type": true, "ulimit": true,
	"[": true, "test": true, "echo": true, "printf": true,

	// Control structures (not external commands)
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"while": true, "do": true, "done": true, "for": true, "in": true,
	"case": true, "esac": true, "until": true, "select": true,

	// Bash/dash additional builtins
	"source": true, "local": true, "declare": true, "typeset": true,
	"let": true, "enable": true, "builtin": true, "caller": true,
	"compgen": true, "complete": true, "compopt": true, "dirs": true,
	"disown": true, "help": true, "history": true, "logout": true,
	"mapfile": true, "popd": true, "pushd": true, "shopt": true,
	"suspend": true, "bind": true, "readarray": true, "function": true,
}

// scriptResult contains the analysis results for a single script
type scriptResult struct {
	File    string   `json:"file"`
	Deps    []string `json:"deps"`
	Shell   string   `json:"shell,omitempty"`
	Missing []string `json:"missing,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// extractDeps parses a shell script and returns the list of external dependencies
func extractDeps(ctx context.Context, r io.Reader, filename string) ([]string, error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(r, filename)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	deps := make(map[string]bool)
	funcs := make(map[string]bool)
	aliases := make(map[string]bool)
	wrapperFuncs := make(map[string]bool) // Functions that execute their arguments

	// First pass: collect function and alias definitions, identify wrapper functions
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.FuncDecl:
			funcs[n.Name.Value] = true
			// Check if this function executes its arguments (e.g., contains "$@" in command position)
			if executesArguments(n.Body) {
				wrapperFuncs[n.Name.Value] = true
			}
		case *syntax.CallExpr:
			// Check for alias definitions
			if len(n.Args) > 0 {
				word := n.Args[0]
				if len(word.Parts) > 0 {
					if lit, ok := word.Parts[0].(*syntax.Lit); ok {
						if lit.Value == "alias" && len(n.Args) > 1 {
							// Parse alias name from "name=value" format
							aliasArg := n.Args[1]
							aliasStr := wordToString(aliasArg)
							if idx := strings.Index(aliasStr, "="); idx > 0 {
								aliases[aliasStr[:idx]] = true
							}
						}
					}
				}
			}
		}
		return true
	})

	// Second pass: collect command invocations
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CallExpr:
			if len(n.Args) > 0 {
				cmdName := wordToString(n.Args[0])

				// If this is a wrapper function call, analyze its first argument as a potential command
				if wrapperFuncs[cmdName] && len(n.Args) > 1 {
					firstArg := wordToString(n.Args[1])
					// Apply same filtering as regular commands
					if !shellBuiltins[firstArg] && !funcs[firstArg] && !aliases[firstArg] && firstArg != "" {
						if strings.HasPrefix(firstArg, "/") {
							deps[firstArg] = true
						} else if !strings.Contains(firstArg, "$") && !strings.Contains(firstArg, "*") {
							deps[firstArg] = true
						}
					}
				}

				// Original logic: handle direct command invocations
				// Skip if it's a builtin, function, or alias
				if !shellBuiltins[cmdName] && !funcs[cmdName] && !aliases[cmdName] && cmdName != "" {
					// Handle absolute paths
					if strings.HasPrefix(cmdName, "/") {
						deps[cmdName] = true
					} else {
						// Only add if it looks like a command (no variable expansion, etc)
						if !strings.Contains(cmdName, "$") && !strings.Contains(cmdName, "*") {
							deps[cmdName] = true
						}
					}
				}
			}
		}
		return true
	})

	// Convert map to sorted slice
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}
	sort.Strings(result)

	return result, nil
}

// extractShebang reads the first line of a file and extracts the raw shebang content after #!.
// Returns empty string if the file doesn't have a shebang.
func extractShebang(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return "", nil
	}

	firstLine := strings.TrimSpace(scanner.Text())

	// Check if it starts with #!
	if !strings.HasPrefix(firstLine, "#!") {
		return "", nil
	}

	// Return everything after #! (trimmed of leading spaces)
	return strings.TrimLeft(firstLine[2:], " "), nil
}

// getShebangProgram extracts just the interpreter program from a shebang string.
// Handles /usr/bin/env wrappers and strips arguments (like -f in "/bin/sh -f").
func getShebangProgram(shebang string) string {
	if shebang == "" {
		return ""
	}

	// Handle /usr/bin/env wrapper - extract the program after env
	if strings.HasPrefix(shebang, "/usr/bin/env ") {
		parts := strings.Fields(shebang)
		if len(parts) >= 2 {
			return parts[1]
		}
		return ""
	}

	// For direct interpreter paths, strip any arguments
	// e.g., "/bin/sh -f" -> "/bin/sh"
	parts := strings.Fields(shebang)
	if len(parts) > 0 {
		return parts[0]
	}

	return ""
}

// executesArguments checks if a function body contains "$@" or similar patterns in command position
// This identifies "wrapper functions" that execute commands passed as arguments
func executesArguments(body *syntax.Stmt) bool {
	found := false
	syntax.Walk(body, func(node syntax.Node) bool {
		if found {
			return false // Stop walking once we've found it
		}
		switch n := node.(type) {
		case *syntax.CallExpr:
			// Check if the command being invoked is "$@" or contains "$@"
			if len(n.Args) > 0 {
				word := n.Args[0]
				if containsAllPositionalParams(word) {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

// containsAllPositionalParams checks if a word contains "$@" or "$*"
func containsAllPositionalParams(w *syntax.Word) bool {
	if w == nil {
		return false
	}
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.ParamExp:
			// Check for $@ or $*
			if p.Param.Value == "@" || p.Param.Value == "*" {
				return true
			}
		case *syntax.DblQuoted:
			// Check inside double quotes for "$@" or "$*"
			for _, qp := range p.Parts {
				if paramExp, ok := qp.(*syntax.ParamExp); ok {
					if paramExp.Param.Value == "@" || paramExp.Param.Value == "*" {
						return true
					}
				}
			}
		}
	}
	return false
}

// wordToString converts a syntax.Word to a string
func wordToString(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			// For double-quoted strings, we need to extract the content
			for _, qp := range p.Parts {
				if lit, ok := qp.(*syntax.Lit); ok {
					sb.WriteString(lit.Value)
				}
			}
		}
	}
	return sb.String()
}

// findMissing compares deps against available executables in searchPath
func findMissing(deps []string, searchPath string) ([]string, error) {
	available := make(map[string]bool)

	// Walk the search path and collect all executables
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Check if it's an executable file or symlink to a file
		if !info.IsDir() && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			if info.Mode()&0111 != 0 || info.Mode()&os.ModeSymlink != 0 {
				// For symlinks, check if target is a file
				if info.Mode()&os.ModeSymlink != 0 {
					target, err := os.Stat(path)
					if err == nil && target.IsDir() {
						return nil
					}
				}
				basename := filepath.Base(path)
				available[basename] = true
				// Also add the full path if it's an absolute path dep
				available[path] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan %s: %w", searchPath, err)
	}

	// Find deps that are not available
	var missing []string
	for _, dep := range deps {
		if !available[dep] {
			missing = append(missing, dep)
		}
	}

	return missing, nil
}

// outputResults prints results in text or JSON format
func outputResults(w io.Writer, results []scriptResult, jsonOut bool) error {
	if jsonOut {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	// Text output
	for _, result := range results {
		if result.Error != "" {
			fmt.Fprintf(w, "%s:\n  error: %s\n", result.File, result.Error)
			continue
		}

		fmt.Fprintf(w, "%s:\n", result.File)
		fmt.Fprintf(w, "  deps: %s\n", strings.Join(result.Deps, " "))
		if result.Shell != "" {
			fmt.Fprintf(w, "  shell: %s\n", result.Shell)
		}
		if result.Missing != nil {
			fmt.Fprintf(w, "  missing: %s\n", strings.Join(result.Missing, " "))
		}
	}

	return nil
}
