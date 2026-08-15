package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFile writes content to <dir>/<name> and returns the path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// opts builds an options with a start command and expected patterns written to
// files under a fresh temp dir. Extra files are added via the returned dir.
func opts(t *testing.T, start, expected string) (options, string) {
	t.Helper()
	dir := t.TempDir()
	return options{
		timeout:   3,
		startFile: writeFile(t, dir, "start", start),
		expected:  writeFile(t, dir, "expected", expected),
	}, dir
}

func TestAllPatternsFound(t *testing.T) {
	o, _ := opts(t, `echo "listening on port 8080"; echo "db connected"; sleep 30`,
		"listening on port\ndb connected")
	if rc := execute(o); rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
}

func TestMissingPatternTimesOut(t *testing.T) {
	o, _ := opts(t, `echo hello; sleep 30`, "this-never-appears")
	o.timeout = 1
	start := time.Now()
	if rc := execute(o); rc != 1 {
		t.Fatalf("rc = %d, want 1", rc)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("took %s, expected to give up promptly", d)
	}
}

func TestDaemonExitsEarlyButMatched(t *testing.T) {
	// Prints the pattern then exits 0 before the timeout.
	o, _ := opts(t, `echo "startup complete"`, "startup complete")
	if rc := execute(o); rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
}

func TestDaemonExitsEarlyMissing(t *testing.T) {
	o, _ := opts(t, `echo bye`, "never")
	o.timeout = 5
	if rc := execute(o); rc != 1 {
		t.Fatalf("rc = %d, want 1", rc)
	}
}

func TestErrorStringDetected(t *testing.T) {
	o, dir := opts(t, `echo "ready"; echo "ERROR: boom"; sleep 30`, "ready")
	o.errorStr = writeFile(t, dir, "errors", "ERROR\nFATAL")
	if rc := execute(o); rc != 1 {
		t.Fatalf("rc = %d, want 1 (error string should fail)", rc)
	}
}

func TestSetupFailureAborts(t *testing.T) {
	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o.setupFile = writeFile(t, dir, "setup", "exit 3")
	if rc := execute(o); rc != 3 {
		t.Fatalf("rc = %d, want 3 (setup exit code)", rc)
	}
}

func TestPostFailureSetsExitCode(t *testing.T) {
	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o.postFile = writeFile(t, dir, "post", "exit 4")
	if rc := execute(o); rc != 4 {
		t.Fatalf("rc = %d, want 4 (post exit code with no missing/error)", rc)
	}
}

func TestPostSuccess(t *testing.T) {
	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o.postFile = writeFile(t, dir, "post", "true")
	if rc := execute(o); rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
}

func TestSigtermStopsSleeper(t *testing.T) {
	// A daemon that would sleep far longer than the test; once the pattern is
	// found the tool must SIGTERM it and return promptly.
	o, _ := opts(t, `echo up; sleep 300`, "up")
	start := time.Now()
	if rc := execute(o); rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("took %s, expected prompt termination", d)
	}
}

// withCheckURL adds a --check-url-file with the given YAML to an options.
func withCheckURL(t *testing.T, o options, dir, yaml string) options {
	t.Helper()
	o.checkURLFile = writeFile(t, dir, "checkurl", yaml)
	return o
}

func TestCheckURLPasses(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer ts.Close()

	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o = withCheckURL(t, o, dir, "url: "+ts.URL+"\nexpected:\n  code: 200\n  body-contains: \"hello world\"\n")
	if rc := execute(o); rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
}

func TestCheckURLWrongCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer ts.Close()

	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o.timeout = 1
	o = withCheckURL(t, o, dir, "url: "+ts.URL+"\nexpected:\n  code: 500\n")
	if rc := execute(o); rc != 1 {
		t.Fatalf("rc = %d, want 1 (wrong status code)", rc)
	}
}

func TestCheckURLBodyMissing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hi there"))
	}))
	defer ts.Close()

	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o.timeout = 1
	o = withCheckURL(t, o, dir, "url: "+ts.URL+"\nexpected:\n  body-contains: \"not present\"\n")
	if rc := execute(o); rc != 1 {
		t.Fatalf("rc = %d, want 1 (body substring missing)", rc)
	}
}

func TestCheckURLDefaultAccepts2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer ts.Close()

	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o = withCheckURL(t, o, dir, "url: "+ts.URL+"\n")
	if rc := execute(o); rc != 0 {
		t.Fatalf("rc = %d, want 0 (2xx with no explicit code)", rc)
	}
}

func TestCheckURLDefaultRejects4xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o.timeout = 1
	o = withCheckURL(t, o, dir, "url: "+ts.URL+"\n")
	if rc := execute(o); rc != 1 {
		t.Fatalf("rc = %d, want 1 (>=400 with no explicit code)", rc)
	}
}

func TestCheckURLConnectionRefusedRetriesThenFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close() // nothing is listening now

	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	o.timeout = 1
	o = withCheckURL(t, o, dir, "url: "+url+"\n")
	start := time.Now()
	if rc := execute(o); rc != 1 {
		t.Fatalf("rc = %d, want 1 (connection refused)", rc)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("took %s, expected to give up near the timeout", d)
	}
}

func TestCheckURLList(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	o, dir := opts(t, `echo ready; sleep 30`, "ready")
	yaml := "- url: " + ts.URL + "\n  expected:\n    code: 200\n- url: " + ts.URL + "/other\n"
	o = withCheckURL(t, o, dir, yaml)
	if rc := execute(o); rc != 0 {
		t.Fatalf("rc = %d, want 0 (list of checks)", rc)
	}
}

func TestParseChecksForms(t *testing.T) {
	dir := t.TempDir()

	single := writeFile(t, dir, "single", "url: http://x/\nexpected:\n  body-contains: hi\n")
	got, err := parseChecks(single)
	if err != nil || len(got) != 1 || got[0].URL != "http://x/" {
		t.Fatalf("single: got %+v err %v", got, err)
	}
	if len(got[0].Expected.BodyContains) != 1 || got[0].Expected.BodyContains[0] != "hi" {
		t.Fatalf("scalar body-contains not parsed: %+v", got[0].Expected.BodyContains)
	}

	list := writeFile(t, dir, "list", "- url: http://a/\n- url: http://b/\n  expected:\n    body-contains:\n      - x\n      - y\n")
	got, err = parseChecks(list)
	if err != nil || len(got) != 2 {
		t.Fatalf("list: got %+v err %v", got, err)
	}
	if len(got[1].Expected.BodyContains) != 2 {
		t.Fatalf("list body-contains not parsed: %+v", got[1].Expected.BodyContains)
	}

	empty := writeFile(t, dir, "empty", "   \n")
	if got, err := parseChecks(empty); err != nil || got != nil {
		t.Fatalf("empty: got %+v err %v", got, err)
	}
}

func TestMissingStartFile(t *testing.T) {
	if rc := execute(options{timeout: 1}); rc != 2 {
		t.Fatalf("rc = %d, want 2 (usage error)", rc)
	}
}

func TestInvalidExpectedPattern(t *testing.T) {
	o, _ := opts(t, `echo hi`, "(unclosed")
	if rc := execute(o); rc != 2 {
		t.Fatalf("rc = %d, want 2 (bad regex)", rc)
	}
}
