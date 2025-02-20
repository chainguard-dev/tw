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
}

func retryCommand() *cobra.Command {
	cfg := &retryCfg{}

	cmd := &cobra.Command{
		Use:     "retry -- <command>",
		Example: ``,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd, args)
		},
	}

	cmd.Flags().IntVarP(&cfg.Attempts, "attempts", "a", 1, "Number of times to retry")
	cmd.Flags().DurationVarP(&cfg.Delay, "delay", "d", 1*time.Second, "Delay between attempts")
	cmd.Flags().DurationVarP(&cfg.Timeout, "timeout", "t", 5*time.Minute, "Timeout for the command")

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
	l.InfoContext(ctx, "args received", "args", args)

	attempt := 0
	err := retry.Do(
		func() error {
			attempt++
			l.InfoContextf(ctx, "[%d/%d] attempting command", attempt, c.Attempts)

			command := exec.CommandContext(ctx, args[0], args[1:]...)
			command.Stdout = cmd.OutOrStdout()
			command.Stderr = cmd.ErrOrStderr()
			command.Env = os.Environ()

			if err := command.Run(); err != nil {
				return err
			}

			return nil
		},
		retry.OnRetry(func(attempt uint, err error) {
			l.ErrorContextf(ctx, "[%d/%d] command failed, retrying: %s", attempt, c.Attempts, err)
		}),
		retry.Context(ctx),
		retry.Attempts(uint(c.Attempts)),
		retry.Delay(c.Delay),
	)

	return err
}
