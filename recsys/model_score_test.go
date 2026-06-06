package recsys

import (
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

	scored, err := rankCandidatesForSize(db, nil, []int{1, 2}, now, 2)

	require.NoError(t, err)
	require.Len(t, scored, 2)
	assert.Equal(t, 1, scored[0].hole.ID)
}
