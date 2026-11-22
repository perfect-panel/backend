// Package logtest redirects the process-wide logger output for the length of
// a test, so a test can assert what was logged or keep expected errors out of
// the test output. The logger output is global: tests using it must not run
// in parallel with other tests that log.
package logtest

import (
	"bytes"
	"io"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger"
)

// Buffer holds the log entries written while it is installed.
type Buffer struct {
	buf *bytes.Buffer
}

// Discard drops every log entry until t ends, then restores the previous
// output.
func Discard(t *testing.T) {
	prev := logger.Reset()
	logger.SetWriter(logger.NewWriter(io.Discard))

	t.Cleanup(func() {
		logger.SetWriter(prev)
	})
}

// NewCollector collects every log entry until t ends, then restores the
// previous output.
func NewCollector(t *testing.T) *Buffer {
	var buf bytes.Buffer
	writer := logger.NewWriter(&buf)
	prev := logger.Reset()
	logger.SetWriter(writer)

	t.Cleanup(func() {
		logger.SetWriter(prev)
	})

	return &Buffer{
		buf: &buf,
	}
}

// Reset drops the entries collected so far.
func (b *Buffer) Reset() {
	b.buf.Reset()
}

// String returns the entries collected so far, one per line.
func (b *Buffer) String() string {
	return b.buf.String()
}
