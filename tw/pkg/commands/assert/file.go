package assert

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type fileCfg struct {
	File string `json:"file"`

	Files []fileEntry `json:"files"`
}

type fileEntry struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Contains string `json:"contains"`
}

func fileCommand() *cobra.Command {
	cfg := &fileCfg{}

	cmd := &cobra.Command{
		Use:   "file",
		Short: "Assert things about a file on disk",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if cfg.File == "" {
				return nil
			}

			data, err := os.ReadFile(cfg.File)
			if err != nil {
				return err
			}

			return yaml.Unmarshal(data, cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd, args)
		},
	}

	cmd.Flags().StringVarP(&cfg.File, "file", "f", "", "File to read config from")

	return cmd
}

func (c *fileCfg) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	clog.InfoContext(ctx, "wassert file", "file", c.File)

	passes := true
	for _, f := range c.Files {
		err := f.Assert(ctx)
		if err != nil {
			clog.ErrorContext(ctx, "assertion failed", "err", err)
			passes = false
		}
	}

	if !passes {
		return fmt.Errorf("file assertion failed")
	}

	clog.InfoContext(ctx, "all assertions passed")

	return nil
}

func (e *fileEntry) Assert(ctx context.Context) error {
	if e.Exists {
		clog.InfoContext(ctx, "exists", "path", e.Path)

		_, err := os.Stat(e.Path)
		if err != nil {
			return &fileAssertError{msg: fmt.Sprintf("file %s does not exist", e.Path)}
		}
	}

	if e.Contains != "" {
		clog.InfoContext(ctx, "contains", "path", e.Path, "contains", e.Contains)

		fn := func() error {
			f, err := os.Open(e.Path)
			if err != nil {
				return &fileAssertError{msg: fmt.Sprintf("failed to open file %s", e.Path)}
			}
			defer f.Close()

			expects := strings.Split(strings.TrimSpace(e.Contains), "\n")

			// at least one line must match at least one expect
			matched := false

			scanner := bufio.NewScanner(f)
			i := 0
			for scanner.Scan() {
				line := scanner.Text()
				i++

				if lineMatches(line, expects...) {
					clog.InfoContext(ctx, "found line match", "line", line, "expect", expects, "line_number", i)
					matched = true
					break
				}

				if matched {
					break
				}
			}

			if !matched {
				return &fileAssertError{msg: fmt.Sprintf("file %s does not contain any of %s", e.Path, e.Contains)}
			}

			return nil
		}

		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}

type fileAssertError struct {
	msg string
}

func (e *fileAssertError) Error() string {
	return e.msg
}
