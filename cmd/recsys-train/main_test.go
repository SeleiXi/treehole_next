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

func TestSplitSamplesByTimeRebalancesOneSidedEvalWhenPossible(t *testing.T) {
	base := time.Now()
	samples := []sample{
		{label: 1, at: base, group: "old-positive"},
		{label: 0, at: base.Add(time.Hour), group: "old-negative"},
		{label: 0, at: base.Add(2 * time.Hour), group: "new-negative"},
		{label: 1, at: base.Add(3 * time.Hour), group: "new-positive"},
	}

	split := splitSamplesByTimeDetailed(samples, 0.25)
	trainSamples := split.train
	evalSamples := split.eval

	require.Len(t, trainSamples, 2)
	require.Len(t, evalSamples, 2)
	assert.Equal(t, splitStrategyLabelDiversity, split.strategy)
	require.True(t, hasLabelDiversity(trainSamples))
	require.True(t, hasLabelDiversity(evalSamples))
	assert.Equal(t, "new-negative", evalSamples[0].group)
	assert.Equal(t, "new-positive", evalSamples[1].group)
}

func TestSplitSamplesByTimeSkipsEvalWhenSinglePositiveCannotBeShared(t *testing.T) {
	base := time.Now()
	samples := []sample{
		{label: 0, at: base, group: "a"},
		{label: 0, at: base.Add(time.Hour), group: "b"},
		{label: 0, at: base.Add(2 * time.Hour), group: "c"},
		{label: 1, at: base.Add(3 * time.Hour), group: "d"},
	}

	split := splitSamplesByTimeDetailed(samples, 0.25)
	trainSamples := split.train
	evalSamples := split.eval

	require.Len(t, trainSamples, 4)
	require.Empty(t, evalSamples)
	assert.Equal(t, splitStrategyTrainAll, split.strategy)
	require.True(t, hasLabelDiversity(trainSamples))
	require.NoError(t, validateSplitLabelDiversity(trainSamples, evalSamples, false))
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
	assert.Contains(t, searchSQL, "se.floor_id")
	assert.Contains(t, searchSQL, "se.hole_id")
	assert.Contains(t, searchSQL, "0 AS label")
	assert.Contains(t, searchSQL, "0 AS tag_count")
	assert.Contains(t, searchSQL, "FORCE INDEX (idx_search_event_type_created)")
}

func TestApplyHomeFeedbackGatesLooseFallbackToLegacyEmptyRequestID(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	rows := []homeRow{
		{
			UserID:    42,
			HoleID:    1,
			RequestID: "request-1",
			SampleAt:  sampleAt,
		},
		{
			UserID:   42,
			HoleID:   1,
			SampleAt: sampleAt,
		},
	}
	events := []homeFeedbackEvent{
		{
			UserID:    42,
			HoleID:    1,
			EventType: "open",
			RequestID: "other-request",
			CreatedAt: sampleAt.Add(time.Minute),
		},
	}

	applyHomeFeedback(rows, events)

	assert.Equal(t, 0.0, rows[0].Label)
	assert.Equal(t, 0.55, rows[1].Label)
}

func TestApplySearchFeedbackPreservesExactRequestPriority(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	rows := []searchRow{{
		UserID:    42,
		QueryHash: strings.Repeat("a", 64),
		RequestID: "search-request",
		FloorID:   10,
		HoleID:    1,
		SampleAt:  sampleAt,
	}}
	searchEvents := []searchActionEvent{
		{
			UserID:    42,
			QueryHash: rows[0].QueryHash,
			RequestID: "search-request",
			FloorID:   10,
			EventType: "hide",
			CreatedAt: sampleAt.Add(time.Minute),
		},
	}
	feedEvents := []homeFeedbackEvent{
		{
			UserID:    42,
			HoleID:    1,
			RequestID: "search-request",
			EventType: "favorite",
			CreatedAt: sampleAt.Add(2 * time.Minute),
		},
	}

	applySearchFeedback(rows, searchEvents, feedEvents)

	assert.Equal(t, 0.0, rows[0].Label)
}

func TestApplySearchFeedbackUsesFeedAndLegacyFallbacks(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	rows := []searchRow{
		{
			UserID:    42,
			QueryHash: strings.Repeat("a", 64),
			RequestID: "search-request",
			FloorID:   10,
			HoleID:    1,
			SampleAt:  sampleAt,
		},
		{
			UserID:   42,
			HoleID:   1,
			SampleAt: sampleAt,
		},
	}
	feedEvents := []homeFeedbackEvent{
		{
			UserID:    42,
			HoleID:    1,
			RequestID: "search-request",
			EventType: "open",
			CreatedAt: sampleAt.Add(time.Minute),
		},
		{
			UserID:    42,
			HoleID:    1,
			RequestID: "other-request",
			EventType: "reply",
			CreatedAt: sampleAt.Add(2 * time.Minute),
		},
	}

	applySearchFeedback(rows, nil, feedEvents)

	assert.Equal(t, 0.7, rows[0].Label)
	assert.Equal(t, 0.75, rows[1].Label)
}

func TestHomeTrainingQuerySelectsHoleIDForAffinityEnrichment(t *testing.T) {
	homeSQL := homeSamplesSQL()

	assert.Contains(t, homeSQL, "fe.hole_id")
	assert.Contains(t, homeSQL, "0 AS label")
	assert.Contains(t, homeSQL, "0 AS tag_count")
	assert.Contains(t, homeSQL, "0 AS open_count")
	assert.Contains(t, homeSQL, "0 AS impression_count")
}

func TestBootstrapTrainingQueriesUseRealEngagementWithoutEvents(t *testing.T) {
	searchSQL := bootstrapSearchSamplesSQL()
	assert.Contains(t, searchSQL, "FROM floor f")
	assert.Contains(t, searchSQL, "JOIN hole h ON h.id = f.hole_id")
	assert.Contains(t, searchSQL, "f.`like` >= 3")
	assert.Contains(t, searchSQL, "CONCAT('bootstrap-search-division-', h.division_id)")

	homeSQL := bootstrapHomeSamplesSQL()
	assert.Contains(t, homeSQL, "FROM hole h")
	assert.Contains(t, homeSQL, "h.reply >= 5")
	assert.Contains(t, homeSQL, "CONCAT('bootstrap-home-division-', h.division_id)")
	assert.Contains(t, homeSQL, "LEFT JOIN hole_feature hf")
}

func TestAssignBootstrapSearchRanksWithinGroups(t *testing.T) {
	rows := []searchRow{
		{RequestID: "a"},
		{RequestID: "a"},
		{RequestID: "b"},
		{RequestID: "a"},
	}

	assignBootstrapSearchRanks(rows)

	assert.Equal(t, 0, rows[0].BaseRank)
	assert.Equal(t, 1, rows[1].BaseRank)
	assert.Equal(t, 0, rows[2].BaseRank)
	assert.Equal(t, 2, rows[3].BaseRank)
	assert.Equal(t, rows[3].BaseRank, rows[3].Position)
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

func TestHomeFeaturesExposeHistoricalAffinities(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)

	features := homeFeatures(homeRow{
		SampleAt:         sampleAt,
		Reply:            3,
		View:             5,
		DivisionAffinity: 3.0,
		TagAffinity:      2.5,
		HoleCreatedAt:    sampleAt.Add(-time.Hour),
		HoleUpdatedAt:    sampleAt.Add(-time.Hour),
	})

	assert.Equal(t, 3.0, features["division_affinity"])
	assert.Equal(t, 2.5, features["tag_affinity"])
	assert.Equal(t, features["rule_score"]+5.5, features["fallback_score"])

	capped := homeFeatures(homeRow{
		SampleAt:         sampleAt,
		DivisionAffinity: 10,
		TagAffinity:      5,
		HoleCreatedAt:    sampleAt.Add(-time.Hour),
		HoleUpdatedAt:    sampleAt.Add(-time.Hour),
	})
	assert.Equal(t, capped["rule_score"]+8, capped["fallback_score"])
}

func TestApplyHomeFeedbackBuildsLabelsAndPriorCounts(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	rows := []homeRow{{
		UserID:    42,
		HoleID:    1,
		RequestID: "request-1",
		SampleAt:  sampleAt,
	}}
	events := []homeFeedbackEvent{
		{
			UserID:    42,
			HoleID:    1,
			EventType: "impression",
			CreatedAt: sampleAt.Add(-time.Hour),
		},
		{
			UserID:    42,
			HoleID:    1,
			EventType: "open",
			CreatedAt: sampleAt.Add(-30 * time.Minute),
		},
		{
			UserID:    42,
			HoleID:    1,
			EventType: "favorite",
			RequestID: "request-1",
			CreatedAt: sampleAt.Add(time.Minute),
		},
		{
			UserID:    42,
			HoleID:    1,
			EventType: "click",
			RequestID: "request-1",
			CreatedAt: sampleAt.Add(3 * time.Hour),
		},
	}

	applyHomeFeedback(rows, events)

	assert.Equal(t, 1.0, rows[0].Label)
	assert.Equal(t, 1, rows[0].OpenCount)
	assert.Equal(t, 1, rows[0].ImpressionCount)
}

func TestApplyHomeAffinitiesUsesPriorUserHistory(t *testing.T) {
	sampleAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	rows := []homeRow{{
		UserID:     42,
		HoleID:     1,
		DivisionID: 7,
		SampleAt:   sampleAt,
	}}
	events := []homeAffinityEvent{
		{
			UserID:     42,
			HoleID:     2,
			EventType:  "favorite",
			DivisionID: 7,
			CreatedAt:  sampleAt.Add(-time.Hour),
		},
		{
			UserID:     42,
			HoleID:     3,
			EventType:  "reply",
			DivisionID: 9,
			CreatedAt:  sampleAt.Add(-2 * time.Hour),
		},
		{
			UserID:     42,
			HoleID:     4,
			EventType:  "open",
			DivisionID: 7,
			CreatedAt:  sampleAt.Add(time.Minute),
		},
		{
			UserID:     42,
			HoleID:     5,
			EventType:  "subscribe",
			DivisionID: 7,
			CreatedAt:  sampleAt.Add(-91 * 24 * time.Hour),
		},
		{
			UserID:     99,
			HoleID:     6,
			EventType:  "favorite",
			DivisionID: 7,
			CreatedAt:  sampleAt.Add(-time.Hour),
		},
	}
	tags := map[int][]int{
		1: {10, 20},
		2: {10, 30},
		3: {20},
		4: {10},
		5: {10},
		6: {10},
	}

	applyHomeAffinities(rows, events, tags)

	assert.InDelta(t, 1.2, rows[0].DivisionAffinity, 0.000001)
	assert.InDelta(t, 1.54, rows[0].TagAffinity, 0.000001)
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

func TestValidateSplitLabelDiversityRejectsOneSidedTrainOrEval(t *testing.T) {
	healthyTrain := []sample{
		{label: 1, group: "q1"},
		{label: 0, group: "q1"},
	}
	healthyEval := []sample{
		{label: 1, group: "q2"},
		{label: 0, group: "q2"},
	}

	err := validateSplitLabelDiversity([]sample{
		{label: 0, group: "q1"},
		{label: 0, group: "q1"},
	}, healthyEval, false)
	assert.ErrorContains(t, err, "train label diversity")

	err = validateSplitLabelDiversity(healthyTrain, []sample{
		{label: 1, group: "q2"},
		{label: 1, group: "q2"},
	}, false)
	assert.ErrorContains(t, err, "eval label diversity")

	require.NoError(t, validateSplitLabelDiversity(healthyTrain, healthyEval, false))
	require.NoError(t, validateSplitLabelDiversity(healthyTrain, nil, false))
}

func TestValidateSplitLabelDiversityAllowsExplicitWeakData(t *testing.T) {
	err := validateSplitLabelDiversity([]sample{
		{label: 0, group: "q1"},
	}, []sample{
		{label: 1, group: "q2"},
	}, true)

	require.NoError(t, err)
}
