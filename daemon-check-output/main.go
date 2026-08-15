// daemon-check-output starts a daemon, waits for expected log output, optionally
// runs setup/post scripts, then terminates the daemon and reports the result.
//
// It is the tool behind the test/tw/daemon-check-output melange pipeline. The
// pipeline is a thin wrapper that writes each melange input to a file (via quoted
// heredocs, so no shell expansion of user content) and passes the paths here.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const progName = "daemon-check-output"

func infof(format string, a ...any) {
	fmt.Printf("INFO[%s]: %s\n", progName, fmt.Sprintf(format, a...))
}

type options struct {
	timeout      int
	startFile    string
	expected     string
	errorStr     string
	setupFile    string
	postFile     string
	checkURLFile string
}

// urlCheck is one declarative HTTP check performed after the daemon is ready.
type urlCheck struct {
	URL      string            `yaml:"url"`
	Method   string            `yaml:"method"`
	Headers  map[string]string `yaml:"headers"`
	Expected urlExpect         `yaml:"expected"`
}

type urlExpect struct {
	Code         int           `yaml:"code"`
	BodyContains stringOrSlice `yaml:"body-contains"`
}

// stringOrSlice accepts either a single YAML scalar or a sequence of scalars.
type stringOrSlice []string

func (s *stringOrSlice) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*s = []string{value.Value}
		return nil
	}
	var arr []string
	if err := value.Decode(&arr); err != nil {
		return err
	}
	*s = arr
	return nil
}

func main() {
	o := options{}
	flag.IntVar(&o.timeout, "timeout", 30, "seconds to wait for expected output")
	flag.StringVar(&o.startFile, "start-file", "", "file containing the daemon start command (required)")
	flag.StringVar(&o.expected, "expected-file", "", "file with newline-separated regex patterns that must all appear (required)")
	flag.StringVar(&o.errorStr, "error-strings-file", "", "file with newline-separated regex patterns that indicate failure")
	flag.StringVar(&o.setupFile, "setup-file", "", "file with setup commands to run before starting the daemon")
	flag.StringVar(&o.postFile, "post-file", "", "file with commands to run after the daemon is ready")
	flag.StringVar(&o.checkURLFile, "check-url-file", "", "YAML file describing declarative HTTP checks to run after the daemon is ready")
	flag.Parse()

	os.Exit(execute(o))
}

func execute(o options) int {
	start, err := readTrim(o.startFile)
	if err != nil || start == "" {
		infof("ERROR: a non-empty --start-file is required (%v)", err)
		return 2
	}

	expected, err := readPatterns(o.expected)
	if err != nil {
		infof("ERROR: reading --expected-file: %v", err)
		return 2
	}
	expectedRes, err := compileAll(expected)
	if err != nil {
		infof("ERROR: %v", err)
		return 2
	}

	errPats, err := readPatterns(o.errorStr)
	if err != nil {
		infof("ERROR: reading --error-strings-file: %v", err)
		return 2
	}
	errRes, err := compileAll(errPats)
	if err != nil {
		infof("ERROR: %v", err)
		return 2
	}

	tmpd, err := os.MkdirTemp("", progName)
	if err != nil {
		infof("ERROR: failed to create tmpdir: %v", err)
		return 2
	}
	defer os.RemoveAll(tmpd)

	// setup runs before the daemon and aborts on failure.
	if setup, _ := readTrim(o.setupFile); setup != "" {
		infof("running setup")
		if code, err := runScript(tmpd, "setup", setup); err != nil {
			infof("ERROR: setup failed with %d", code)
			return code
		}
	}

	outPath := filepath.Join(tmpd, "out")
	outF, err := os.Create(outPath)
	if err != nil {
		infof("ERROR: failed to create output file: %v", err)
		return 2
	}
	defer outF.Close()

	devnull, err := os.Open(os.DevNull)
	if err != nil {
		infof("ERROR: failed to open %s: %v", os.DevNull, err)
		return 2
	}
	defer devnull.Close()

	// Run the start command through "sh -c" so quoting/env/args behave as a
	// shell command line. The content is a single, discrete argument -- it is
	// never concatenated into a larger shell string.
	cmd := exec.Command("/bin/sh", "-c", start)
	cmd.Stdout = outF
	cmd.Stderr = outF
	cmd.Stdin = devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		infof("ERROR: failed to start daemon: %v", err)
		return 2
	}
	pid := cmd.Process.Pid
	infof("daemon started as pid %d with: %s", pid, start)

	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

	infof("looking for %d expected pattern(s) within %d seconds", len(expected), o.timeout)
	matched := make([]bool, len(expectedRes))
	began := time.Now()
	secs := func() int { return int(time.Since(began).Seconds()) }
	scan := func() {
		out := readAll(outPath)
		for i, re := range expectedRes {
			if !matched[i] && re.MatchString(out) {
				matched[i] = true
				infof("found within %d seconds: %s", secs(), expected[i])
			}
		}
	}
	allMatched := func() bool {
		for _, m := range matched {
			if !m {
				return false
			}
		}
		return true
	}

	for {
		scan()
		if allMatched() {
			break
		}
		select {
		case <-done:
			infof("daemon pid %d exited after %d seconds", pid, secs())
			scan()
		default:
			if time.Since(began) >= time.Duration(o.timeout)*time.Second {
				infof("timeout %d seconds reached.", o.timeout)
			} else {
				time.Sleep(time.Second)
				continue
			}
		}
		break
	}

	found := 0
	var missing []string
	for i, m := range matched {
		if m {
			found++
		} else {
			missing = append(missing, expected[i])
		}
	}

	// Scan for error strings.
	var errFound []string
	if len(errRes) == 0 {
		infof("error string checking is disabled.")
	} else {
		out := readAll(outPath)
		for i, re := range errRes {
			if re.MatchString(out) {
				errFound = append(errFound, errPats[i])
				infof("ERROR: found error string '%s' in output", errPats[i])
			}
		}
	}

	// post runs after readiness; its failure is recorded but does not skip cleanup.
	postFailed, postCode := false, 0
	if post, _ := readTrim(o.postFile); post != "" {
		infof("running post")
		if code, err := runScript(tmpd, "post", post); err != nil {
			postFailed, postCode = true, code
			infof("ERROR: post failed with %d", code)
		}
	}

	// check-url runs declarative HTTP checks against the ready daemon.
	var urlFails []string
	if checks, err := parseChecks(o.checkURLFile); err != nil {
		infof("ERROR: parsing --check-url-file: %v", err)
		urlFails = append(urlFails, fmt.Sprintf("invalid check-url: %v", err))
	} else if len(checks) > 0 {
		urlFails = runURLChecks(checks, time.Duration(o.timeout)*time.Second)
	}

	infof("-- begin output --")
	for _, line := range strings.Split(strings.TrimRight(readAll(outPath), "\n"), "\n") {
		fmt.Printf("> %s\n", line)
	}
	infof("-- end   output --")

	if found != len(expected) {
		infof("ERROR: found %d of %d expected pattern(s) in output. missing:", found, len(expected))
		for _, m := range missing {
			fmt.Printf("> %s\n", m)
		}
	} else {
		infof("found %d of %d expected pattern(s) in output.", found, len(expected))
	}
	if len(errFound) > 0 {
		infof("ERROR: matched %d error string(s) in output.", len(errFound))
	} else if len(errRes) > 0 {
		infof("found 0 / %d error string(s) in output.", len(errRes))
	}
	if len(urlFails) > 0 {
		infof("ERROR: %d url check(s) failed:", len(urlFails))
		for _, f := range urlFails {
			fmt.Printf("> %s\n", f)
		}
	}

	rc := 0
	if postFailed {
		rc = postCode
	}
	if found != len(expected) || len(errFound) > 0 || len(urlFails) > 0 {
		rc = 1
	}

	termWaitKill(pid, done, 30*time.Second)
	return rc
}

// termWaitKill sends SIGTERM to the daemon's process group, waits up to killWait
// for it to exit, then sends SIGKILL. Signaling the group (not just the direct
// child) catches daemons that fork workers.
func termWaitKill(pid int, done <-chan struct{}, killWait time.Duration) {
	select {
	case <-done:
		return
	default:
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	infof("SIGTERM sent to pgid %d", pid)
	select {
	case <-done:
		return
	case <-time.After(killWait):
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	infof("SIGKILL sent to pgid %d", pid)
	<-done
}

// runScript writes content to a script (prepending "#!/bin/sh -ex" if it has no
// shebang), makes it executable, and runs it. Returns the exit code and error.
func runScript(dir, name, content string) (int, error) {
	body := content
	if !strings.HasPrefix(body, "#!") {
		body = "#!/bin/sh -ex\n" + body
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body+"\n"), 0o755); err != nil {
		return 1, err
	}
	cmd := exec.Command(p)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), err
		}
		return 1, err
	}
	return 0, nil
}

// parseChecks reads the --check-url-file YAML, accepting either a single check
// mapping or a sequence of them.
func parseChecks(path string) ([]urlCheck, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(b)) == "" {
		return nil, nil
	}
	var checks []urlCheck
	if err := yaml.Unmarshal(b, &checks); err != nil {
		// Fall back to a single mapping.
		var single urlCheck
		if err2 := yaml.Unmarshal(b, &single); err2 != nil {
			return nil, err
		}
		checks = []urlCheck{single}
	}
	return checks, nil
}

func runURLChecks(checks []urlCheck, timeout time.Duration) []string {
	var fails []string
	for _, c := range checks {
		if err := runOneURLCheck(c, timeout); err != nil {
			infof("ERROR: check-url %s failed: %v", c.URL, err)
			fails = append(fails, fmt.Sprintf("%s: %v", c.URL, err))
		} else {
			infof("check-url %s ok", c.URL)
		}
	}
	return fails
}

// runOneURLCheck retries the check once per second until it passes or timeout
// elapses, so a port that is still coming up (connection refused) is tolerated.
func runOneURLCheck(c urlCheck, timeout time.Duration) error {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid or unsupported url %q", c.URL)
	}
	method := c.Method
	if method == "" {
		method = http.MethodGet
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lastErr = attemptURL(method, c)
		if lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(time.Second)
	}
}

func attemptURL(method string, c urlCheck) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.URL, nil)
	if err != nil {
		return err
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if c.Expected.Code != 0 {
		if resp.StatusCode != c.Expected.Code {
			return fmt.Errorf("status %d, want %d", resp.StatusCode, c.Expected.Code)
		}
	} else if resp.StatusCode >= 400 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	for _, sub := range c.Expected.BodyContains {
		if !strings.Contains(string(body), sub) {
			return fmt.Errorf("body does not contain %q", sub)
		}
	}
	return nil
}

func readTrim(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// readPatterns returns the non-empty, whitespace-trimmed lines of a file. This
// matches the shell `while read line` behavior of the original pipeline.
func readPatterns(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, ln := range strings.Split(string(b), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
		}
	}
	return out, nil
}

func compileAll(pats []string) ([]*regexp.Regexp, error) {
	res := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", p, err)
		}
		res = append(res, re)
	}
	return res, nil
}

func readAll(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}
