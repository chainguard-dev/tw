package shu

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// listen returns the address of a port that accepts connections.
func listen(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

// closedPort returns the address of a port that refuses connections.
func closedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func TestWaitRun(t *testing.T) {
	tests := []struct {
		name          string
		targets       []string
		errorContains string
	}{
		{
			name:          "no targets returns error",
			errorContains: "at least one --tcp",
		},
		{
			name:    "listening port succeeds",
			targets: []string{listen(t)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			cfg := waitCfg{
				Timeout:    time.Second,
				Interval:   time.Millisecond,
				TCPTargets: tt.targets,
			}
			err := cfg.Run(cmd, nil)
			if tt.errorContains != "" {
				require.ErrorContains(t, err, tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWaitTimeout(t *testing.T) {
	addr := listen(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		timeout time.Duration
		target  string
		ctx     context.Context
		wantErr error
	}{
		{
			name:   "zero timeout allows a connection",
			target: addr,
		},
		{
			name:    "negative timeout expires immediately",
			timeout: -time.Second,
			target:  addr,
			wantErr: context.DeadlineExceeded,
		},
		{
			name:    "positive timeout expires",
			timeout: 20 * time.Millisecond,
			target:  closedPort(t),
			wantErr: context.DeadlineExceeded,
		},
		{
			name:    "zero timeout respects parent cancellation",
			target:  addr,
			ctx:     cancelled,
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			cfg := waitCfg{
				Timeout:    tt.timeout,
				Interval:   time.Millisecond,
				TCPTargets: []string{tt.target},
			}
			err := cfg.Run(cmd, nil)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
