// Copyright 2025 Chainguard, Inc.
// SPDX-License-Identifier: Apache-2.0

package chelm

import (
	"bytes"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"chainguard.dev/sdk/helm/images"
	"github.com/google/go-containerregistry/pkg/name"
	"gopkg.in/yaml.v3"
)

// ExtractionResult contains images found by extractors.
type ExtractionResult struct {
	All         []images.OCIRef
	ByExtractor map[string][]string
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

	// Process in sorted order for deterministic output
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
			ref, ok := parseImageRef(candidate)
			if !ok {
				continue
			}
			// Use SDK for digest refs (preserves tag in tag@digest), fall back for tag-only
			ociRef, err := images.NewRef(candidate)
			if err != nil {
				// SDK requires digest; construct OCIRef for tag-only refs
				ociRef = images.OCIRef{
					Registry:     ref.Context().RegistryStr(),
					Repo:         ref.Context().RepositoryStr(),
					RegistryRepo: ref.Context().Name(),
					FullRef:      ref.Name(),
				}
				if t, ok := ref.(name.Tag); ok {
					ociRef.Tag = t.TagStr()
				}
			}
			normalized := ociRef.FullRef

			if !extSeen[normalized] {
				extSeen[normalized] = true
				extImages = append(extImages, normalized)
			}
			if !seen[normalized] {
				seen[normalized] = true
				result.All = append(result.All, ociRef)
			}
		}

		slices.Sort(extImages)
		result.ByExtractor[extName] = extImages
	}

	slices.SortFunc(result.All, func(a, b images.OCIRef) int {
		return strings.Compare(a.FullRef, b.FullRef)
	})

	return result
}

// StructuredExtractor extracts all string values from parsed YAML.
type StructuredExtractor struct{}

func (StructuredExtractor) Extract(docs []map[string]any) []string {
	var results []string
	var walk func(any)
	walk = func(obj any) {
		switch v := obj.(type) {
		case string:
			results = append(results, v)
		case map[string]any:
			for _, val := range v {
				walk(val)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	for _, doc := range docs {
		walk(doc)
	}
	return results
}

// RegexExtractor scans re-encoded YAML for image-like patterns.
// Re-encoding strips comments that might cause false positives.
type RegexExtractor struct{}

var imagePatterns = []*regexp.Regexp{
	// Full registry paths: gcr.io/project/image:tag or :tag@digest or @digest
	regexp.MustCompile(`[a-zA-Z0-9][-a-zA-Z0-9.]*\.[a-zA-Z0-9][-a-zA-Z0-9.]*(?::[0-9]+)?/[-a-zA-Z0-9._/]+(?::[a-zA-Z0-9][-a-zA-Z0-9._]*(?:@sha256:[a-fA-F0-9]{64})?|@sha256:[a-fA-F0-9]{64})`),
	// Digest refs: nginx@sha256:...
	regexp.MustCompile(`[a-zA-Z0-9][-a-zA-Z0-9._/]*@sha256:[a-fA-F0-9]{64}`),
	// Docker Hub org/repo:tag[@digest]
	regexp.MustCompile(`\b[a-zA-Z0-9][-a-zA-Z0-9_]*/[a-zA-Z0-9][-a-zA-Z0-9._]*:[a-zA-Z0-9][-a-zA-Z0-9._]*(?:@sha256:[a-fA-F0-9]{64})?\b`),
	// Simple repo:tag[@digest]
	regexp.MustCompile(`\b[a-zA-Z0-9][-a-zA-Z0-9._]*:[a-zA-Z0-9][-a-zA-Z0-9._]*(?:@sha256:[a-fA-F0-9]{64})?\b`),
}

func (RegexExtractor) Extract(docs []map[string]any) []string {
	// Re-encode docs to bytes (strips comments)
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

	// Sort by position, then filter overlaps
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

// parseImageRef validates and normalizes an image reference.
func parseImageRef(s string) (name.Reference, bool) {
	if strings.HasPrefix(s, "-") {
		return nil, false
	}
	if !strings.Contains(s, ":") && !strings.Contains(s, "@") {
		return nil, false
	}
	// Reject host:port (purely numeric tag)
	if idx := strings.LastIndex(s, ":"); idx != -1 && !strings.Contains(s, "@") {
		if _, err := strconv.ParseUint(s[idx+1:], 10, 64); err == nil {
			return nil, false
		}
	}
	ref, err := name.ParseReference(s)
	if err != nil {
		return nil, false
	}
	return ref, true
}
