package modelrank

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinearModelScoresAndClamps(t *testing.T) {
	minScore := -1.0
	maxScore := 2.0
	model := LinearModel{
		Intercept: 0.5,
		Weights: map[string]float64{
			"a": 2,
			"b": -1,
		},
		MinScore: &minScore,
		MaxScore: &maxScore,
	}

	assert.Equal(t, 1.5, model.Score(map[string]float64{"a": 1, "b": 1}))
	assert.Equal(t, 2.0, model.Score(map[string]float64{"a": 10}))
	assert.Equal(t, -1.0, model.Score(map[string]float64{"b": 10}))
}

func TestLinearModelNormalizesFeatureStats(t *testing.T) {
	model := LinearModel{
		Weights: map[string]float64{
			"x": 2,
		},
		FeatureStats: map[string]FeatureStats{
			"x": {Mean: 10, Std: 5},
		},
	}

	assert.Equal(t, 4.0, model.Score(map[string]float64{"x": 20}))
}

func TestLoadLinearModelCachesByFileMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":"test","intercept":1,"weights":{"x":2}}`), 0o644))

	model, err := Load(path)

	require.NoError(t, err)
	require.NotNil(t, model)
	assert.Equal(t, "test", model.Version)
	assert.Equal(t, 5.0, model.Score(map[string]float64{"x": 2}))
}
