// Package chelm provides core functionality for validating Helm chart image mappings.
package chelm

import "chainguard.dev/sdk/helm/images"

// CGMeta is the cg.json schema for Chainguard Helm chart metadata.
// After parsing with Parse(), Test is guaranteed to be non-nil with at least one case.
type CGMeta struct {
	Images map[string]*images.Image `json:"images,omitempty"`
	Test   *TestSpec                `json:"test,omitempty"`
}

// TestSpec defines test configuration for chart validation.
type TestSpec struct {
	Values map[string]any `json:"values,omitempty"` // Global values for all cases
	Ignore []string       `json:"ignore,omitempty"` // Images to ignore during checks
	Cases  []TestCase     `json:"cases"`
}

// TestCase defines a single test case.
type TestCase struct {
	Name   string         `json:"name"`
	Images []string       `json:"images,omitempty"` // Image IDs to include in this case
	Values map[string]any `json:"values,omitempty"` // Case-specific values
	// Registry every image must resolve under, defaulting to the test
	// registry. A case that sets a chart's registry override in Values names
	// the host it expects here, so an override the chart ignores fails instead
	// of rendering the same refs as the case without it.
	Registry string `json:"registry,omitempty"`
}
