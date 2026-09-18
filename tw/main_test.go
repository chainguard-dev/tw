package main_test

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/chainguard-dev/clog"
	"github.com/chainguard-dev/tw/pkg/commands/compilereqs"
	"github.com/chainguard-dev/tw/pkg/commands/dgrep"
	"github.com/chainguard-dev/tw/pkg/commands/kgrep"
	"github.com/chainguard-dev/tw/pkg/commands/kimages"
	"github.com/chainguard-dev/tw/pkg/commands/sfuzz"
	"github.com/chainguard-dev/tw/pkg/commands/shu"
	"github.com/chainguard-dev/tw/pkg/commands/wassert"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/spf13/cobra"
)

var (
	script = flag.String("script", "", "path to script to run, will take precedence over dir")
	dir    = flag.String("dir", "", "path to directory with scripts to run")
	update = flag.Bool("update", false, "update relevant golden files")
)

var cmds = map[string]*cobra.Command{
	"compilereqs": compilereqs.Command(),
	"dgrep":       dgrep.Command(),
	"sfuzz":       sfuzz.Command(),
	"kgrep":       kgrep.Command(),
	"kimages":     kimages.Command(),
	"wassert":     wassert.Command(),
	"shu":         shu.Command(),
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestScript(t *testing.T) {
	files := []string{}
	if *script != "" {
		files = append(files, *script)
	} else if *dir != "" {
	} else {
		clog.FatalContext(context.Background(), "script or dir flag is required")
		os.Exit(1)
	}

	// All tests spawned will share the same parent context from the test
	ctx := t.Context()

	tscmds := map[string]func(ts *testscript.TestScript, neg bool, args []string){}
	for n, cmd := range cmds {
		tscmds[n] = RegisterCmd(ctx, cmd)
	}

	testscript.Run(t, testscript.Params{
		Files:         files,
		Dir:           *dir,
		UpdateScripts: *update,
		Cmds:          tscmds,
	})
}

func RegisterCmd(ctx context.Context, cmd *cobra.Command) func(ts *testscript.TestScript, neg bool, args []string) {
	return func(ts *testscript.TestScript, neg bool, args []string) {
		ctx = clog.WithLogger(ctx, clog.New(NewTestScriptLogger(ts).Handler()))
		cmd.SetArgs(args)
		cmd.SetOut(ts.Stdout())
		cmd.SetErr(ts.Stderr())

		// Throw an error only if the command is expected to fail
		err := cmd.ExecuteContext(ctx)
		if neg {
			if err == nil {
				ts.Fatalf("expected command to fail but it didn't")
			}
		} else {
			if err != nil {
				ts.Fatalf("failed to execute command: %v", err)
			}
		}
	}
}

type TestScriptHandler struct {
	ts *testscript.TestScript
}

func NewTestScriptLogger(ts *testscript.TestScript) *slog.Logger {
	return slog.New(&TestScriptHandler{ts: ts})
}

func (h *TestScriptHandler) Handle(ctx context.Context, rec slog.Record) error {
	var sb strings.Builder
	sb.WriteString(rec.Message)

	rec.Attrs(func(a slog.Attr) bool {
		sb.WriteString(fmt.Sprintf(" %s=%v", a.Key, a.Value))
		return true
	})

	_, err := fmt.Fprintf(h.ts.Stderr(), "%s\n", sb.String())
	return err
}

func (h *TestScriptHandler) WithAttrs(attrs []slog.Attr) slog.Handler           { return h }
func (h *TestScriptHandler) WithGroup(name string) slog.Handler                 { return h }
func (h *TestScriptHandler) Enabled(ctx context.Context, level slog.Level) bool { return true }
