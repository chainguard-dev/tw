package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"chainguard.dev/tw/chelm/internal/chelm"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// TestOutput is the JSON output format for chelm test.
type TestOutput struct {
	Passed bool         `json:"passed"`
	Cases  []CaseOutput `json:"cases"`
}

// CaseOutput is the result for a single test case.
type CaseOutput struct {
	Name       string              `json:"name"`
	Passed     bool                `json:"passed"`
	Images     []string            `json:"images"`
	Expected   []string            `json:"expected,omitempty"`
	Missing    []string            `json:"missing,omitempty"`
	Extractors map[string][]string `json:"extractors,omitempty"`
	Error      string              `json:"error,omitempty"`
}

var extractors = map[string]chelm.Extractor{
	"structured": chelm.NewStructuredExtractor([]chelm.ImagePathRule{
		{Pattern: chelm.GKPattern{Group: "", Kind: "Pod"}, Paths: [][]string{
			{"spec", "containers"},
			{"spec", "initContainers"},
			{"spec", "ephemeralContainers"},
		}},
		{Pattern: chelm.GKPattern{Group: "apps", Kind: "Deployment"}, Paths: [][]string{
			{"spec", "template", "spec", "containers"},
			{"spec", "template", "spec", "initContainers"},
			{"spec", "template", "spec", "ephemeralContainers"},
		}},
		{Pattern: chelm.GKPattern{Group: "apps", Kind: "DaemonSet"}, Paths: [][]string{
			{"spec", "template", "spec", "containers"},
			{"spec", "template", "spec", "initContainers"},
			{"spec", "template", "spec", "ephemeralContainers"},
		}},
		{Pattern: chelm.GKPattern{Group: "apps", Kind: "ReplicaSet"}, Paths: [][]string{
			{"spec", "template", "spec", "containers"},
			{"spec", "template", "spec", "initContainers"},
			{"spec", "template", "spec", "ephemeralContainers"},
		}},
		{Pattern: chelm.GKPattern{Group: "apps", Kind: "StatefulSet"}, Paths: [][]string{
			{"spec", "template", "spec", "containers"},
			{"spec", "template", "spec", "initContainers"},
			{"spec", "template", "spec", "ephemeralContainers"},
		}},
		{Pattern: chelm.GKPattern{Group: "batch", Kind: "Job"}, Paths: [][]string{
			{"spec", "template", "spec", "containers"},
			{"spec", "template", "spec", "initContainers"},
			{"spec", "template", "spec", "ephemeralContainers"},
		}},
		{Pattern: chelm.GKPattern{Group: "batch", Kind: "CronJob"}, Paths: [][]string{
			{"spec", "jobTemplate", "spec", "template", "spec", "containers"},
			{"spec", "jobTemplate", "spec", "template", "spec", "initContainers"},
			{"spec", "jobTemplate", "spec", "template", "spec", "ephemeralContainers"},
		}},
		// Known service mesh sidecar image annotations (wildcard - any GK)
		{Pattern: chelm.GKPattern{Group: "*", Kind: "*"}, Paths: [][]string{
			{"metadata", "annotations", "sidecar.istio.io/proxyImage"},
			{"metadata", "annotations", "inject.istio.io/templates"},
			{"metadata", "annotations", "linkerd.io/proxy-image"},
		}},
	}),
	"regex": chelm.RegexExtractor{},
}

var testCmd = &cobra.Command{
	Use:   "test <cg.json>",
	Short: "Validate chart images against cg.json",
	Long: `Run all test cases defined in cg.json and validate images.

For each test case:
  1. Generate helm values with test markers
  2. Render chart with helm template
  3. Extract and validate all images

Exit code is non-zero if any test case fails.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		chartPath, _ := cmd.Flags().GetString("chart")
		kubeVersion, _ := cmd.Flags().GetString("kube-version")
		extraValuesStr, _ := cmd.Flags().GetString("extra-values")
		setFlags, _ := cmd.Flags().GetStringSlice("set")
		testRegistry, _ := cmd.Flags().GetString("test-registry")

		// Load cg.json
		f, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer f.Close()

		meta, err := chelm.Parse(f)
		if err != nil {
			return err
		}

		// Parse extra values
		var extraValues map[string]any
		if extraValuesStr != "" {
			if err := yaml.Unmarshal([]byte(extraValuesStr), &extraValues); err != nil {
				return fmt.Errorf("parsing extra-values: %w", err)
			}
		}

		// Validate marker paths exist in chart values.yaml
		valuesPath := filepath.Join(chartPath, "values.yaml")
		vf, err := os.Open(valuesPath)
		if err != nil {
			return fmt.Errorf("opening chart values: %w", err)
		}
		var chartValues map[string]any
		if err := yaml.NewDecoder(vf).Decode(&chartValues); err != nil {
			vf.Close()
			return fmt.Errorf("decoding chart values: %w", err)
		}
		vf.Close()

		if err := chelm.ValidateValuesPaths(meta.Images, chartValues); err != nil {
			return err
		}

		output := TestOutput{Passed: true}

		// Run each test case
		for _, tc := range meta.Test.Cases {
			caseOut := CaseOutput{Name: tc.Name, Passed: true}

			// Generate values
			values, err := chelm.GenerateValues(meta, tc.Name, testRegistry, extraValues)
			if err != nil {
				caseOut.Error = fmt.Sprintf("generating values: %v", err)
				caseOut.Passed = false
				output.Passed = false
				output.Cases = append(output.Cases, caseOut)
				continue
			}

			// Write values to temp file
			valuesFile, err := os.CreateTemp("", "chelm-values-*.yaml")
			if err != nil {
				return err
			}
			enc := yaml.NewEncoder(valuesFile)
			enc.SetIndent(2)
			if err := enc.Encode(values); err != nil {
				os.Remove(valuesFile.Name())
				return err
			}
			valuesFile.Close()

			// Run helm template
			helmArgs := []string{"template", chartPath, "-f", valuesFile.Name(), "--skip-tests", "--skip-crds"}
			if kubeVersion != "" {
				helmArgs = append(helmArgs, "--kube-version", kubeVersion)
			}
			for _, s := range setFlags {
				helmArgs = append(helmArgs, "--set", s)
			}

			helmCmd := exec.Command("helm", helmArgs...)
			var rendered, helmStderr bytes.Buffer
			helmCmd.Stdout = &rendered
			helmCmd.Stderr = &helmStderr

			if err := helmCmd.Run(); err != nil {
				os.Remove(valuesFile.Name())
				caseOut.Error = fmt.Sprintf("helm template: %v: %s", err, helmStderr.String())
				caseOut.Passed = false
				output.Passed = false
				output.Cases = append(output.Cases, caseOut)
				continue
			}
			os.Remove(valuesFile.Name())

			// Extract images
			extraction := chelm.ExtractImages(&rendered, extractors)

			for _, u := range extraction.Unparseable {
				fmt.Fprintf(cmd.ErrOrStderr(), "WARN: ignoring unparseable image reference %q (extractor %s): %s\n",
					u.Candidate, u.Extractor, u.Error)
			}

			// Build ignore set - matches against original extracted strings
			ignoreSet := make(map[string]bool)
			for _, ig := range meta.Test.Ignore {
				ignoreSet[ig] = true
			}

			// Build set of expected image IDs for this test case
			expectedImageIDs := make(map[string]bool)
			for _, id := range tc.Images {
				expectedImageIDs[strings.ToLower(id)] = true
			}
			caseOut.Expected = tc.Images
			foundImageIDs := make(map[string]bool)

			// Validate each extracted image is fully parameterized with test markers
			for _, ref := range extraction.All {
				caseOut.Images = append(caseOut.Images, ref.FullRef)

				if ignoreSet[ref.Original] {
					continue
				}

				// Check registry (case-insensitive per OCI spec)
				if !strings.EqualFold(ref.Registry, testRegistry) {
					caseOut.Passed = false
					output.Passed = false
					continue
				}

				// Check repository: must be {DefaultTestRepository}/{imageID}
				repoPrefix := chelm.DefaultTestRepository + "/"
				if !strings.HasPrefix(ref.Repo, repoPrefix) {
					caseOut.Passed = false
					output.Passed = false
					continue
				}
				imageID := strings.TrimPrefix(ref.Repo, repoPrefix)
				if !expectedImageIDs[imageID] {
					caseOut.Passed = false
					output.Passed = false
					continue
				}
				foundImageIDs[imageID] = true

				// Check tag/digest matches test values
				if ref.Digest != "" && ref.Digest != chelm.DefaultTestDigest {
					caseOut.Passed = false
					output.Passed = false
				}
				if ref.Tag != "" && ref.Tag != chelm.DefaultTestTag {
					caseOut.Passed = false
					output.Passed = false
				}
			}

			// Check all expected images were found
			for id := range expectedImageIDs {
				if !foundImageIDs[id] {
					caseOut.Missing = append(caseOut.Missing, id)
					caseOut.Passed = false
					output.Passed = false
				}
			}
			caseOut.Extractors = extraction.ByExtractor

			output.Cases = append(output.Cases, caseOut)
		}

		// Always output JSON
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(output); err != nil {
			return err
		}

		if !output.Passed {
			for _, c := range output.Cases {
				if !c.Passed {
					fmt.Fprintf(cmd.ErrOrStderr(), "FAIL: case %q", c.Name)
					if len(c.Missing) > 0 {
						fmt.Fprintf(cmd.ErrOrStderr(), ": missing images: %s", strings.Join(c.Missing, ", "))
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "\n")
					if len(c.Missing) > 0 {
						fmt.Fprintf(cmd.ErrOrStderr(), "  %d images expected, %d found\n",
							len(c.Expected), len(c.Expected)-len(c.Missing))
					}
				}
			}
			return fmt.Errorf("validation failed")
		}
		return nil
	},
}

func init() {
	testCmd.Flags().String("chart", ".", "Path to chart directory")
	testCmd.Flags().String("kube-version", "", "Kubernetes version for helm template")
	testCmd.Flags().String("extra-values", "", "Extra values YAML to merge")
	testCmd.Flags().StringSlice("set", nil, "Set values (passed to helm --set)")
	testCmd.Flags().String("test-registry", chelm.DefaultTestRegistry, "Registry for test marker images")
}
