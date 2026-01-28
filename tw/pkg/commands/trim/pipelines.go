package trim

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"chainguard.dev/melange/pkg/build"
	"chainguard.dev/melange/pkg/config"
	"gopkg.in/yaml.v3"
)

// PipelinePackages extracts needs.packages from all embedded melange pipelines
// Returns a map of pipeline name -> list of packages it needs
func PipelinePackages() (map[string][]string, error) {
	result := make(map[string][]string)

	err := fs.WalkDir(build.PipelinesFS, "pipelines", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}

		data, err := build.PipelinesFS.ReadFile(path)
		if err != nil {
			return err
		}

		var pipeline config.Pipeline
		if err := yaml.Unmarshal(data, &pipeline); err != nil {
			// Skip files that don't parse as pipelines
			return nil
		}

		// Extract pipeline name from path: "pipelines/go/build.yaml" -> "go/build"
		name := strings.TrimPrefix(path, "pipelines/")
		name = strings.TrimSuffix(name, ".yaml")

		if pipeline.Needs != nil && len(pipeline.Needs.Packages) > 0 {
			// Apply default input values for substitution
			packages := applyDefaults(pipeline.Needs.Packages, pipeline.Inputs)
			result[name] = packages
		}

		return nil
	})

	return result, err
}

// applyDefaults substitutes ${{inputs.X}} with default values from pipeline.Inputs
func applyDefaults(packages []string, inputs map[string]config.Input) []string {
	var result []string
	for _, pkg := range packages {
		resolvedPkg := pkg
		// Substitute all input references with their default values
		for name, input := range inputs {
			placeholder := fmt.Sprintf("${{inputs.%s}}", name)
			resolvedPkg = strings.ReplaceAll(resolvedPkg, placeholder, input.Default)
		}
		// Only include if fully resolved (no remaining ${{ references)
		if !strings.Contains(resolvedPkg, "${{") && resolvedPkg != "" {
			result = append(result, resolvedPkg)
		}
	}
	return result
}

// PipelineResolver provides lookup of packages required by pipelines
type PipelineResolver struct {
	// pipelinePackages maps pipeline name -> packages it needs
	pipelinePackages map[string][]string
}

// NewPipelineResolver creates a resolver for pipeline packages
func NewPipelineResolver() (*PipelineResolver, error) {
	pkgs, err := PipelinePackages()
	if err != nil {
		return nil, fmt.Errorf("loading pipeline packages: %w", err)
	}
	return &PipelineResolver{pipelinePackages: pkgs}, nil
}

// GetPipelinePackages returns the packages needed by a pipeline
func (r *PipelineResolver) GetPipelinePackages(pipelineName string) []string {
	return r.pipelinePackages[pipelineName]
}

// InferTestPipelinePackage infers what package a test pipeline provides
// For test/tw/foo-check -> provides "foo-check"
func InferTestPipelinePackage(pipelineName string) string {
	return filepath.Base(pipelineName)
}

// GetPackagesFromPipelines extracts all packages needed by a list of pipeline uses
func (r *PipelineResolver) GetPackagesFromPipelines(pipelineUses []string) map[string]string {
	result := make(map[string]string)
	for _, use := range pipelineUses {
		pkgs := r.GetPipelinePackages(use)
		for _, pkg := range pkgs {
			result[pkg] = use
		}
		// For test pipelines, also infer the package name
		if strings.HasPrefix(use, "test/") {
			inferredPkg := InferTestPipelinePackage(use)
			result[inferredPkg] = use
		}
	}
	return result
}
