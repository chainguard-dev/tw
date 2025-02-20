package shu

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type waitCfg struct {
	Timeout     time.Duration
	Interval    time.Duration
	DialTimeout time.Duration
	TCPTargets  []string
}

func waitCommand() *cobra.Command {
	cfg := &waitCfg{}

	cmd := &cobra.Command{
		Use:   "wait --tcp host:port [--tcp host:port]... [-- command...]",
		Short: "Wait for services to respond to TCP connections before running a command",
		Example: `  # Wait for a single service
  shu wait --tcp localhost:8080

  # Wait for multiple services
  shu wait --tcp redis:6379 --tcp postgres:5432

  # Wait and then run a command
  shu wait --tcp localhost:8080 -- echo "Service is up!"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd, args)
		},
	}

	cmd.Flags().DurationVarP(&cfg.Timeout, "timeout", "t", 30*time.Second, "Timeout for the wait (0 for no timeout)")
	cmd.Flags().DurationVarP(&cfg.DialTimeout, "dial-timeout", "d", 5*time.Second, "Timeout for each dial attempt")
	cmd.Flags().DurationVarP(&cfg.Interval, "interval", "i", 1*time.Second, "Time between connection attempts")
	cmd.Flags().StringArrayVarP(&cfg.TCPTargets, "tcp", "", nil, "TCP target host:port to wait for (can be specified multiple times)")

	return cmd
}

func (c *waitCfg) Run(cmd *cobra.Command, args []string) error {
	if len(c.TCPTargets) == 0 {
		return fmt.Errorf("at least one --tcp must be specified")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), c.Timeout)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)
	for _, target := range c.TCPTargets {
		t := target
		g.Go(func() error {
			return c.tcpWait(gctx, t)
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	if len(args) == 0 {
		// We're done, just return
		return nil
	}

	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Stdout = cmd.OutOrStdout()
	command.Stderr = cmd.ErrOrStderr()
	command.Env = os.Environ()
	return command.Run()
}

func (c *waitCfg) tcpWait(ctx context.Context, target string) error {
	l := clog.FromContext(ctx).With("target", target)
	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			d := net.Dialer{
				Timeout: c.DialTimeout,
			}

			conn, err := d.DialContext(ctx, "tcp", target)
			if err == nil {
				conn.Close()
				l.InfoContext(ctx, "target is up", "duration", time.Since(start).Round(time.Second))
				return nil
			}

			l.InfoContext(ctx, "target is not yet up, retrying", "duration", time.Since(start).Round(time.Second))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.Interval):
				continue
			}
		}
	}
}
