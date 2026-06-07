package recsys

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"treehole_next/config"
	"treehole_next/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoreCandidateUsesConfiguredModel(t *testing.T) {
	oldEnabled := config.Config.RecsysModelRanking
	oldPath := config.Config.RecsysModelPath
	t.Cleanup(func() {
		config.Config.RecsysModelRanking = oldEnabled
		config.Config.RecsysModelPath = oldPath
	})

	path := filepath.Join(t.TempDir(), "home-model.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"version": "test-home",
		"intercept": 1,
		"weights": {
			"rule_score": 0,
			"reply_log": 2
		}
	}`), 0o644))
	config.Config.RecsysModelRanking = true
	config.Config.RecsysModelPath = path

	now := time.Now()
	hole := &models.Hole{ID: 1, Reply: 3, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	feedback := userFeedback{
		opened:      map[int]int{},
		negative:    map[int]bool{},
		impressions: map[int]int{},
		divisions:   map[int]float64{},
		tags:        map[int]float64{},
	}

	score := scoreCandidate(hole, nil, feedback, nil, now)

	assert.InDelta(t, 1+2*1.386294361, score, 0.000001)
}

func TestScoreCandidateIgnoresIncompatibleTaskModel(t *testing.T) {
	oldEnabled := config.Config.RecsysModelRanking
	oldPath := config.Config.RecsysModelPath
	t.Cleanup(func() {
		config.Config.RecsysModelRanking = oldEnabled
		config.Config.RecsysModelPath = oldPath
	})

	path := filepath.Join(t.TempDir(), "search-model.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"version": "test-search",
		"task": "search",
		"intercept": 100,
		"weights": {
			"rule_score": 0
		}
	}`), 0o644))
	config.Config.RecsysModelRanking = true
	config.Config.RecsysModelPath = path

	now := time.Now()
	hole := &models.Hole{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	feedback := userFeedback{
		opened:      map[int]int{},
		negative:    map[int]bool{},
		impressions: map[int]int{},
		divisions:   map[int]float64{},
		tags:        map[int]float64{},
	}
	ruleScore := scoreHole(hole, nil, now)

	score := scoreCandidate(hole, nil, feedback, nil, now)

	assert.InDelta(t, ruleScore, score, 0.000001)
}

func TestScoreCandidateModelUsesBaseRuleScoreFeature(t *testing.T) {
	oldEnabled := config.Config.RecsysModelRanking
	oldPath := config.Config.RecsysModelPath
	t.Cleanup(func() {
		config.Config.RecsysModelRanking = oldEnabled
		config.Config.RecsysModelPath = oldPath
	})

	path := filepath.Join(t.TempDir(), "home-model.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"version": "test-home",
		"task": "home",
		"weights": {
			"rule_score": 1,
			"feedback_impression_count": 0
		}
	}`), 0o644))
	config.Config.RecsysModelRanking = true
	config.Config.RecsysModelPath = path

	now := time.Now()
	hole := &models.Hole{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	feedback := userFeedback{
		opened:      map[int]int{},
		negative:    map[int]bool{},
		impressions: map[int]int{1: 2},
		divisions:   map[int]float64{},
		tags:        map[int]float64{},
	}
	baseRuleScore := scoreHole(hole, nil, now)

	score := scoreCandidate(hole, nil, feedback, nil, now)

	assert.InDelta(t, baseRuleScore, score, 0.000001)
	assert.Less(t, baseRuleScore-feedback.penalty(hole.ID), score)
}

func TestScoreHoleIgnoresFutureFeatureSnapshot(t *testing.T) {
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	hole := &models.Hole{
		ID:        1,
		Reply:     3,
		View:      7,
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	feature := &models.HoleFeature{
		Reply24h:         100,
		View24h:          200,
		HotScore:         20,
		QualityScore:     10,
		ControversyScore: 5,
	}

	noFeatureScore := scoreHole(hole, nil, now)
	feature.UpdatedAt = now.Add(time.Minute)
	futureFeatureScore := scoreHole(hole, feature, now)
	feature.UpdatedAt = now.Add(-time.Minute)
	pastFeatureScore := scoreHole(hole, feature, now)

	assert.InDelta(t, noFeatureScore, futureFeatureScore, 0.000001)
	assert.Greater(t, pastFeatureScore, noFeatureScore)
}

func TestCandidateFeaturesIgnoreFutureFeatureSnapshot(t *testing.T) {
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	hole := &models.Hole{
		ID:        1,
		Reply:     3,
		View:      7,
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	feature := &models.HoleFeature{
		Reply24h:     100,
		View24h:      200,
		HotScore:     20,
		QualityScore: 10,
		Reply1h:      4,
		View1h:       5,
	}
	feedback := userFeedback{}

	feature.UpdatedAt = now.Add(time.Minute)
	features := candidateFeatures(hole, feature, feedback, nil, now, 1)
	assert.NotContains(t, features, "feature_hot_score")
	assert.Equal(t, math.Log1p(float64(hole.Reply)), features["reply_24h_log"])

	feature.UpdatedAt = now.Add(-time.Minute)
	features = candidateFeatures(hole, feature, feedback, nil, now, 1)
	assert.Equal(t, feature.HotScore, features["feature_hot_score"])
	assert.Equal(t, math.Log1p(float64(feature.Reply24h)), features["reply_24h_log"])
}

func TestCandidateFeaturesExposeFallbackPenaltyFeature(t *testing.T) {
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	hole := &models.Hole{
		ID:         1,
		Reply:      3,
		View:       7,
		DivisionID: 1,
		CreatedAt:  now.Add(-2 * time.Hour),
		UpdatedAt:  now.Add(-time.Hour),
	}
	feedback := userFeedback{
		opened:      map[int]int{1: 2},
		negative:    map[int]bool{},
		impressions: map[int]int{1: 3},
		divisions:   map[int]float64{1: 3},
		tags:        map[int]float64{10: 10},
	}
	ruleScore := scoreHole(hole, nil, now)

	features := candidateFeatures(hole, nil, feedback, []int{10}, now, ruleScore)

	penalty := 2*2.25 + 3*1.75
	assert.Equal(t, penalty, features["feedback_penalty"])
	assert.Equal(t, ruleScore+8-penalty, features["fallback_score"])
}

func TestRankCandidatesUsesConfiguredModel(t *testing.T) {
	oldEnabled := config.Config.RecsysModelRanking
	oldPath := config.Config.RecsysModelPath
	t.Cleanup(func() {
		config.Config.RecsysModelRanking = oldEnabled
		config.Config.RecsysModelPath = oldPath
	})

	path := filepath.Join(t.TempDir(), "home-model.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"version": "test-home",
		"weights": {
			"reply_log": -5
		}
	}`), 0o644))
	config.Config.RecsysModelRanking = true
	config.Config.RecsysModelPath = path

	db := newRankerTestDB(t)
	now := time.Now()
	require.NoError(t, db.Create(&[]models.Hole{
		{ID: 1, Reply: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10},
		{ID: 2, Reply: 20, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 11},
	}).Error)

	scored, err := rankCandidatesForSize(db, nil, []int{1, 2}, now, 2, true)

	require.NoError(t, err)
	require.Len(t, scored, 2)
	assert.Equal(t, 1, scored[0].hole.ID)
}

func TestRankCandidatesCanDisableConfiguredModel(t *testing.T) {
	oldEnabled := config.Config.RecsysModelRanking
	oldPath := config.Config.RecsysModelPath
	t.Cleanup(func() {
		config.Config.RecsysModelRanking = oldEnabled
		config.Config.RecsysModelPath = oldPath
	})

	path := filepath.Join(t.TempDir(), "home-model.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"version": "test-home",
		"weights": {
			"reply_log": -5
		}
	}`), 0o644))
	config.Config.RecsysModelRanking = true
	config.Config.RecsysModelPath = path

	db := newRankerTestDB(t)
	now := time.Now()
	require.NoError(t, db.Create(&[]models.Hole{
		{ID: 1, Reply: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10},
		{ID: 2, Reply: 20, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 11},
	}).Error)

	scored, err := rankCandidatesForSize(db, nil, []int{1, 2}, now, 2, false)

	require.NoError(t, err)
	require.Len(t, scored, 2)
	assert.Equal(t, 2, scored[0].hole.ID)
}
