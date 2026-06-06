package memory_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/klaus/pkg/memory"
)

func TestNoOp_Retrieve(t *testing.T) {
	var c memory.Client = memory.NoOp{}
	chunks, err := c.Retrieve(t.Context(), "ctx-1", "what did we discuss?", 5)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestNoOp_Store(t *testing.T) {
	var c memory.Client = memory.NoOp{}
	err := c.Store(t.Context(), "ctx-1", "user", "hello world")
	require.NoError(t, err)
}
