package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chainguard-dev/clog"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/spf13/cobra"
)

type cfg struct {
	Image       string
	Environment string
	MatchMode   string
}

type ImageConfig struct {
	Config struct {
		Env []string `json:"Env"`
	} `json:"config"`
}

var validMatchModes = []string{"exact", "prefix", "relative", "contains"}	

func main() {
	cmd := Command()
	if err := cmd.Execute(); err != nil {
		clog.ErrorContextf(cmd.Context(), "failed to execute command: %v", err)
		os.Exit(1)
	}
}

func isValidMatchMode(mode string) bool {
	for _, valid := range validMatchModes {
		if mode == valid {
			return true
		}
	}
	return false
}

func Command() *cobra.Command {
	cfg := &cfg{}

	cmd := &cobra.Command{
		Use:   "crane-check",
		Short: "Check image config using crane",
		Long: `Tool that evaluates image configuration using crane.

The image to evaluate is simply passed via the '--image' argument (required)

Environment variables (required, --env) can be specified in two formats:
  - Space-separated: ENV_VAR1=value1 ENV_VAR2=value2
  - Newline-separated (useful for multi-line input)

Match modes (--match):
  - exact: Provided value and image value must match exactly (default)
  - prefix: Provided value is a prefix of the image value
  - relative: Either value is a prefix of the other
  - contains: The provided value is a substring within the image value

Usage:
  crane-check --image=cgr.dev/chainguard/go:latest --env="GOPATH=/go GOROOT=/usr/lib/go"
  crane-check --image=alpine:latest --env="PATH=/usr/lib" --match=relative`,
  		// We don't want to print usage every single time there's an error
		SilenceUsage: true,
		// We don't want duplicate error output
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Throw an error early if we provide an invalid match mode
			if !isValidMatchMode(cfg.MatchMode) {
				return fmt.Errorf("invalid match-mode: %s (must be one of: %s)", cfg.MatchMode, strings.Join(validMatchModes, ", "))
			}
			return cfg.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&cfg.Image, "image", "", "Image to be assessed (e.g., cgr.dev/chainguard/crane:latest) [required]")
	cmd.Flags().StringVar(&cfg.Environment, "env", "", "Environment variables to check (space or newline-separated, format: ENV_VAR=VALUE) [required]")
	cmd.Flags().StringVar(&cfg.MatchMode, "match", "exact", "Match mode: 'exact', 'relative', or 'contains'")

	cmd.MarkFlagRequired("image")
	cmd.MarkFlagRequired("env")

	return cmd
}

func (c *cfg) Run(ctx context.Context) error {
	log := clog.FromContext(ctx).With("image", c.Image)

	// Parse passed environment variables
	envVars := parseEnvironmentVariables(c.Environment)
	if envVars == nil {
		return fmt.Errorf("no environment variables provided")
	}
	log.InfoContext(ctx, "retrieving image configuration")

	// Get the image config with crane
	imageConfig, err := getCraneConfig(ctx, c.Image)
	if err != nil {
		return fmt.Errorf("failed to get image configuration: %w", err)
	}

	// Parse the config
	var imgCfg ImageConfig
	if err := json.Unmarshal([]byte(imageConfig), &imgCfg); err != nil {
		return fmt.Errorf("failed to parse image configuration: %w", err)
	}

	// Ensure environment exists in config
	if imgCfg.Config.Env == nil {
		return fmt.Errorf("failed to parse environment from image config")
	}
	log.InfoContext(ctx, "successfully parsed environment from image config")

	// Check each environment variable
	for _, envVar := range envVars {
		if err := matchEnvironmentVariable(ctx, envVar, imgCfg.Config.Env, c.MatchMode); err != nil {
			return err
		}
	}
	log.InfoContext(ctx, "all environment variables match expected values")

	return nil
}

func parseEnvironmentVariables(envStr string) []string {
	envStr = strings.TrimSpace(envStr)
	if envStr == "" {
		return nil
	}

	// Attempt to split input via newlines first
	// I want newline separation to "just" work in our pipelines
	lines := strings.Split(envStr, "\n")
	var envVars []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Ignore commented out lines and skip empty ones
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Account for space separated env vars
		if strings.Contains(line, " ") && strings.Count(line, "=") > 1 {
			envs := strings.Fields(line)
			envVars = append(envVars, envs...)
		} else {
			envVars = append(envVars, line)
		}
	}

	return envVars
}

func getCraneConfig(ctx context.Context, image string) (string, error) {
	log := clog.FromContext(ctx)

	// Get image config with crane
	configJSON, err := crane.Config(image, crane.WithContext(ctx))
	if err != nil {
		log.ErrorContextf(ctx, "crane config failed: %v", err)
		return "", fmt.Errorf("crane config failed: %w", err)
	}

	return string(configJSON), nil
}

func matchEnvironmentVariable(ctx context.Context, envStr string, imageEnv []string, matchMode string) error {
	log := clog.FromContext(ctx)

	// Split env var and value
	parts := strings.SplitN(envStr, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid environment variable format: %s (expected KEY=VALUE)", envStr)
	}

	envVar := parts[0]
	envVal := parts[1]

	// Make sure environment variable is in image config
	var foundEnv string
	found := false
	for _, imageEnvVar := range imageEnv {
		if strings.HasPrefix(imageEnvVar, envVar+"=") {
			foundEnv = imageEnvVar
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("failed to find %s in image config", envVar)
	}
	log.InfoContext(ctx, "found variable in image config", "var", envVar)

	// Now split the env from the image config
	imageParts := strings.SplitN(foundEnv, "=", 2)
	imageVal := ""
	if len(imageParts) == 2 {
		imageVal = imageParts[1]
	}

	matched := false
	switch matchMode {
	case "exact":
		matched = envVal == imageVal
	case "prefix":
		matched = strings.HasPrefix(imageVal, envVal)
	case "relative":
		matched = strings.HasPrefix(imageVal, envVal) || strings.HasPrefix(envVal, imageVal)
	case "contains":
		matched = strings.Contains(imageVal, envVal)
	default:
		// We should never ever reach this point, but if we do...
		return fmt.Errorf("unsupported match mode: %s", matchMode)
	}

	if !matched {
		return fmt.Errorf(`invalid value provided for %s:
  Provided value: %s
  Image value:    %s
  Match mode:     %s`, envVar, envVal, imageVal, matchMode)
	}
	log.InfoContext(ctx, "environment variable successfully matched", "var", envVar, "value", envVal, "match", matchMode)

	return nil
}
