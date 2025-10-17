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

type cfg struct {
	verbose bool
	jsonOut bool
}

// Command returns the cobra command for shell-deps
func Command() *cobra.Command {
	cfg := &cfg{}
	cmd := &cobra.Command{
		Use:   "shell-deps",
		Short: "Analyze shell script dependencies",
		Long:  "Process shell scripts (bash, dash, or sh) and list external programs (deps) that the shell script may invoke.",
	}

	cmd.PersistentFlags().BoolVarP(&cfg.verbose, "verbose", "v", false, "increase verbosity")
	cmd.PersistentFlags().BoolVar(&cfg.jsonOut, "json", false, "output in JSON format")

	cmd.AddCommand(
		cfg.showCommand(),
		cfg.scanCommand(),
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

	// First pass: collect function and alias definitions
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.FuncDecl:
			funcs[n.Name.Value] = true
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
		if result.Missing != nil {
			fmt.Fprintf(w, "  missing: %s\n", strings.Join(result.Missing, " "))
		}
	}

	return nil
}
