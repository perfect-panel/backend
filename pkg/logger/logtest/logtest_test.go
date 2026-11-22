package logtest

import (
	"testing"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestCollector(t *testing.T) {
	const input = "hello"
	c := NewCollector(t)
	logger.Info(input)
	assert.Contains(t, c.String(), input)
	c.Reset()
	assert.Empty(t, c.String())
}

// Discard drops the entries of its test only: the output in place before it
// is back once that test ends.
func TestDiscardRestoresTheOutput(t *testing.T) {
	c := NewCollector(t)
	t.Run("discarding", func(t *testing.T) {
		Discard(t)
		logger.Info("dropped")
	})
	logger.Info("kept")
	assert.NotContains(t, c.String(), "dropped")
	assert.Contains(t, c.String(), "kept")
}
