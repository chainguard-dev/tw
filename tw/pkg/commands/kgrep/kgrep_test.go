package kgrep

import (
	"bufio"
	"errors"
	"strings"
	"testing"

	"github.com/chainguard-dev/tw/pkg/logscan"
)

func newCfg(t *testing.T, c *cfg) *cfg {
	t.Helper()
	if err := c.prerun(t.Context(), []string{"pod/test"}); err != nil {
		t.Fatalf("prerun() error = %v", err)
	}
	return c
}

func TestScanAfterLongLine(t *testing.T) {
	c := newCfg(t, &cfg{Patterns: []string{"ready"}, DefaultErrors: true})
	in := strings.Repeat("x", 1<<20) + "\nERROR after a long line\nready\n"

	matched := map[int]bool{}
	expected, notExpected, err := c.scan(strings.NewReader(in), "test", "default", matched)
	if err != nil {
		t.Fatalf("scan() error = %v", err)
	}
	if len(expected) != 1 || !matched[0] {
		t.Errorf("scan() expected = %v, matched = %v; want the ready line", expected, matched)
	}
	if len(notExpected) != 1 || !strings.Contains(notExpected[0].Text, "after a long line") {
		t.Errorf("scan() notExpected = %v; want the ERROR line after the long line", notExpected)
	}
}

func TestScanLineTooLong(t *testing.T) {
	c := newCfg(t, &cfg{DefaultErrors: true})
	in := strings.Repeat("x", logscan.MaxLineSize+1) + "\nERROR\n"

	if _, _, err := c.scan(strings.NewReader(in), "test", "default", map[int]bool{}); !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("scan() error = %v, want %v", err, bufio.ErrTooLong)
	}
}
