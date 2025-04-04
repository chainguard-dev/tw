package helm

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	DefaultTimeout = 5 * time.Minute
)

type cmdConfig struct {
	path   string
	values []string
}

type Inventory struct {
	Chart  InventoryChartInfo `json:"chart"`
	Values any                `json:"values"`
}

type InventoryChartInfo struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
}

func Command() *cobra.Command {
	cfg := &cmdConfig{}

	cmd := &cobra.Command{
		Use:           "helm-inventory -- [HELM_COMMAND...]",
		Short:         "Wrap a helm [install|upgrade] command to produce an inventory suitable for an intoto attestation",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd.Context(), args)
		},
	}

	cmd.Flags().StringVarP(&cfg.path, "path", "p", "", "Path where the inventory will be written")
	cmd.Flags().StringSliceVarP(&cfg.values, "values", "f", []string{}, "Value files to include in the inventory, must match with what's passed to helm.")

	return cmd
}

func (c *cmdConfig) Run(ctx context.Context, hargs []string) error {
	clog.InfoContext(ctx, "running whelm", "helm_args", hargs)

	hopts, err := helmCommand(hargs...)
	if err != nil {
		return fmt.Errorf("failed to parse helm command: %w", err)
	}

	clog.InfoContext(ctx, "running helm command",
		"subcommand", strings.Join(hargs, " "),
		"chart_name", hopts.chart,
		"chart_repo", hopts.repo,
		"chart_version", hopts.version,
		"chart_namespace", hopts.namespace,
	)

	tdir, err := os.MkdirTemp("", "helm-inventory-pull-")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tdir)

	cpath, err := pullChart(ctx, tdir, hopts)
	if err != nil {
		return fmt.Errorf("failed to pull chart: %w", err)
	}

	inv, err := c.inventory(ctx, cpath, hopts)
	if err != nil {
		return fmt.Errorf("failed to create inventory: %w", err)
	}

	if c.path != "" {
		rawInv, err := json.Marshal(inv)
		if err != nil {
			return fmt.Errorf("failed to marshal inventory: %w", err)
		}

		if err := os.WriteFile(c.path, rawInv, 0644); err != nil {
			return fmt.Errorf("failed to write inventory to file: %w", err)
		}
	}

	clog.InfoContext(ctx, "inventory created", "inventory", inv)

	// Run the actual helm command we're wrapping
	hcmd := exec.CommandContext(ctx, hargs[0], hargs[1:]...)
	hcmd.Stdout = os.Stdout
	hcmd.Stderr = os.Stderr
	hcmd.Stdin = os.Stdin

	if err := hcmd.Run(); err != nil {
		return fmt.Errorf("wrapped helm command failed: %w", err)
	}

	return nil
}

func pullChart(ctx context.Context, destDir string, hopts *helmOpts) (string, error) {
	pullArgs := []string{
		"pull", hopts.chart,
		"--destination", destDir,
	}

	if hopts.repo != "" {
		pullArgs = append(pullArgs, "--repo", hopts.repo)
	}

	if hopts.version != "" {
		pullArgs = append(pullArgs, "--version", hopts.version)
	}

	cmd := exec.CommandContext(ctx, "helm", pullArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	clog.InfoContext(ctx, "pulling chart", "cmd", cmd.Args)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("helm command failed: %w", err)
	}

	// Find the chart file
	matches, err := filepath.Glob(filepath.Join(destDir, "*.tgz"))
	if err != nil {
		return "", fmt.Errorf("failed to glob files: %w", err)
	}

	if len(matches) != 1 {
		return "", fmt.Errorf("expected 1 file, got %d", len(matches))
	}

	return matches[0], nil
}

func (c *cmdConfig) inventory(_ context.Context, chartPath string, hopts *helmOpts) (*Inventory, error) {
	f, err := os.Open(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	teer := io.TeeReader(f, hasher)

	gzr, err := gzip.NewReader(teer)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	meta := new(chart.Metadata)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read next header: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		if filepath.Base(hdr.Name) != "Chart.yaml" {
			continue
		}

		found = true

		if err := yaml.NewDecoder(tr).Decode(&meta); err != nil {
			return nil, fmt.Errorf("failed to decode Chart.yaml: %w", err)
		}

		break
	}

	if !found {
		return nil, fmt.Errorf("failed to find Chart.yaml")
	}

	// Include the allowlist'd values files
	vfiles := make(map[string]string)
	for _, v := range c.values {
		vfiles[v] = v
	}

	vinvs := []string{}
	for _, v := range hopts.values {
		vf, ok := vfiles[v]
		if !ok {
			continue
		}

		vinvs = append(vinvs, vf)
	}

	vopts := values.Options{
		ValueFiles: vinvs,
	}
	vals, err := vopts.MergeValues(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to merge values: %w", err)
	}

	return &Inventory{
		Chart: InventoryChartInfo{
			Name:       meta.Name,
			Version:    meta.Version,
			Repository: hopts.repo,
			Digest:     fmt.Sprintf("sha256:%x", hasher.Sum(nil)),
		},
		Values: vals,
	}, nil
}

type helmOpts struct {
	name         string
	namespace    string
	version      string
	repo         string
	values       []string
	op           string
	chart        string
	generateName bool
}

func helmCommand(args ...string) (*helmOpts, error) {
	opts := &helmOpts{}

	cmd := &cobra.Command{
		Use:                "helm",
		Args:               cobra.ExactArgs(2),
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.Flags().StringVarP(&opts.repo, "repo", "r", "", "")
	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "default", "")
	cmd.Flags().StringVarP(&opts.version, "version", "v", "", "")
	cmd.Flags().StringSliceVarP(&opts.values, "values", "f", []string{}, "")
	cmd.Flags().BoolVarP(&opts.generateName, "generate-name", "g", false, "")

	if err := cmd.ParseFlags(args); err != nil {
		clog.Warnf("found flags we don't know about: %v", err)
	}

	remainingArgs := cmd.Flags().Args()
	if len(remainingArgs) < 2 {
		return nil, fmt.Errorf("not enough positional arguments")
	}

	opts.op = remainingArgs[1] // install/upgrade

	if opts.op == "install" {
		if opts.generateName {
			if len(remainingArgs) < 3 {
				return nil, fmt.Errorf("missing chart argument for install --generate-name")
			}
			opts.name = ""
			opts.chart = remainingArgs[2]
		} else {
			if len(remainingArgs) < 4 {
				return nil, fmt.Errorf("missing required arguments for install")
			}
			opts.name = remainingArgs[2]
			opts.chart = remainingArgs[3]
		}
	} else if opts.op == "upgrade" {
		if len(remainingArgs) < 4 {
			return nil, fmt.Errorf("missing required arguments for upgrade")
		}
		opts.name = remainingArgs[2]
		opts.chart = remainingArgs[3]
	} else {
		return nil, fmt.Errorf("invalid operation: %s", opts.op)
	}

	return opts, nil
}
