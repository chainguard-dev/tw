package dgrep

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	DefaultTimeout = 15 * time.Second
)

// Common error patterns for --std-errors flag
var commonErrorPatterns = []string{
	"ERROR",
	"FATAL",
	"FAIL",
	"Exception",
	"panic",
	"Traceback.*most.recent.call",
	"command not found",
	"java.lang.*Exception",
	"segmentation fault",
	"org.jruby.exceptions",
	"Gem::MissingSpecError",
	"Permission denied",
}

type cfg struct {
	Container      string
	Timeout        time.Duration
	IgnoreCase     bool
	Retry          int
	Patterns       []string
	NotExpected    []string
	NotExpectedExclude []string
	InvertMatch    bool
	DefaultErrors  bool

	compiled           []*regexp.Regexp
	notExpectedCompiled []*regexp.Regexp
	highlighter        func(string) string
}

func Command() *cobra.Command {
	cfg := &cfg{}

	cmd := &cobra.Command{
		Use:          "dgrep CONTAINER [PATTERN]",
		Short:        "Simple docker container log grepping",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return cfg.prerun(cmd.Context(), args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd)
		},
	}

	cmd.Flags().DurationVarP(&cfg.Timeout, "timeout", "t", DefaultTimeout, "time to wait for logs to appear")
	cmd.Flags().IntVarP(&cfg.Retry, "retry", "r", 3, "number of times to retry a failed request")
	cmd.Flags().BoolVarP(&cfg.IgnoreCase, "ignore-case", "i", true, "toggle to ignore case for the match")
	cmd.Flags().StringArrayVarP(&cfg.Patterns, "regexp", "e", nil, "regular expression to match (must be present)")
	cmd.Flags().StringArrayVar(&cfg.NotExpected, "ne", nil, "regular expression that must NOT be present")
	cmd.Flags().StringArrayVar(&cfg.NotExpectedExclude, "ne-exclude", nil, "exclude specific patterns from --std-errors (only works with --std-errors)")
	cmd.Flags().BoolVar(&cfg.DefaultErrors, "std-errors", false, fmt.Sprintf("check for %d standard error patterns", len(commonErrorPatterns)))
	cmd.Flags().BoolVarP(&cfg.InvertMatch, "invert-match", "v", false, "toggle to invert the match")

	return cmd
}

func (c *cfg) Run(cmd *cobra.Command) error {
	ctx := cmd.Context()

	l := clog.FromContext(ctx).With("container", c.Container)

	attempt := 0
	err := wait.ExponentialBackoffWithContext(ctx, wait.Backoff{
		Steps:    c.Retry + 1,
		Duration: c.Timeout,
		Factor:   1.0, // Keep backoff linear
	}, func(ctx context.Context) (bool, error) {
		attempt++

		if err := c.retryableRun(ctx); err != nil {
			l.ErrorContextf(ctx, "[%d/%d] failed to run dgrep: %v", attempt, c.Retry+1, err)
			return false, nil
		}

		l.InfoContext(ctx, "dgrep succeeded", "attempt", attempt, "timeout", c.Timeout)
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("dgrep failed after %d attempt(s)", attempt)
	}

	return nil
}

func (c *cfg) retryableRun(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation(), client.WithTimeout(DefaultTimeout))
	if err != nil {
		return fmt.Errorf("failed to create docker client: %v", err)
	}
	defer cli.Close()

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
		Timestamps: true,
	}

	// Set a timeout for the context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	reader, err := cli.ContainerLogs(ctx, c.Container, options)
	if err != nil {
		return fmt.Errorf("failed to get container logs: %v", err)
	}
	defer reader.Close()

	matches := []match{}
	matchedPatterns := make(map[int]bool)
	notExpectedMatches := []match{}

	// Use stdcopy to properly handle Docker's multiplexed stream format
	var stdoutBuf, stderrBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, reader); err != nil && err != io.EOF {
		return fmt.Errorf("error reading container logs: %v", err)
	}

	// Process stdout
	scanner := bufio.NewScanner(strings.NewReader(stdoutBuf.String()))
	for scanner.Scan() {
		line := scanner.Text()
		
		// Check expected patterns
		for i, re := range c.compiled {
			if re.MatchString(line) {
				matches = append(matches, match{
					Container: c.Container,
					Text:      re.ReplaceAllStringFunc(line, c.highlighter),
				})
				matchedPatterns[i] = true
				break
			}
		}
		
		// Check not-expected patterns
		for _, re := range c.notExpectedCompiled {
			if re.MatchString(line) {
				notExpectedMatches = append(notExpectedMatches, match{
					Container: c.Container,
					Text:      re.ReplaceAllStringFunc(line, c.highlighter),
				})
			}
		}
	}

	// Process stderr
	scanner = bufio.NewScanner(strings.NewReader(stderrBuf.String()))
	for scanner.Scan() {
		line := scanner.Text()
		
		// Check expected patterns
		for i, re := range c.compiled {
			if re.MatchString(line) {
				matches = append(matches, match{
					Container: c.Container,
					Text:      re.ReplaceAllStringFunc(line, c.highlighter),
				})
				matchedPatterns[i] = true
			}
		}
		
		// Check not-expected patterns
		for _, re := range c.notExpectedCompiled {
			if re.MatchString(line) {
				notExpectedMatches = append(notExpectedMatches, match{
					Container: c.Container,
					Text:      re.ReplaceAllStringFunc(line, c.highlighter),
				})
			}
		}
	}

	// Print all matches at the end
	nmatches := len(matches)
	nNotExpected := len(notExpectedMatches)
	
	clog.InfoContextf(ctx, "found %d expected matches in container %s", nmatches, c.Container)
	for i, m := range matches {
		clog.InfoContextf(ctx, "-- [%d/%d] expected in %s: %s", i+1, nmatches, m.Container, m.Text)
	}

	if nNotExpected > 0 {
		clog.InfoContextf(ctx, "found %d not-expected matches in container %s", nNotExpected, c.Container)
		for i, m := range notExpectedMatches {
			clog.InfoContextf(ctx, "-- [%d/%d] not-expected in %s: %s", i+1, nNotExpected, m.Container, m.Text)
		}
	}

	if c.InvertMatch && nmatches > 0 {
		return fmt.Errorf("found %d unwanted matches in container %s", nmatches, c.Container)
	}

	// Fail if any not-expected patterns were found
	if nNotExpected > 0 {
		return fmt.Errorf("found %d not-expected matches in container %s", nNotExpected, c.Container)
	}

	// Check if all expected patterns were matched (only if not using invert match)
	if !c.InvertMatch && len(c.Patterns) > 0 {
		if len(matchedPatterns) < len(c.compiled) {
			// Find which patterns were not matched
			var missingPatterns []string
			for i, pattern := range c.Patterns {
				if !matchedPatterns[i] {
					missingPatterns = append(missingPatterns, pattern)
				}
			}
			return fmt.Errorf("no match found for expected pattern(s): %v", missingPatterns)
		}
	}

	return nil
}

func (c *cfg) prerun(_ context.Context, args []string) error {
	c.Container = args[0]

	// Validate --ne-exclude requires --std-errors
	if len(c.NotExpectedExclude) > 0 && !c.DefaultErrors {
		return fmt.Errorf("--ne-exclude can only be used with --std-errors")
	}

	// Add default error patterns if --std-errors is specified
	if c.DefaultErrors {
		// Start with all default patterns
		patterns := make([]string, len(commonErrorPatterns))
		copy(patterns, commonErrorPatterns)
		
		// Remove excluded patterns
		for _, exclude := range c.NotExpectedExclude {
			filtered := []string{}
			for _, pattern := range patterns {
				if pattern != exclude {
					filtered = append(filtered, pattern)
				}
			}
			patterns = filtered
		}
		
		// Add defaults (after exclusions) to not-expected patterns
		c.NotExpected = append(c.NotExpected, patterns...)
	}

	if len(c.Patterns) == 0 && len(c.NotExpected) == 0 {
		return fmt.Errorf("expected at least one pattern via -e/--regexp or --ne")
	}

	// Check for conflicting patterns (same pattern in both -e and --ne)
	for _, expected := range c.Patterns {
		if slices.Contains(c.NotExpected, expected) {
			return fmt.Errorf("conflicting pattern '%s' found in both -e and --ne flags", expected)
		}
	}

	for _, p := range c.Patterns {
		if c.IgnoreCase {
			p = "(?i)" + p
		}
		c.compiled = append(c.compiled, regexp.MustCompile(p))
	}

	for _, p := range c.NotExpected {
		if c.IgnoreCase {
			p = "(?i)" + p
		}
		c.notExpectedCompiled = append(c.notExpectedCompiled, regexp.MustCompile(p))
	}

	c.highlighter = func(s string) string {
		if isatty.IsTerminal(os.Stdout.Fd()) {
			return "\x1b[32;1m" + s + "\x1b[0m"
		}
		return "( " + s + " )"
	}
	return nil
}

type match struct {
	Container string
	Text      string
}
