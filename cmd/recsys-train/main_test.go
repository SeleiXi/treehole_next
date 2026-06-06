package main

import (
	"math"
	"strings"
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

func TestTrainingQueriesGateLooseFallbackToLegacyEmptyRequestID(t *testing.T) {
	searchSQL := searchSamplesSQL()
	assert.Contains(t, searchSQL, "fe.request_id = se.request_id")
	assert.Contains(t, searchSQL, "AND se.request_id <> ''")
	assert.Contains(t, searchSQL, "AND se.request_id = ''")

	homeSQL := homeSamplesSQL()
	assert.Contains(t, homeSQL, "fa.request_id = fe.request_id")
	assert.Contains(t, homeSQL, "AND fe.request_id <> ''")
	assert.Contains(t, homeSQL, "AND fe.request_id = ''")

	assert.GreaterOrEqual(t, strings.Count(searchSQL, "request_id = ''"), 1)
	assert.GreaterOrEqual(t, strings.Count(homeSQL, "request_id = ''"), 1)
}

func TestSearchFeaturesIgnoreFutureFeatureSnapshot(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	futureFeatureAt := sampleAt.Add(time.Minute)
	score := 12.0
	reply24h := 8

	features := searchFeatures(searchRow{
		SampleAt:         sampleAt,
		FeatureUpdatedAt: &futureFeatureAt,
		HotScore:         &score,
		Reply24h:         &reply24h,
	})

	assert.NotContains(t, features, "feature_hot_score")
	assert.NotContains(t, features, "feature_reply_24h_log")

	pastFeatureAt := sampleAt.Add(-time.Minute)
	features = searchFeatures(searchRow{
		SampleAt:         sampleAt,
		FeatureUpdatedAt: &pastFeatureAt,
		HotScore:         &score,
		Reply24h:         &reply24h,
	})

	assert.Equal(t, score, features["feature_hot_score"])
	assert.Equal(t, math.Log1p(float64(reply24h)), features["feature_reply_24h_log"])
}

func TestHomeFeaturesUseOnlySampleTimeFeatureSnapshot(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	futureFeatureAt := sampleAt.Add(time.Minute)
	score := 9.0
	reply24h := 100

	features := homeFeatures(homeRow{
		SampleAt:         sampleAt,
		Reply:            3,
		FeatureUpdatedAt: &futureFeatureAt,
		HotScore:         &score,
		Reply24h:         &reply24h,
	})

	assert.NotContains(t, features, "feature_hot_score")
	assert.Equal(t, math.Log1p(3.0), features["reply_24h_log"])

	pastFeatureAt := sampleAt.Add(-time.Minute)
	features = homeFeatures(homeRow{
		SampleAt:         sampleAt,
		Reply:            3,
		FeatureUpdatedAt: &pastFeatureAt,
		HotScore:         &score,
		Reply24h:         &reply24h,
	})

	assert.Equal(t, score, features["feature_hot_score"])
	assert.Equal(t, math.Log1p(float64(reply24h)), features["reply_24h_log"])
}

func TestHomeFeaturesExposeFeedbackPenaltyAndFallbackScore(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)

	features := homeFeatures(homeRow{
		SampleAt:        sampleAt,
		Reply:           3,
		View:            5,
		OpenCount:       2,
		ImpressionCount: 3,
		HoleCreatedAt:   sampleAt.Add(-time.Hour),
		HoleUpdatedAt:   sampleAt.Add(-time.Hour),
	})

	penalty := 2*2.25 + 3*1.75
	assert.Equal(t, penalty, features["feedback_penalty"])
	assert.Equal(t, features["rule_score"]-penalty, features["fallback_score"])
}

func TestHomeGroupUsesRequestOrMinuteBucket(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 3, 40, 0, time.UTC)

	assert.Equal(t, "request-1", homeGroup(homeRow{
		UserID:    42,
		RequestID: "request-1",
		SampleAt:  sampleAt,
	}))
	assert.Equal(t, "user:42:202606061003", homeGroup(homeRow{
		UserID:   42,
		SampleAt: sampleAt,
	}))
	assert.Equal(t, "user:42", homeGroup(homeRow{
		UserID: 42,
	}))
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
