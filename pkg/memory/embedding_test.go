package memory

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateAndNormalize(t *testing.T) {
	t.Run("exact length is re-normalized", func(t *testing.T) {
		vec := []float32{3, 4}
		got := truncateAndNormalize(vec, 2)
		assertUnitLength(t, got)
		assert.InDelta(t, 0.6, got[0], 1e-6)
		assert.InDelta(t, 0.8, got[1], 1e-6)
	})

	t.Run("longer vector is truncated then re-normalized", func(t *testing.T) {
		// vec has 4 elements, dim=2; truncate to [3,4] then normalize.
		vec := []float32{3, 4, 9, 9}
		got := truncateAndNormalize(vec, 2)
		assert.Len(t, got, 2)
		assertUnitLength(t, got)
	})

	t.Run("shorter vector is unchanged in length", func(t *testing.T) {
		vec := []float32{1, 0}
		got := truncateAndNormalize(vec, 768)
		assert.Len(t, got, 2)
		assertUnitLength(t, got)
	})

	t.Run("zero-norm vector returned unchanged", func(t *testing.T) {
		vec := []float32{0, 0, 0}
		got := truncateAndNormalize(vec, 3)
		assert.Equal(t, []float32{0, 0, 0}, got)
	})

	t.Run("768-dim vector is unit-length after normalize", func(t *testing.T) {
		vec := make([]float32, 768)
		for i := range vec {
			vec[i] = 1
		}
		got := truncateAndNormalize(vec, 768)
		assert.Len(t, got, 768)
		assertUnitLength(t, got)
	})
}

// assertUnitLength checks that the L2 norm of vec is within floating-point
// tolerance of 1.0.
func assertUnitLength(t *testing.T, vec []float32) {
	t.Helper()
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	assert.InDelta(t, 1.0, math.Sqrt(sum), 1e-5)
}
