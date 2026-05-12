package chelm

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"chainguard.dev/sdk/helm/images"
	"dario.cat/mergo"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/opencontainers/go-digest"
)

// Test constants for generating marker values.
// These map to the ${...} markers: registry, repo, tag, digest, pseudo_tag, ref, registry_repo
const (
	DefaultTestRegistry   = "cgr.test"
	DefaultTestRepository = "chainguard/test"
	DefaultTestTag        = "v0.0.0"
)

// TestDigest returns a digest.Digest given an imageID.
func TestDigest(imageID string) digest.Digest {
	h := sha256.Sum256([]byte(imageID))
	return digest.NewDigestFromBytes(digest.SHA256, h[:])
}

// DeclaresDigest reports whether img's values reference a marker whose
// resolved value contains a digest: ${digest}, ${pseudo_tag} (tag@digest in
// a tag-shaped slot), or ${ref} (repo@digest).
func DeclaresDigest(img *images.Image) bool {
	if img == nil || img.Values == nil {
		return false
	}
	var found bool
	m := &images.Mapping{Images: map[string]*images.Image{"_": img}}
	// Abuse Walk for lexing: the SDK's lexer is unexported, but Walk recurses
	// through values and hands us the TokenList for each string. The return
	// value and any walk error are discarded — we only want the side effect.
	_, _ = m.Walk(func(_ string, tokens images.TokenList) (any, error) {
		for _, tok := range tokens {
			f, ok := tok.(images.RefField)
			if !ok {
				continue
			}
			if f == images.Digest || f == images.PseudoTag || f == images.Ref {
				found = true
			}
		}
		return "", nil
	})
	return found
}

// GenerateValues creates Helm values for a test case.
// Merges in order: image values < global test values < case values < extra values
func GenerateValues(meta *CGMeta, caseName, testRegistry string, extra map[string]any) (map[string]any, error) {
	// Find the test case
	var tc *TestCase
	for i := range meta.Test.Cases {
		if meta.Test.Cases[i].Name == caseName {
			tc = &meta.Test.Cases[i]
			break
		}
	}
	if tc == nil {
		return nil, fmt.Errorf("test case %q not found", caseName)
	}

	// Generate image values with test markers
	imageVals, err := generateImageValues(&images.Mapping{Images: meta.Images}, testRegistry)
	if err != nil {
		return nil, fmt.Errorf("generating image values: %w", err)
	}

	result := make(map[string]any)
	for _, layer := range []map[string]any{imageVals, meta.Test.Values, tc.Values, extra} {
		if err := mergo.Merge(&result, layer, mergo.WithOverride); err != nil {
			return nil, fmt.Errorf("merging values: %w", err)
		}
	}
	return result, nil
}

func generateImageValues(m *images.Mapping, testRegistry string) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}

	registry, err := name.NewRegistry(testRegistry)
	if err != nil {
		return nil, fmt.Errorf("invalid marker base %q: %w", testRegistry, err)
	}

	vals, err := m.Walk(testResolver(registry))
	if err != nil {
		return nil, err
	}
	return vals.Merge()
}

// testResolver returns a WalkFunc that substitutes markers with test values.
func testResolver(registry name.Registry) images.WalkFunc {
	return func(imageID string, tokens images.TokenList) (any, error) {
		repo := registry.Repo(DefaultTestRepository, strings.ToLower(imageID))

		var sb strings.Builder
		for _, tok := range tokens {
			switch v := tok.(type) {
			case images.RefField:
				sb.WriteString(resolveField(v, imageID, repo))
			default:
				sb.WriteString(fmt.Sprint(v))
			}
		}
		return sb.String(), nil
	}
}

func resolveField(f images.RefField, imageID string, repo name.Repository) string {
	d := TestDigest(imageID)
	switch f {
	case images.Registry:
		return repo.RegistryStr()
	case images.Repo:
		return repo.RepositoryStr()
	case images.RegistryRepo:
		return repo.Name()
	case images.Tag:
		return DefaultTestTag
	case images.Digest:
		return d.String()
	case images.PseudoTag:
		return DefaultTestTag + "@" + d.String()
	case images.Ref:
		return repo.Digest(d.String()).Name()
	default:
		return ""
	}
}
