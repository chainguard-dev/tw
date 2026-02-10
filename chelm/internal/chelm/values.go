package chelm

import (
	"fmt"
	"strings"

	"chainguard.dev/sdk/helm/images"
	"dario.cat/mergo"
	"github.com/google/go-containerregistry/pkg/name"
)

// Test constants for generating marker values.
// These map to the ${...} markers: registry, repo, tag, digest, pseudo_tag, ref, registry_repo
const (
	DefaultTestRegistry   = "cgr.test"
	DefaultTestRepository = "chainguard/test"
	DefaultTestTag        = "v0.0.0"
	DefaultTestDigest     = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	DefaultTestPseudoTag  = DefaultTestTag + "@" + DefaultTestDigest
)

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
				sb.WriteString(resolveField(v, repo))
			default:
				sb.WriteString(fmt.Sprint(v))
			}
		}
		return sb.String(), nil
	}
}

func resolveField(f images.RefField, repo name.Repository) string {
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
		return DefaultTestDigest
	case images.PseudoTag:
		return DefaultTestPseudoTag
	case images.Ref:
		return repo.Digest(DefaultTestDigest).Name()
	default:
		return ""
	}
}
