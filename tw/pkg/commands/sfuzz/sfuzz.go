//go:build linux
// +build linux

package sfuzz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"chainguard.dev/apko/pkg/apk/apk"
	"github.com/armon/go-radix"
	"github.com/chainguard-dev/clog"
	"github.com/chainguard-dev/tw/pkg/commands/ptrace"
	"github.com/spf13/cobra"
)

const (
	DefaultTimeout = 30 * time.Second
)

var (
	DefaultCommonFlags = []string{"--version", "--help", "version", "-h", "-v", "-version", "-help", "-V"}
	DefaultBinDirs     = []string{"/bin", "/usr/bin", "/usr/local/bin"}
)

type cfg struct {
	Apk            string
	Bins           []string
	Out            string
	Trace          bool
	TraceFSAIgnore []string
}

func Command() *cobra.Command {
	cfg := &cfg{}

	cmd := &cobra.Command{
		Use:   "sfuzz",
		Short: "A really simple, stupid binary fuzzer",
		Args:  cobra.ExactArgs(0),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd, args)
		},
	}

	cmd.Flags().StringVarP(&cfg.Apk, "apk", "a", "", "apk name")
	cmd.Flags().StringSliceVarP(&cfg.Bins, "bin", "b", []string{}, "binaries to 'fuzz'")
	cmd.Flags().StringVarP(&cfg.Out, "out", "o", "sfuzz.out.json", "output file")
	cmd.Flags().BoolVarP(&cfg.Trace, "trace", "t", false, "trace mode")
	cmd.Flags().StringSliceVarP(&cfg.TraceFSAIgnore, "trace-fs-ignore", "i", []string{}, "ignore files with these path prefixes when tracing (e.g., /usr/lib)")

	return cmd
}

func (c *cfg) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	clog.InfoContext(ctx, "running sfuzz", "apk", c.Apk, "bins", c.Bins)

	// collection of commands to fuzz
	commands := []string{}

	if c.Apk != "" {
		clog.InfoContext(ctx, "looking for apk", "apk", c.Apk)

		a, err := apk.New()
		if err != nil {
			return fmt.Errorf("failed to create apk: %v", err)
		}

		pkgs, err := a.GetInstalled()
		if err != nil {
			return fmt.Errorf("failed to get installed packages: %v", err)
		}

		for _, pkg := range pkgs {
			if pkg.Name == c.Apk {
				clog.InfoContext(ctx, "found package", "pkg", pkg.Name)
				for _, f := range pkg.Files {
					p := "/" + f.Name

					ep, err := exec.LookPath(p)
					if err != nil {
						continue
					}

					// if its in a check directory
					for _, dir := range DefaultBinDirs {
						if strings.HasPrefix(ep, dir) {
							clog.InfoContext(ctx, "found executable", "exe", ep)
							commands = append(commands, ep)
							break
						}
					}
				}
			}
		}
	}

	if len(c.Bins) > 0 {
		clog.InfoContext(ctx, "using executables", "bins", c.Bins)
		commands = append(commands, c.Bins...)
	}

	thits := make([]success, 0)
	tfails := make([]error, 0)

	select {
	case <-ctx.Done():
	default:
		for _, cmd := range commands {
			chits, cerrs := c.fuzz(ctx, cmd, DefaultCommonFlags...)
			thits = append(thits, chits...)
			tfails = append(tfails, cerrs...)
		}
	}

	if len(thits) == 0 {
		clog.InfoContext(ctx, "all commands failed")
		for _, failure := range tfails {
			clog.InfoContextf(ctx, "--- %v", failure)
		}
		return fmt.Errorf("")
	}

	clog.InfoContextf(ctx, "found %d successes", len(thits))
	for _, success := range thits {
		clog.InfoContextf(ctx, "command '%s %s' exited with code %d", success.Command, success.Flag, success.ExitCode)
		clog.InfoContextf(ctx, "-- stdout: \n%s", success.stdout)
		clog.InfoContextf(ctx, "-- stderr: \n%v", success.stderr)
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(thits); err != nil {
		return fmt.Errorf("failed to encode json: %v", err)
	}

	return nil
}

type success struct {
	Command       string            `json:"command"`
	ExitCode      int               `json:"exit_code"`
	Flag          string            `json:"flag"`
	FilesAccessed map[string]uint64 `json:"files_accessed,omitempty"`

	stdout string
	stderr string
}

func (c *cfg) fuzz(ctx context.Context, command string, flags ...string) ([]success, []error) {
	var successes []success
	var failures []error

	for _, flag := range flags {
		clog.InfoContextf(ctx, "fuzzing %s %s", command, flag)

		var runner Runner
		if c.Trace {
			runner = &tracer{
				ignore: c.TraceFSAIgnore,
			}
		} else {
			runner = &cmder{}
		}

		hit, err := runner.Run(ctx, command, flag)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		clog.InfoContextf(ctx, "--- [%s]: success hit with flag %q", command, flag)
		successes = append(successes, hit)
	}

	return successes, failures
}

type Runner interface {
	Run(ctx context.Context, args ...string) (success, error)
}

type cmder struct{}

func (c *cmder) Run(ctx context.Context, args ...string) (success, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return success{}, err
	}

	return success{
		ExitCode: cmd.ProcessState.ExitCode(),
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		Command:  args[0],
		Flag:     args[1],
	}, nil
}

type tracer struct {
	ignore []string
}

func (t *tracer) Run(ctx context.Context, args ...string) (success, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var stdout, stderr bytes.Buffer
	topts := ptrace.TracerOpts{
		Args:     args,
		Stdout:   &stdout,
		Stderr:   &stderr,
		SignalCh: make(chan os.Signal, 1),
	}

	pt, err := ptrace.New(args, topts)
	if err != nil {
		return success{}, fmt.Errorf("failed to create tracer: %v", err)
	}

	if err := pt.Start(ctx); err != nil {
		return success{}, fmt.Errorf("failed to start tracer: %v", err)
	}

	report := pt.Wait()

	success := success{
		ExitCode: report.ExitCode,
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		Command:  args[0],
		Flag:     args[1],
	}

	if len(report.FSActivity) > 0 {
		success.FilesAccessed = make(map[string]uint64)

		r := radix.New()
		for _, prefix := range t.ignore {
			r.Insert(prefix, true)
		}

		// Sort paths for consistent output
		paths := make([]string, 0, len(report.FSActivity))
		for path := range report.FSActivity {
			paths = append(paths, path)
		}
		sort.Strings(paths)

		for _, path := range paths {
			_, _, prefixed := r.LongestPrefix(path)
			if prefixed {
				continue
			}

			info := report.FSActivity[path]
			success.FilesAccessed[path] = info.OpsAll
		}
	}

	return success, nil
}
