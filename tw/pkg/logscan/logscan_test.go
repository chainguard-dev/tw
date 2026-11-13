package logscan

import (
	"bufio"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func collect(t *testing.T, in string) ([]string, error) {
	t.Helper()
	var got []string
	err := Lines(strings.NewReader(in), func(line string) {
		got = append(got, line)
	})
	return got, err
}

func TestLines(t *testing.T) {
	long := strings.Repeat("x", 1<<20)

	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "no trailing newline", in: "a\nb", want: []string{"a", "b"}},
		{name: "crlf", in: "a\r\nb\r\n", want: []string{"a", "b"}},
		{name: "blank lines kept", in: "a\n\nb\n", want: []string{"a", "", "b"}},
		// A line past bufio's 64KiB default used to end the scan, so the
		// ERROR line after it was never matched.
		{name: "line over 64KiB", in: long + "\nERROR after a long line\n", want: []string{long, "ERROR after a long line"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect(t, tc.in)
			if err != nil {
				t.Fatalf("Lines() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Lines() lines (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLinesTooLong(t *testing.T) {
	in := "first\n" + strings.Repeat("x", MaxLineSize+1) + "\nlast\n"
	got, err := collect(t, in)
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("Lines() error = %v, want %v", err, bufio.ErrTooLong)
	}
	if diff := cmp.Diff([]string{"first"}, got); diff != "" {
		t.Errorf("Lines() lines before the error (-want +got):\n%s", diff)
	}
}
