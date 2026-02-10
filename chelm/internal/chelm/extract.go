package chelm

import (
	"bytes"
	"io"
	"regexp"
	"slices"
	"strings"

	"chainguard.dev/sdk/helm/images"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ExtractedImage holds an image reference and its original extracted string.
type ExtractedImage struct {
	images.OCIRef
	Original string // the original string before normalization
}

// UnparseableCandidate records an image candidate that could not be parsed as an OCI reference.
type UnparseableCandidate struct {
	Candidate string
	Error     string
	Extractor string
}

// ExtractionResult contains images found by extractors.
type ExtractionResult struct {
	All         []ExtractedImage
	ByExtractor map[string][]string
	Unparseable []UnparseableCandidate
}

// Extractor finds candidate image references.
type Extractor interface {
	Extract(docs []map[string]any) []string
}

// ExtractImages parses YAML from r and runs extractors to find image references.
func ExtractImages(r io.Reader, extractors map[string]Extractor) *ExtractionResult {
	dec := yaml.NewDecoder(r)
	var docs []map[string]any
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break
		}
		docs = append(docs, doc)
	}

	result := &ExtractionResult{ByExtractor: make(map[string][]string)}
	seen := make(map[string]bool)

	extNames := make([]string, 0, len(extractors))
	for n := range extractors {
		extNames = append(extNames, n)
	}
	slices.Sort(extNames)

	for _, extName := range extNames {
		ext := extractors[extName]
		var extImages []string
		extSeen := make(map[string]bool)

		for _, candidate := range ext.Extract(docs) {
			ociRef, err := images.NewRef(candidate)
			if err != nil {
				result.Unparseable = append(result.Unparseable, UnparseableCandidate{
					Candidate: candidate,
					Error:     err.Error(),
					Extractor: extName,
				})
				continue
			}
			normalized := ociRef.FullRef

			if !extSeen[normalized] {
				extSeen[normalized] = true
				extImages = append(extImages, normalized)
			}
			if !seen[normalized] {
				seen[normalized] = true
				result.All = append(result.All, ExtractedImage{OCIRef: ociRef, Original: candidate})
			}
		}

		slices.Sort(extImages)
		result.ByExtractor[extName] = extImages
	}

	slices.SortFunc(result.All, func(a, b ExtractedImage) int {
		return strings.Compare(a.FullRef, b.FullRef)
	})

	return result
}

// GKPattern matches Kubernetes Group/Kind with optional wildcards.
type GKPattern struct {
	Group string // exact match, or "*" for any
	Kind  string // exact match, or "*" for any
}

func (p GKPattern) matches(gk schema.GroupKind) bool {
	return (p.Group == "*" || p.Group == gk.Group) &&
		(p.Kind == "*" || p.Kind == gk.Kind)
}

// ImagePathRule defines where to find container images for matching resources.
type ImagePathRule struct {
	Pattern GKPattern
	Paths   [][]string // paths to container arrays or image fields
}

// StructuredExtractor extracts container images from known Kubernetes resource locations.
type StructuredExtractor struct {
	rules []ImagePathRule
}

// NewStructuredExtractor creates an extractor with the given rules.
func NewStructuredExtractor(rules []ImagePathRule) *StructuredExtractor {
	return &StructuredExtractor{rules: rules}
}

// Extract finds all container images in the given documents.
// All matching rules are applied - images are collected from every rule that matches.
func (e *StructuredExtractor) Extract(docs []map[string]any) []string {
	var results []string

	for _, doc := range docs {
		apiVersion, _ := doc["apiVersion"].(string)
		kind, _ := doc["kind"].(string)
		gv, _ := schema.ParseGroupVersion(apiVersion)
		gk := schema.GroupKind{Group: gv.Group, Kind: kind}

		for _, rule := range e.rules {
			if !rule.Pattern.matches(gk) {
				continue
			}

			for _, path := range rule.Paths {
				val, found, _ := unstructured.NestedFieldNoCopy(doc, path...)
				if !found {
					continue
				}

				if s, ok := val.(string); ok && s != "" {
					results = append(results, s)
					continue
				}

				if arr, ok := val.([]any); ok {
					for _, item := range arr {
						if s, ok := item.(string); ok && s != "" {
							results = append(results, s)
						} else if m, ok := item.(map[string]any); ok {
							if img, _ := m["image"].(string); img != "" {
								results = append(results, img)
							}
						}
					}
				}
			}
		}
	}

	return results
}

// RegexExtractor scans YAML for image-like patterns with registry domains or digests.
type RegexExtractor struct{}

var imagePatterns = []*regexp.Regexp{
	// Registry/repo with tag and optional digest: gcr.io/project/image:tag[@digest]
	regexp.MustCompile(`[a-zA-Z0-9][-a-zA-Z0-9.]*\.[a-zA-Z0-9][-a-zA-Z0-9.]*(?::[0-9]+)?/[-a-zA-Z0-9._/]+:[a-zA-Z0-9][-a-zA-Z0-9._]*(?:@sha256:[a-fA-F0-9]{64})?`),
	// Registry/repo with digest only: gcr.io/project/image@sha256:...
	regexp.MustCompile(`[a-zA-Z0-9][-a-zA-Z0-9.]*\.[a-zA-Z0-9][-a-zA-Z0-9.]*(?::[0-9]+)?/[-a-zA-Z0-9._/]+@sha256:[a-fA-F0-9]{64}`),
	// Any image with digest: image@sha256:...
	regexp.MustCompile(`[a-zA-Z0-9][-a-zA-Z0-9._/]*@sha256:[a-fA-F0-9]{64}`),
}

// Extract finds image references in re-encoded YAML.
func (RegexExtractor) Extract(docs []map[string]any) []string {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	for _, doc := range docs {
		enc.Encode(doc)
	}
	enc.Close()
	raw := buf.Bytes()

	type match struct {
		value string
		start int
		end   int
	}

	var all []match
	for _, p := range imagePatterns {
		for _, loc := range p.FindAllIndex(raw, -1) {
			all = append(all, match{
				value: string(raw[loc[0]:loc[1]]),
				start: loc[0],
				end:   loc[1],
			})
		}
	}

	slices.SortFunc(all, func(a, b match) int { return a.start - b.start })

	var results []string
	lastEnd := 0
	for _, m := range all {
		if m.start >= lastEnd {
			results = append(results, m.value)
			lastEnd = m.end
		}
	}
	return results
}
