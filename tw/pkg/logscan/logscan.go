// Package logscan reads logs line by line for the grep commands.
package logscan

import (
	"bufio"
	"io"
)

// MaxLineSize is the longest line Lines accepts. bufio.Scanner's default is
// 64KiB, which one structured JSON log line can exceed.
const MaxLineSize = 16 << 20

// Lines calls fn with each line of r, without its line ending. A read error,
// including a line longer than MaxLineSize (bufio.ErrTooLong), is returned
// instead of ending the scan early with the remaining lines unread.
func Lines(r io.Reader, fn func(line string)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, MaxLineSize)
	for scanner.Scan() {
		fn(scanner.Text())
	}
	return scanner.Err()
}
