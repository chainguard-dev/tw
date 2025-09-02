package kgrep

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

const (
	DefaultTimeout = 5 * time.Second
)

// Common error patterns for --default flag
var commonErrorPatterns = []string{
	"ERROR",
	"FATAL",
	"FAIL",
	"Exception",
	"panic",
	"Traceback",
	"command not found",
	"NullPointerException",
	"RuntimeException",
	"TimeoutException",
	"OutOfMemoryError",
	"StackOverflow",
	"segmentation fault",
}

type cfg struct {
	Name        string
	Namespace   string
	Timeout     time.Duration
	IgnoreCase  bool
	Container   string
	Retry       int
	Patterns    []string
	NotExpected []string
	NotExpectedExclude []string
	InvertMatch bool
	DefaultErrors bool

	names              []string
	compiled           []*regexp.Regexp
	notExpectedCompiled []*regexp.Regexp
	highlighter        func(string) string
}

func Command() *cobra.Command {
	cfg := &cfg{}

	cmd := &cobra.Command{
		Use:          "kgrep RESOURCE [PATTERN]",
		Short:        "Simple kubernetes pod grepping",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return cfg.prerun(cmd.Context(), args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd)
		},
	}

	cmd.Flags().StringVarP(&cfg.Namespace, "namespace", "n", "default", "namespace to install the release into")
	cmd.Flags().DurationVarP(&cfg.Timeout, "timeout", "t", DefaultTimeout, "time to wait for logs to appear")
	cmd.Flags().IntVarP(&cfg.Retry, "retry", "r", 0, "number of times to retry a failed request")
	cmd.Flags().BoolVarP(&cfg.IgnoreCase, "ignore-case", "i", false, "toggle to ignore case for the match")
	cmd.Flags().StringVarP(&cfg.Container, "container", "c", "", "container to grep logs from (if not specified, will search in all)")
	cmd.Flags().StringArrayVarP(&cfg.Patterns, "regexp", "e", nil, "regular expression to match that must be present")
	cmd.Flags().StringArrayVar(&cfg.NotExpected, "ne", nil, "regular expression that must NOT be present")
	cmd.Flags().StringArrayVar(&cfg.NotExpectedExclude, "ne-exclude", nil, "exclude specific patterns from --default (only works with --default)")
	cmd.Flags().BoolVar(&cfg.DefaultErrors, "default", false, fmt.Sprintf("add default %d common error patterns", len(commonErrorPatterns)))
	cmd.Flags().BoolVarP(&cfg.InvertMatch, "invert-match", "v", false, "toggle to invert the match")

	return cmd
}

func (c *cfg) Run(cmd *cobra.Command) error {
	ctx := cmd.Context()

	l := clog.FromContext(ctx).With("resource", c.names, "namespace", c.Namespace)

	attempt := 0
	err := wait.ExponentialBackoffWithContext(ctx, wait.Backoff{
		Steps:    c.Retry + 1,
		Duration: c.Timeout,
		Factor:   1.0, // Keep backoff linear
	}, func(ctx context.Context) (bool, error) {
		attempt++

		if err := c.retryableRun(ctx); err != nil {
			l.ErrorContextf(ctx, "[%d/%d] failed to run kgrep: %v", attempt, c.Retry+1, err)
			return false, nil
		}

		l.InfoContext(ctx, "kgrep succeeded", "attempt", attempt, "timeout", c.Timeout)
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("kgrep failed after %d attempt(s)", attempt)
	}

	return nil
}

func (c *cfg) retryableRun(ctx context.Context) error {
	getter := genericclioptions.NewConfigFlags(false)

	infos, err := util.NewFactory(getter).NewBuilder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		NamespaceParam(c.Namespace).
		DefaultNamespace().
		SingleResourceType().
		ResourceNames(c.names[0], c.names[1:]...).
		Do().Infos()
	if err != nil {
		return fmt.Errorf("failed to get infos: %v", err)
	}

	if len(infos) != 1 {
		return fmt.Errorf("expected 1 info, got %d", len(infos))
	}

	lopts := &corev1.PodLogOptions{}
	if c.Container != "" {
		lopts.Container = c.Container
	}
	lall := lopts.Container == ""

	reqs, err := polymorphichelpers.LogsForObjectFn(getter, infos[0].Object, lopts, 10*time.Second, lall)
	if err != nil {
		return fmt.Errorf("failed to get logs: %v", err)
	}

	matches := []match{}
	matchedPatterns := make(map[int]bool)
	notExpectedMatches := []match{}
	
	for obj, req := range reqs {
		stream, err := req.Stream(ctx)
		if err != nil {
			return fmt.Errorf("failed to stream logs: %v", err)
		}
		defer stream.Close()

		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := scanner.Text()
			
			// Check expected patterns
			for i, re := range c.compiled {
				if re.MatchString(line) {
					matches = append(matches, match{
						Name:      obj.Name,
						Namespace: obj.Namespace,
						Text:      re.ReplaceAllStringFunc(line, c.highlighter),
					})
					matchedPatterns[i] = true
				}
			}
			
			// Check not-expected patterns
			for _, re := range c.notExpectedCompiled {
				if re.MatchString(line) {
					notExpectedMatches = append(notExpectedMatches, match{
						Name:      obj.Name,
						Namespace: obj.Namespace,
						Text:      re.ReplaceAllStringFunc(line, c.highlighter),
					})
				}
			}
		}
	}

	nmatches := len(matches)
	nNotExpected := len(notExpectedMatches)
	
	clog.InfoContextf(ctx, "found %d expected matches in %s", nmatches, infos[0].String())
	for i, m := range matches {
		clog.InfoContextf(ctx, "-- [%d/%d] expected in %s/%s: %s", i+1, nmatches, m.Name, m.Namespace, m.Text)
	}

	if nNotExpected > 0 {
		clog.InfoContextf(ctx, "found %d not-expected matches in %s", nNotExpected, infos[0].String())
		for i, m := range notExpectedMatches {
			clog.InfoContextf(ctx, "-- [%d/%d] not-expected in %s/%s: %s", i+1, nNotExpected, m.Name, m.Namespace, m.Text)
		}
	}


	if c.InvertMatch && nmatches > 0 {
		return fmt.Errorf("found %d unwanted matches in %s", nmatches, infos[0].String())
	}

	// Fail if any not-expected patterns were found
	if nNotExpected > 0 {
		return fmt.Errorf("found %d not-expected matches in %s", nNotExpected, infos[0].String())
	}

	// Check if all expected patterns were matched
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
	c.names = strings.Split(args[0], "/")

	// Validate --ne-exclude requires --default
	if len(c.NotExpectedExclude) > 0 && !c.DefaultErrors {
		return fmt.Errorf("--ne-exclude can only be used with --default")
	}

	// Add default error patterns if --default is specified
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

	// Compile expected patterns
	for _, p := range c.Patterns {
		if c.IgnoreCase {
			p = "(?i)" + p
		}
		c.compiled = append(c.compiled, regexp.MustCompile(p))
	}

	// Compile not-expected patterns
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
	Name      string
	Namespace string
	Text      string
}
