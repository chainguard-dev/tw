package chelm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"chainguard.dev/sdk/helm/images"
)

// Parse parses and validates a cg.json from the given reader.
func Parse(r io.Reader) (*CGMeta, error) {
	var meta CGMeta
	if err := json.NewDecoder(r).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decoding JSON: %w", err)
	}

	// Default to a single "default" test case if none specified
	if meta.Test == nil {
		meta.Test = &TestSpec{}
	}
	if len(meta.Test.Cases) == 0 {
		meta.Test.Cases = []TestCase{{Name: "default"}}
	}

	if err := meta.Validate(); err != nil {
		return nil, err
	}
	return &meta, nil
}

// Validate checks the CGMeta for consistency.
func (m *CGMeta) Validate() error {
	// Validate markers via SDK (requires round-trip through images.Parse)
	if len(m.Images) > 0 {
		if err := validateMarkers(&images.Mapping{Images: m.Images}); err != nil {
			return err
		}
	}

	// Validate test case image references
	if m.Test == nil {
		return nil
	}
	for _, tc := range m.Test.Cases {
		for _, imgID := range tc.Images {
			if _, ok := m.Images[imgID]; !ok {
				return fmt.Errorf("test case %q references unknown image %q", tc.Name, imgID)
			}
		}
	}
	return nil
}

// validateMarkers uses the SDK's Parse to validate ${...} markers.
func validateMarkers(m *images.Mapping) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshaling for validation: %w", err)
	}
	_, err = images.Parse(bytes.NewReader(data))
	return err
}
