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
