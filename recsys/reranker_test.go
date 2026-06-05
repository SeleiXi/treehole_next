package recsys

import (
	"testing"
	"time"

	"treehole_next/models"

	"github.com/stretchr/testify/assert"
)

func TestRerankFreshnessUsesAge(t *testing.T) {
	now := time.Now()
	oldQuiet := &models.Hole{ID: 1, UserID: 1, DivisionID: 1, Reply: 0, CreatedAt: now.Add(-7 * 24 * time.Hour)}
	freshActive := &models.Hole{ID: 2, UserID: 2, DivisionID: 2, Reply: 8, CreatedAt: now.Add(-time.Hour)}
	scored := []scoredHole{
		{hole: oldQuiet, score: 10},
		{hole: freshActive, score: 9},
	}

	holes := rerank(scored, 2, now)

	assert.Len(t, holes, 2)
	assert.Equal(t, 2, holes[0].ID)
}
