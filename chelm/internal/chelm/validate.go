package chelm

import (
	"fmt"
	"strings"

	"chainguard.dev/sdk/helm/images"
)

// ValidateValuesPaths checks that every leaf in each image's Values map has a
// corresponding key in chartValues. Every leaf path in Values will become a
// JSON patch replace operation at resolve time, so missing paths cause cryptic
// patch errors. This validates early with a clear message.
func ValidateValuesPaths(imgs map[string]*images.Image, chartValues map[string]any) error {
	var errs []string
	for imageID, img := range imgs {
		if img == nil || img.Values == nil {
			continue
		}
		errs = checkPaths(errs, imageID, img.Values, chartValues, nil)
	}
	if len(errs) > 0 {
		return fmt.Errorf("values path validation failed:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// checkPaths recursively walks vals (an image's Values subtree) alongside
// the corresponding chartVals subtree from values.yaml. For each leaf, it checks
// that the key exists in chartVals. chartVals is nil when an ancestor was missing.
func checkPaths(errs []string, imageID string, vals, chartVals map[string]any, path []string) []string {
	for key, v := range vals {
		p := append(path, key)

		if child, ok := v.(map[string]any); ok {
			var nextChart map[string]any
			if chartVals != nil {
				if sub, ok := chartVals[key]; ok {
					if m, ok := sub.(map[string]any); ok {
						nextChart = m
					}
				}
			}
			errs = checkPaths(errs, imageID, child, nextChart, p)
			continue
		}

		// Leaf value — must exist in chart values.
		if chartVals == nil {
			errs = append(errs, fmt.Sprintf(
				"image %q sets value at path %s, but the chart's values.yaml has no key at that path",
				imageID, strings.Join(p, ".")))
		} else if _, ok := chartVals[key]; !ok {
			errs = append(errs, fmt.Sprintf(
				"image %q sets value at path %s, but the chart's values.yaml has no key at that path",
				imageID, strings.Join(p, ".")))
		}
	}
	return errs
}
