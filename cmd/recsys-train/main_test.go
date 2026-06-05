package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitSamplesByTimeUsesNewestForEval(t *testing.T) {
	base := time.Now()
	samples := []sample{
		{at: base.Add(2 * time.Hour), group: "c"},
		{at: base, group: "a"},
		{at: base.Add(time.Hour), group: "b"},
		{at: base.Add(3 * time.Hour), group: "d"},
	}

	trainSamples, evalSamples := splitSamplesByTime(samples, 0.25)

	require.Len(t, trainSamples, 3)
	require.Len(t, evalSamples, 1)
	assert.Equal(t, "d", evalSamples[0].group)
}

func TestTrainStoresFeatureStats(t *testing.T) {
	samples := []sample{
		{label: 0, features: map[string]float64{"x": 0}},
		{label: 1, features: map[string]float64{"x": 10}},
	}

	model := train(samples, 2, 0.01, 0)

	stats, ok := model.FeatureStats["x"]
	require.True(t, ok)
	assert.Equal(t, 5.0, stats.Mean)
	assert.Equal(t, 5.0, stats.Std)
}

func TestEvaluateReportsModelAndBaselineMetrics(t *testing.T) {
	samples := []sample{
		{label: 1, group: "q1", features: map[string]float64{"base_rank": 0, "x": 1}},
		{label: 0, group: "q1", features: map[string]float64{"base_rank": 1, "x": 0}},
		{label: 1, group: "q2", features: map[string]float64{"base_rank": 1, "x": 1}},
		{label: 0, group: "q2", features: map[string]float64{"base_rank": 0, "x": 0}},
	}
	model := train(samples, 5, 0.05, 0)

	metrics := evaluate(samples, model, "search")

	assert.Contains(t, metrics, "model_logloss")
	assert.Contains(t, metrics, "model_auc")
	assert.Contains(t, metrics, "baseline_auc")
	assert.Contains(t, metrics, "model_ndcg_10")
	assert.Contains(t, metrics, "baseline_ndcg_10")
}

func TestValidateTrainingDataRejectsWeakData(t *testing.T) {
	samples := []sample{
		{label: 1, group: "q1"},
		{label: 1, group: "q2"},
		{label: 0, group: "q2"},
	}

	err := validateTrainingData(samples, trainingDataGate{
		minSamples:   4,
		minPositives: 2,
		minNegatives: 1,
		minGroups:    2,
	})
	assert.ErrorContains(t, err, "samples=3")

	err = validateTrainingData(samples, trainingDataGate{
		minSamples:   3,
		minPositives: 2,
		minNegatives: 2,
		minGroups:    2,
	})
	assert.ErrorContains(t, err, "negatives=1")
}

func TestValidateTrainingDataAllowsHealthyOrExplicitWeakData(t *testing.T) {
	samples := []sample{
		{label: 1, group: "q1"},
		{label: 0, group: "q1"},
		{label: 1, group: "q2"},
		{label: 0, group: "q2"},
	}

	err := validateTrainingData(samples, trainingDataGate{
		minSamples:   4,
		minPositives: 2,
		minNegatives: 2,
		minGroups:    2,
	})
	require.NoError(t, err)

	err = validateTrainingData(samples[:1], trainingDataGate{
		minSamples:   100,
		minPositives: 100,
		minNegatives: 100,
		minGroups:    100,
		allowWeak:    true,
	})
	require.NoError(t, err)
}
