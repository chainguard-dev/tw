package shu

import (
	"context"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestNewCommand(t *testing.T) {
	shellArgs := []string{"[[ 'x' == 'y' ]] || (echo \"bad\" >&2; exit 10)"} // NOTE: Add `set -x;` at the start to see what's going on.

	c := newCommand(context.Background(), true, shellArgs)

	err := c.Run()
	require.NotNil(t, err) // We're expecting an error here since exit >0

	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok)

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	code := status.ExitStatus()
	require.Equal(t, 10, code)
}

func TestRetryRun(t *testing.T) {
	tests := []struct {
		name          string
		cfg           retryCfg
		args          []string
		expectedError bool
		errorContains string
	}{
		{
			name: "no command returns error",
			cfg: retryCfg{
				Attempts: 1,
				Delay:    1 * time.Millisecond,
				Timeout:  5 * time.Second,
			},
			args:          []string{},
			expectedError: true,
			errorContains: "no command provided",
		},
		{
			name: "successful command",
			cfg: retryCfg{
				Attempts: 1,
				Delay:    1 * time.Millisecond,
				Timeout:  5 * time.Second,
			},
			args:          []string{"true"},
			expectedError: false,
		},
		{
			name: "failing command",
			cfg: retryCfg{
				Attempts: 2,
				Delay:    1 * time.Millisecond,
				Timeout:  5 * time.Second,
			},
			args:          []string{"false"},
			expectedError: true,
		},
		{
			name: "fixed delay flag",
			cfg: retryCfg{
				Attempts:   1,
				Delay:      1 * time.Millisecond,
				Timeout:    5 * time.Second,
				FixedDelay: true,
			},
			args:          []string{"true"},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			err := tt.cfg.Run(cmd, tt.args)

			if tt.expectedError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
