package compilereqs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
)

type cfg struct {
	Package      string
	Version      string
	Dependencies string
	Python       string
	Output       string
	Index        string
}

func Command() *cobra.Command {
	cfg := &cfg{}

	cmd := &cobra.Command{
		Use:   "compilereqs",
		Short: "Compile a locked requirements file for Python packages and bundles",
		Long: `Compile a locked requirements file for Python packages and bundles.

This command uses uv to compile a locked requirements file for Python packages and bundles.
It creates a project with uv, adds the main package and any indirect dependencies to the
project, and exports a locked requirements file. It also, optionally, handles auth to
Chainguard Libraries.

Examples:
  tw compilereqs -p requests -v 2.31.0
  tw compilereqs -p django -v 4.2.0 -d "celery redis"
  tw compilereqs -p flask -v 2.3.0 --python 3.13
  tw compilereqs -p numpy -v 1.24.0 -o requirements.txt
  tw compilereqs -p requests -v 2.31.0 -i https://libraries.cgr.dev/python/simple`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd)
		},
	}

	cmd.Flags().StringVarP(&cfg.Package, "package", "p", "", "Main package to generate requirements around (required)")
	cmd.Flags().StringVarP(&cfg.Version, "version", "v", "", "Version of the main package (required)")
	cmd.Flags().StringVarP(&cfg.Dependencies, "dependencies", "d", "", "Additional dependencies to add (space-separated list)")
	cmd.Flags().StringVarP(&cfg.Python, "python", "P", "", "Python version or path (overrides UV_PYTHON)")
	cmd.Flags().StringVarP(&cfg.Output, "output", "o", "requirements.locked", "Output file path or directory for the locked requirements")
	cmd.Flags().StringVarP(&cfg.Index, "index", "i", "https://libraries.cgr.dev/python/simple", "Python package index URL (overrides UV_DEFAULT_INDEX)")

	cmd.MarkFlagRequired("package")
	cmd.MarkFlagRequired("version")

	return cmd
}

func (c *cfg) Run(cmd *cobra.Command) error {
	ctx := cmd.Context()
	log := clog.FromContext(ctx)

	// Validate that uv is available
	if _, err := exec.LookPath("uv"); err != nil {
		return fmt.Errorf("uv is not installed or not in PATH: %w", err)
	}

	// Login to Chainguard Libraries, if requested
	if strings.HasPrefix(c.Index, "https://libraries.cgr.dev") {
		if err := c.librariesLogin(ctx, cmd); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}
	}

	// Determine output path
	outputPath := c.Output
	if !filepath.IsAbs(outputPath) {
		// If relative path, make it relative to current working directory
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %w", err)
		}
		outputPath = filepath.Join(cwd, outputPath)
	}

	// Only relevant if outputPath is a directory
	outputFile := "requirements.locked"

	// Check if output path is a directory
	if fileInfo, err := os.Stat(outputPath); err == nil && fileInfo.IsDir() {
		// If it's a directory, append requirements.locked
		outputPath = filepath.Join(outputPath, outputFile)
		log.DebugContextf(ctx, "Output path is a directory, using: %s", outputPath)
	}

	// Create tmpdir for project
	projectDir, err := os.MkdirTemp("", "tw-compilereqs-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	// Remove the project's tmpdir when we're done with it
	defer func() {
		if err := os.RemoveAll(projectDir); err != nil {
			log.WarnContextf(ctx, "Failed to remove temporary directory %s: %v", projectDir, err)
		}
	}()

	log.InfoContextf(ctx, "Created project directory: %s", projectDir)

	// Set up environment for uv
	env := os.Environ()
	if c.Python != "" {
		// Override UV_PYTHON if --python is specified
		env = append(env, fmt.Sprintf("UV_PYTHON=%s", c.Python))
		log.InfoContextf(ctx, "Using Python: %s", c.Python)
	}

	// Always override UV_DEFAULT_INDEX
	// Defaults to Chainguard Libraries without fallback.
	// Requires explicitly passing --index to use other indexes.
	env = append(env, fmt.Sprintf("UV_DEFAULT_INDEX=%s", c.Index))
	log.InfoContextf(ctx, "Using index: %s", c.Index)

	// Initialize a uv project in a tmpdir
	log.InfoContextf(ctx, "Initializing uv project in %s", projectDir)
	initCmd := exec.CommandContext(ctx, "uv", "init", "--no-workspace")
	initCmd.Dir = projectDir
	initCmd.Env = env
	initCmd.Stdout = cmd.OutOrStdout()
	initCmd.Stderr = cmd.ErrOrStderr()

	if err := initCmd.Run(); err != nil {
		return fmt.Errorf("failed to initialize uv project: %w", err)
	}

	// Add the main package at the provided version
	// We always require a main package with a hard version constraint as
	// that is the package used the APK version is inferred from
	packageSpec := fmt.Sprintf("%s==%s", c.Package, c.Version)
	log.InfoContextf(ctx, "Adding main package: %s", packageSpec)

	addCmd := exec.CommandContext(ctx, "uv", "add", packageSpec)
	addCmd.Dir = projectDir
	addCmd.Env = env
	addCmd.Stdout = cmd.OutOrStdout()
	addCmd.Stderr = cmd.ErrOrStderr()

	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("failed to add package %s: %w", packageSpec, err)
	}

	// Add additional dependencies, if specified
	// We don't require constraints for these, but maybe we should? I'm
	// hopeful this is the exception rather than something we regularly
	// accomodate
	if c.Dependencies != "" {
		deps := strings.Fields(c.Dependencies)
		for _, dep := range deps {
			log.InfoContextf(ctx, "Adding dependency: %s", dep)

			depCmd := exec.CommandContext(ctx, "uv", "add", dep)
			depCmd.Dir = projectDir
			depCmd.Env = env
			depCmd.Stdout = cmd.OutOrStdout()
			depCmd.Stderr = cmd.ErrOrStderr()

			if err := depCmd.Run(); err != nil {
				return fmt.Errorf("failed to add dependency %s: %w", dep, err)
			}
		}
	}

	// Export the requirements file
	log.InfoContextf(ctx, "Exporting requirements to: %s", outputFile)

	exportCmd := exec.CommandContext(ctx, "uv", "export", "--output-file", outputFile)
	exportCmd.Dir = projectDir
	exportCmd.Env = env
	exportCmd.Stdout = cmd.OutOrStdout()
	exportCmd.Stderr = cmd.ErrOrStderr()

	// Export requirements to requirements.locked
	if err := exportCmd.Run(); err != nil {
		return fmt.Errorf("failed to export requirements: %w", err)
	}

	// Copy requirements.locked to output path
	if err := copyFile(filepath.Join(projectDir, outputFile), outputPath); err != nil {
		return fmt.Errorf("failed to copy requirements to %s: %w", outputPath, err)
	}

	log.InfoContextf(ctx, "Successfully created %s", outputPath)

	return nil
}

func (c *cfg) librariesLogin(ctx context.Context, cmd *cobra.Command) error {
	log := clog.FromContext(ctx)

	// Validate that chainctl is available
	if _, err := exec.LookPath("chainctl"); err != nil {
		return fmt.Errorf("chainctl is not installed or not in PATH: %w", err)
	}

	audience := "libraries.cgr.dev"
	log.InfoContextf(ctx, "Retrieving token from chainctl with audience: %s", audience)

	// Get token with chainctl
	tokenCmd := exec.CommandContext(ctx, "chainctl", "auth", "token", "--audience", audience)
	var tokenBuf bytes.Buffer
	tokenCmd.Stdout = &tokenBuf
	tokenCmd.Stderr = cmd.ErrOrStderr()

	if err := tokenCmd.Run(); err != nil {
		return fmt.Errorf("failed to get token with chainctl: %w", err)
	}

	token := strings.TrimSpace(tokenBuf.String())
	if token == "" {
		return fmt.Errorf("chainctl returned an empty token")
	}

	log.InfoContextf(ctx, "Authenticating to Chainguard Libraries: %s", c.Index)

	// Use the token to login to uv
	loginCmd := exec.CommandContext(ctx, "uv", "auth", "login", "--token", token, c.Index)
	loginCmd.Stdout = cmd.OutOrStdout()
	loginCmd.Stderr = cmd.ErrOrStderr()
	loginCmd.Env = os.Environ()

	if err := loginCmd.Run(); err != nil {
		return fmt.Errorf("failed to login to Chainguard Libraries: %w", err)
	}

	log.InfoContextf(ctx, "Successfully authenticated to %s", c.Index)

	return nil
}

func copyFile(src, dest string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, b, 0o644); err != nil {
		return err
	}
	return nil
}
