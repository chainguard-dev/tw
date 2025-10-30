package shu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
)

type retryCfg struct {
	Attempts int
	Delay    time.Duration
	Timeout  time.Duration
	// InBash indicates whether the passed command should be run inside Bash.
	InBash     bool
	FixedDelay bool
}

func retryCommand() *cobra.Command {
	cfg := &retryCfg{}

	cmd := &cobra.Command{
		Use: "retry -- <command>",
		Example: `
  retry -a 5 -- curl http://localhost:8080/healthz

  retry -a 5 -b -- "[ $((RANDOM % 5)) -eq 0 ] && exit 0 || exit 10"
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd, args)
		},
	}

	cmd.Flags().IntVarP(&cfg.Attempts, "attempts", "a", 1, "Number of times to retry")
	cmd.Flags().DurationVarP(&cfg.Delay, "delay", "d", 1*time.Second, "Delay between attempts")
	cmd.Flags().DurationVarP(&cfg.Timeout, "timeout", "t", 5*time.Minute, "Timeout for the command")
	cmd.Flags().BoolVarP(&cfg.InBash, "in-bash", "b", false, "Run the passed Bash inside a Bash shell")
	cmd.Flags().BoolVarP(&cfg.FixedDelay, "fixed-delay", "f", false, "Use fixed delay between retries (default is exponential backoff)")

	return cmd
}

func (c *retryCfg) Run(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command provided")
	}

	rawcmd := strings.Join(args, " ")

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	l := clog.FromContext(ctx).With("command", rawcmd)
	l.InfoContext(ctx, "args received", "args", args, "in-bash", c.InBash)

	attempt := 0
	retryOpts := []retry.Option{
		retry.OnRetry(func(attempt uint, err error) {
			l.ErrorContextf(ctx, "[%d/%d] command failed, retrying: %s", attempt, c.Attempts, err)
		}),
		retry.Context(ctx),
		retry.Attempts(uint(c.Attempts)),
		retry.Delay(c.Delay),
	}

	if c.FixedDelay {
		retryOpts = append(retryOpts, retry.DelayType(retry.FixedDelay))
	}

	err := retry.Do(
		func() error {
			attempt++
			l.InfoContextf(ctx, "[%d/%d] attempting command", attempt, c.Attempts)

			command := newCommand(ctx, c.InBash, args)
			command.Stdout = cmd.OutOrStdout()
			command.Stderr = cmd.ErrOrStderr()
			command.Env = os.Environ()

			if err := command.Run(); err != nil {
				return err
			}

			return nil
		},
		retryOpts...,
	)

	return err
}

func newCommand(ctx context.Context, inShell bool, args []string) *exec.Cmd {
	var c *exec.Cmd
	if inShell {
		shellArgs := make([]string, 0, len(args)+1)
		shellArgs = append(shellArgs, "-c")
		shellArgs = append(shellArgs, args...)

		c = exec.CommandContext(ctx, "/bin/bash", shellArgs...)
	} else {
		c = exec.CommandContext(ctx, args[0], args[1:]...)
	}

	return c
}
