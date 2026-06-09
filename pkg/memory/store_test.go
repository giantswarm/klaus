package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoOp(t *testing.T) {
	var s Store = NoOp{}

	chunks, err := s.Retrieve(t.Context(), "alice", "query", 5)
	require.NoError(t, err)
	require.Empty(t, chunks)

	err = s.Record(t.Context(), "alice", "user", "hello")
	require.NoError(t, err)
}

func TestTruncateAndNormalize(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
		dim  int
	}{
		{
			name: "truncation reduces length",
			vec:  []float32{3, 4, 0},
			dim:  2,
		},
		{
			name: "no truncation needed",
			vec:  []float32{1, 0},
			dim:  4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateAndNormalize(tc.vec, tc.dim)

			expectedLen := tc.dim
			if len(tc.vec) < tc.dim {
				expectedLen = len(tc.vec)
			}
			require.Len(t, result, expectedLen)

			// Check unit length (L2 norm == 1).
			var sumSq float64
			for _, v := range result {
				sumSq += float64(v) * float64(v)
			}
			require.InDelta(t, 1.0, sumSq, 1e-5)
		})
	}
}

func TestTruncateAndNormalize_ZeroVector(t *testing.T) {
	vec := make([]float32, 4)
	result := truncateAndNormalize(vec, 4)
	require.Len(t, result, 4)
}

func TestFactory_NoopWhenDisabled(t *testing.T) {
	t.Setenv("MEMORY_ENABLED", "false")
	s := New("http://kagent:9090", "myagent")
	_, isNoOp := s.(NoOp)
	require.True(t, isNoOp, "expected NoOp when MEMORY_ENABLED=false")
}

func TestFactory_NoopWhenNoModel(t *testing.T) {
	t.Setenv("MEMORY_ENABLED", "true")
	t.Setenv("KLAUS_EMBEDDING_MODEL", "")
	s := New("http://kagent:9090", "myagent")
	_, isNoOp := s.(NoOp)
	require.True(t, isNoOp, "expected NoOp when KLAUS_EMBEDDING_MODEL is unset")
}

func TestFactory_NoopWhenNoEndpoint(t *testing.T) {
	t.Setenv("MEMORY_ENABLED", "true")
	t.Setenv("KLAUS_EMBEDDING_MODEL", "text-embedding-3-small")
	s := New("", "myagent")
	_, isNoOp := s.(NoOp)
	require.True(t, isNoOp, "expected NoOp when endpoint is empty")
}

func TestFactory_KagentStoreWhenFullyConfigured(t *testing.T) {
	t.Setenv("MEMORY_ENABLED", "true")
	t.Setenv("KLAUS_EMBEDDING_MODEL", "text-embedding-3-small")
	s := New("http://kagent:9090", "myagent")
	_, isKagent := s.(*KagentStore)
	require.True(t, isKagent, "expected KagentStore when fully configured")
}
