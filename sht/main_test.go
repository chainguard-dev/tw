package main_test

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/chainguard-dev/sht/internal/control"
	"golang.org/x/sys/unix"
	"mvdan.cc/sh/v3/syntax"
)

func TestScript(t *testing.T) {
	ctx := t.Context()

	if len(flag.Args()) == 0 {
		fmt.Println("no script path provided")
		os.Exit(1)
	}

	scriptPath := flag.Args()[0]

	r, err := NewRunner(scriptPath)
	if err != nil {
		t.Fatal(err)
	}

	t.Run(r.Name, func(t *testing.T) {
		if err := r.Run(ctx, t); err != nil {
			t.Fatal(err)
		}
	})
}

type Runner struct {
	Name           string
	ScriptPath     string
	TestFns        map[string]*TestFn
	OrderedTestFns []string
}

func NewRunner(scriptPath string) (*Runner, error) {
	r := &Runner{
		ScriptPath:     scriptPath,
		TestFns:        make(map[string]*TestFn),
		OrderedTestFns: make([]string, 0),
	}

	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, err
	}

	r.Name = filepath.Base(scriptPath)
	prog, err := syntax.NewParser().Parse(bytes.NewBuffer(raw), r.Name)
	if err != nil {
		return nil, err
	}

	var tfnerr error
	syntax.Walk(prog, func(n syntax.Node) bool {
		if n == nil {
			return true
		}

		if fn, ok := n.(*syntax.FuncDecl); ok {
			if strings.HasPrefix(fn.Name.Value, "shtest_") {
				tfn, err := NewTestFn(fn)
				if err != nil {
					tfnerr = err
					return false
				}
				r.TestFns[fn.Name.Value] = tfn
				r.OrderedTestFns = append(r.OrderedTestFns, fn.Name.Value)
			}
		}
		return true
	})
	if tfnerr != nil {
		return nil, tfnerr
	}

	return r, nil
}

func (r *Runner) Run(ctx context.Context, t *testing.T) error {
	wpath, err := r.render()
	if err != nil {
		return err
	}

	cs := control.NewServer(func(msg control.Message) error {
		switch msg.Command {
		case control.CommandStart:
			tfn, ok := r.TestFns[msg.TestName]
			if !ok {
				return fmt.Errorf("unknown test: %s", msg.TestName)
			}

			go t.Run(msg.TestName, func(subT *testing.T) {
				if err := tfn.Start(subT); err != nil {
					subT.Errorf("failed to start test: %v", err)
					return
				}

				tfn.Stream()

				tfn.Wait()
			})

			return nil
		case control.CommandStop:
			tfn, ok := r.TestFns[msg.TestName]
			if !ok {
				return fmt.Errorf("unknown test: %s", msg.TestName)
			}
			return tfn.End(msg.ExitCode)
		}

		return fmt.Errorf("unknown command: %s", msg.Command)
	})

	if err := cs.Start(); err != nil {
		return err
	}
	defer cs.Stop()

	cmd := exec.CommandContext(ctx, wpath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SHT_CONTROL_ADDR="+cs.Addr())

	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// Don't re-throw an error if the script failed because of user defined things. The overall program will still fail b/c the individual tests will have failures.
			return nil
		}
		return fmt.Errorf("error running test script: %w", err)
	}

	return nil
}

// render will render the wrapped script
func (r *Runner) render() (string, error) {
	tpl := template.New("framework.sh.tpl").Funcs(template.FuncMap{
		"loadScript": func() (string, error) {
			// load the script and trim the first line (shbang)
			f, err := os.Open(r.ScriptPath)
			if err != nil {
				return "", err
			}
			defer f.Close()

			var buf bytes.Buffer

			s := bufio.NewScanner(f)
			for {
				if !s.Scan() {
					break
				}

				if strings.HasPrefix(s.Text(), "#!") {
					continue
				}

				buf.WriteString(s.Text())
				buf.WriteByte('\n')
			}

			return buf.String(), nil
		},
	})

	var err error
	tpl, err = tpl.ParseFS(frameworkFS, "framework.sh.tpl")
	if err != nil {
		return "", err
	}

	wrapped, err := os.CreateTemp("", "sht-")
	if err != nil {
		return "", err
	}
	defer wrapped.Close()

	if err := tpl.Execute(wrapped, r); err != nil {
		return "", err
	}

	if err := os.Chmod(wrapped.Name(), 0755); err != nil {
		return "", err
	}

	return wrapped.Name(), nil
}

type TestFn struct {
	Name           string
	Status         TestStatus
	ExitCode       int
	StdoutPipePath string
	StderrPipePath string

	socketDir string
	done      chan struct{}
	t         *testing.T
}

func NewTestFn(decl *syntax.FuncDecl) (*TestFn, error) {
	tdir, err := os.MkdirTemp("", "sht-tfn")
	if err != nil {
		return nil, err
	}

	tname := decl.Name.Value

	return &TestFn{
		Name:           tname,
		socketDir:      tdir,
		StdoutPipePath: filepath.Join(tdir, tname+".stdout.pipe"),
		StderrPipePath: filepath.Join(tdir, tname+".stderr.pipe"),
		done:           make(chan struct{}),
	}, nil
}

func (t *TestFn) Start(subT *testing.T) error {
	if t.Status != TestStatusNotStarted {
		return fmt.Errorf("test %s is already started", t.Name)
	}
	t.t = subT

	if err := unix.Mkfifo(t.StdoutPipePath, 0666); err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := unix.Mkfifo(t.StderrPipePath, 0666); err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	t.t.Cleanup(func() {
		_ = os.RemoveAll(t.socketDir)
	})

	t.Status = TestStatusRunning

	return nil
}

func (t *TestFn) End(exitCode int) error {
	if t.Status != TestStatusRunning {
		return fmt.Errorf("test %s is not running", t.Name)
	}

	t.ExitCode = exitCode

	switch t.ExitCode {
	case 0:
		t.Status = TestStatusPassed
		t.t.Logf("-- [%s] finished successfully", t.Name)
	default:
		t.Status = TestStatusFailed
		t.t.Errorf("-- [%s] finished with exit code %d", t.Name, exitCode)
	}

	close(t.done)
	return nil
}

func (t *TestFn) Stream() {
	streamPipe := func(pipePath string, prefix string) {
		f, err := os.OpenFile(pipePath, os.O_RDONLY, 0)
		if err != nil {
			t.t.Logf("opening %s pipe: %v", prefix, err)
			return
		}
		defer f.Close()

		s := bufio.NewScanner(f)
		for s.Scan() {
			t.t.Logf("[%s] %s", prefix, s.Text())
		}

		if err := s.Err(); err != nil {
			t.t.Logf("reading from %s pipe: %v", prefix, err)
		}
	}

	go streamPipe(t.StdoutPipePath, "stdout")
	go streamPipe(t.StderrPipePath, "stderr")
}

func (t *TestFn) Wait() {
	select {
	case <-t.done:
	case <-t.t.Context().Done():
		t.t.Errorf("test %s was cancelled", t.Name)
	}
}

type TestStatus int

const (
	TestStatusNotStarted TestStatus = iota
	TestStatusRunning
	TestStatusPassed
	TestStatusFailed
)

//go:embed framework.sh.tpl
var frameworkFS embed.FS
