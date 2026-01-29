package trim

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// Config holds the command configuration
type Config struct {
	DryRun         bool
	Verbose        bool
	JSONOutput     bool
	NoPipelineTrim bool
	Arch           string
}

// RedundantPkg represents a package that was determined to be redundant
type RedundantPkg struct {
	Package    string `json:"package"`
	ProvidedBy string `json:"provided_by"`
	Reason     string `json:"reason"`
}

// TrimResult contains the results of trimming a single file
type TrimResult struct {
	File         string         `json:"file"`
	Redundant    []RedundantPkg `json:"redundant"`
	TotalRemoved int            `json:"total_removed"`
	Error        string         `json:"error,omitempty"`
}

// Command returns the cobra command for trim
func Command() *cobra.Command {
	cfg := &Config{}
	cmd := &cobra.Command{
		Use:   "trim [flags] <yaml-file> [yaml-file...]",
		Short: "Remove redundant package dependencies from melange YAML files",
		Long: `Trim removes redundant package dependencies from melange YAML files by
analyzing APK dependency relationships.

A package is considered redundant if:
- It's a transitive dependency of another package in the same list
- It's provided by a pipeline used in the same scope

Example:
  tw trim mypackage.yaml
  tw trim --dry-run mypackage.yaml
  tw trim --verbose mypackage.yaml another.yaml`,
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.run(cmd.Context(), args, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVarP(&cfg.DryRun, "dry-run", "n", false, "Show what would be removed without modifying")
	cmd.Flags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "Print detailed dependency analysis")
	cmd.Flags().BoolVar(&cfg.JSONOutput, "json", false, "Output results as JSON")
	cmd.Flags().BoolVar(&cfg.NoPipelineTrim, "no-pipeline-trim", false, "Disable pipeline-based trimming")
	cmd.Flags().StringVar(&cfg.Arch, "arch", "", "Target architecture (e.g., x86_64, aarch64). Defaults to host architecture")

	return cmd
}

func (c *Config) run(ctx context.Context, files []string, out io.Writer) error {
	var results []TrimResult

	// Initialize pipeline resolver if needed
	var pipelineResolver *PipelineResolver
	if !c.NoPipelineTrim {
		var err error
		pipelineResolver, err = NewPipelineResolver()
		if err != nil {
			if c.Verbose {
				fmt.Fprintf(out, "Warning: failed to load pipeline packages: %v\n", err)
			}
			// Continue without pipeline trimming
		}
	}

	for _, file := range files {
		result := c.processFile(ctx, file, pipelineResolver, out)
		results = append(results, result)
	}

	// Output results
	if c.JSONOutput {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	// Text output is already printed during processing
	return nil
}

func (c *Config) processFile(ctx context.Context, filePath string, pipelineResolver *PipelineResolver, out io.Writer) TrimResult {
	result := TrimResult{File: filePath}

	// Parse the YAML file
	yamlFile, err := ParseMelangeYAML(filePath)
	if err != nil {
		result.Error = err.Error()
		if !c.JSONOutput {
			fmt.Fprintf(out, "%s: error: %v\n", filePath, err)
		}
		return result
	}

	// Get repositories and build dependency resolver
	repos := yamlFile.GetRepositories()
	if len(repos) == 0 {
		// Use default wolfi repository
		repos = []string{"https://packages.wolfi.dev/os"}
	}

	// Filter out special repository entries that can't be fetched
	repos = filterRepositories(repos)

	// Determine architecture
	arch := c.Arch
	if arch == "" {
		arch = normalizeArch(runtime.GOARCH)
	}

	// Create dependency resolver
	depResolver, err := NewResolver(ctx, repos, nil, arch)
	if err != nil {
		if c.Verbose {
			fmt.Fprintf(out, "Warning: failed to create dependency resolver: %v\n", err)
		}
		// Continue without APK-based trimming, just do pipeline trimming
		depResolver = nil
	}

	// Get all package lists
	packageLists := yamlFile.GetPackages()
	pipelineUses := yamlFile.GetPipelineUses()

	if c.Verbose && !c.JSONOutput {
		fmt.Fprintf(out, "\n%s:\n", filePath)
		fmt.Fprintf(out, "  Found %d package lists\n", len(packageLists))
		for path, pkgs := range packageLists {
			fmt.Fprintf(out, "    %s: %d packages\n", path, len(pkgs))
		}
	}

	// Process each package list
	for path, packages := range packageLists {
		if len(packages) == 0 {
			continue
		}

		redundant := c.findRedundantPackages(packages, depResolver, pipelineResolver, pipelineUses, path)
		if len(redundant) == 0 {
			continue
		}

		// Report redundant packages
		for _, r := range redundant {
			result.Redundant = append(result.Redundant, r)
			if !c.JSONOutput {
				if c.DryRun {
					fmt.Fprintf(out, "%s: would remove %s from %s (%s: %s)\n",
						filePath, r.Package, path, r.Reason, r.ProvidedBy)
				} else {
					fmt.Fprintf(out, "%s: removing %s from %s (%s: %s)\n",
						filePath, r.Package, path, r.Reason, r.ProvidedBy)
				}
			}
		}

		// Remove packages if not dry-run
		if !c.DryRun {
			toRemove := make([]string, len(redundant))
			for i, r := range redundant {
				toRemove[i] = r.Package
			}
			removed := yamlFile.RemovePackages(path, toRemove)
			result.TotalRemoved += len(removed)
		} else {
			result.TotalRemoved += len(redundant)
		}
	}

	// Write changes if not dry-run
	if !c.DryRun && result.TotalRemoved > 0 {
		if err := yamlFile.Write(); err != nil {
			result.Error = fmt.Sprintf("failed to write file: %v", err)
			if !c.JSONOutput {
				fmt.Fprintf(out, "%s: error writing: %v\n", filePath, err)
			}
		}
	}

	if !c.JSONOutput && result.TotalRemoved > 0 {
		action := "removed"
		if c.DryRun {
			action = "would remove"
		}
		fmt.Fprintf(out, "%s: %s %d redundant packages\n", filePath, action, result.TotalRemoved)
	} else if !c.JSONOutput && len(packageLists) > 0 {
		fmt.Fprintf(out, "%s: no redundant packages found\n", filePath)
	}

	return result
}

func (c *Config) findRedundantPackages(
	packages []string,
	depResolver *DependencyResolver,
	pipelineResolver *PipelineResolver,
	pipelineUses map[string][]string,
	packagePath string,
) []RedundantPkg {
	var redundant []RedundantPkg
	pkgSet := make(map[string]bool)
	for _, pkg := range packages {
		pkgSet[pkg] = true
	}

	// Pipeline-based trimming only applies to build-time packages (*.contents.packages),
	// NOT to runtime dependencies (*.dependencies.runtime).
	// Pipelines provide build-time dependencies, not runtime dependencies.
	isBuildTimePackages := strings.HasSuffix(packagePath, ".contents.packages")

	// Get packages provided by pipelines in this scope (only for build-time packages)
	var pipelineProvidedPkgs map[string]string
	if pipelineResolver != nil && isBuildTimePackages {
		scope := getPipelineScope(packagePath)
		pipelines := pipelineUses[scope]
		if len(pipelines) > 0 {
			pipelineProvidedPkgs = pipelineResolver.GetPackagesFromPipelines(pipelines)
		}
	}

	// Precompute transitive deps for all packages to avoid O(n²) IsTransitiveDep calls
	// Maps package name -> set of all packages that have it as a transitive dep
	providedBy := make(map[string]string)
	if depResolver != nil {
		for _, pkg := range packages {
			for dep := range depResolver.GetTransitiveDeps(pkg) {
				if pkgSet[dep] && providedBy[dep] == "" {
					providedBy[dep] = pkg
				}
			}
		}
	}

	for _, pkg := range packages {
		// Check if provided by a pipeline (only for build-time packages)
		if pipelineProvidedPkgs != nil {
			if provider, ok := pipelineProvidedPkgs[pkg]; ok {
				redundant = append(redundant, RedundantPkg{
					Package:    pkg,
					ProvidedBy: provider,
					Reason:     "pipeline provides",
				})
				continue
			}
		}

		// Check if it's a transitive dependency of another package in the list
		if provider, ok := providedBy[pkg]; ok {
			redundant = append(redundant, RedundantPkg{
				Package:    pkg,
				ProvidedBy: provider,
				Reason:     "transitive dependency",
			})
		}
	}

	return redundant
}

// getPipelineScope maps a package path to its corresponding pipeline scope
func getPipelineScope(packagePath string) string {
	// environment.contents.packages -> pipeline
	// package.dependencies.runtime -> pipeline
	// test.environment.contents.packages -> test.pipeline
	// subpackages[name].dependencies.runtime -> subpackages[name].pipeline
	// subpackages[name].test.environment.contents.packages -> subpackages[name].test.pipeline

	if strings.HasPrefix(packagePath, "test.") {
		return "test.pipeline"
	}

	if strings.HasPrefix(packagePath, "subpackages[") {
		// Extract subpackage identifier
		endBracket := strings.Index(packagePath, "]")
		if endBracket > 0 {
			prefix := packagePath[:endBracket+1]
			rest := packagePath[endBracket+1:]
			if strings.HasPrefix(rest, ".test.") {
				return prefix + ".test.pipeline"
			}
			return prefix + ".pipeline"
		}
	}

	return "pipeline"
}

// normalizeArch converts Go's GOARCH values to APK architecture names
func normalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return goarch
	}
}

// filterRepositories removes special repository entries that can't be fetched
// (e.g., @local paths, tagged repositories)
func filterRepositories(repos []string) []string {
	var filtered []string
	for _, repo := range repos {
		// Skip tagged local repositories (e.g., "@local /path/to/repo")
		if strings.HasPrefix(repo, "@") {
			continue
		}
		// Skip file:// URLs which are local paths
		if strings.HasPrefix(repo, "file://") {
			continue
		}
		// Skip bare paths (not URLs)
		if !strings.HasPrefix(repo, "http://") && !strings.HasPrefix(repo, "https://") {
			continue
		}
		filtered = append(filtered, repo)
	}
	return filtered
}
